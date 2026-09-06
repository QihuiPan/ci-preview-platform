# Pipeline configuration

A repository may contain `.ci-preview.yml` at its root. GitHub events load this file at the exact commit SHA. Manual CLI submissions use `--file`. Unknown fields, duplicate YAML keys, dependency cycles, and missing dependencies are rejected.

```yaml
version: 1
jobs:
  test:
    type: command
    image: golang:1.26.8-alpine
    command: [sh, -ec, "go test ./...; mkdir -p artifacts; go test -json ./... > artifacts/tests.json"]
    resources: {cpu: 1, memory_mb: 1024}
    timeout_seconds: 900
    max_attempts: 3
```

Commands are argv arrays, not implicitly evaluated shell strings. Explicitly use `sh -ec` when needed. Jobs run as UID/GID 1000 with a read-only container root, writable `/workspace` and `/tmp`, no Kubernetes token, and public HTTPS/DNS egress only. Images requiring root or arbitrary writable root directories will fail. Jobs do not share workspaces; each independently checks out the same immutable source revision. Dependencies express order, not filesystem transfer.

Write artifacts beneath `/workspace/artifacts`. The worker collects regular files after the job exits; symlinks and special files are rejected. The archive limit is 16 MiB and 2048 files; logs are limited to 1 MiB. Artifacts are available through the API only after completion. Live log streaming is not implemented.

HOME, XDG_CACHE_HOME, GOPATH, GOCACHE, and GOMODCACHE point into the writable /tmp volume. These caches are attempt-local and are not reused across jobs. The temporary volume is limited to 1 GiB; the workspace is limited to 10 GiB. Configure other language tools to write beneath those volumes instead of their image's read-only system paths.

Limits: at most 32 jobs, 1-8 CPUs and 64-16384 MiB per job, 10-3600 seconds, 1-5 lease attempts, priority -10 to 10. The queue admits at most 1000 nonterminal jobs. The default tenant budget is two running jobs, 16 CPUs, and 32768 MiB. A nonzero command exit fails the pipeline; expired infrastructure leases retry up to the job budget. Cancelling or failing one branch cancels remaining nonterminal branches.

The `trusted` field is not an authorization mechanism. The API overwrites it using the operator repository policy. Fork jobs are always untrusted and cannot publish images.

To intentionally rebuild an already submitted revision after fixing infrastructure, use `cictl submit --rerun` with a new idempotency key. Retrying a lost HTTP response must instead use the same key and body; it must not create another logical run.

## Image builds

Enable a trusted repository with an `image_prefix` such as `ghcr.io/my-team` in the auth configuration. Add a separate trusted worker principal with the `buildkit-rootless` capability and a unique token/ID. Add that worker to Helm's `workers` list. Store Docker registry credentials in a Kubernetes `kubernetes.io/dockerconfigjson` Secret and reference its name as that worker's `registrySecret`.

```yaml
version: 1
jobs:
  test:
    image: golang:1.26.8-alpine
    command: [go, test, ./...]
    resources: {cpu: 1, memory_mb: 1024}
  image:
    type: buildkit
    needs: [test]
    resources: {cpu: 2, memory_mb: 2048}
    timeout_seconds: 1800
    buildkit:
      context: .
      dockerfile: Dockerfile
      destination: ghcr.io/my-team/widget:ci
    environment:
      ttl_minutes: 120
      exposure: public
      port: 8080
      health_path: /healthz
```

The trusted builder publishes an immutable digest, requests BuildKit SBOM and provenance attestations, and stores build metadata. A preview without an explicit image uses that digest, never the mutable tag. The registry must support OCI artifacts and be reachable over public HTTPS under the default network policy. SBOM generation may download the scanner image.

Rootless BuildKit requires a seccomp/AppArmor exception and `--oci-worker-no-process-sandbox`; only its dedicated trusted namespace relaxes Pod Security admission. This is not a sandbox for hostile users. Forks must never use that pool. Registry credentials appear only in the builder container, not checkout, ordinary command jobs, or the artifact helper.

The rootless namespace also permits the SETUID/SETGID helper capabilities and setuid privilege transition needed for user-namespace mapping. Nodes must allow unprivileged user namespaces. Some Ubuntu/AppArmor configurations block this; configure dedicated builder nodes according to the upstream BuildKit rootless guide rather than disabling host protections across a shared cluster.

For a private output image, set `controller.imagePullSecret` to an existing registry Secret in `ci-platform`. The controller copies it into previews and references it from the pod.

## Existing immutable preview image

A command job may specify an environment with an explicit `image: registry/app@sha256:...`. Only one build job and one environment are supported per pipeline. Preview TTL is 1-10080 minutes; ports must be 1024-65535. Submit a positive PR number. The entire DAG must succeed before deployment starts. A newer PR revision replaces the older preview after namespace deletion converges.
