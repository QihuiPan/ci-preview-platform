# Installation

## Requirements and safety

Use a dedicated Linux Kubernetes 1.34-1.35 cluster. Calico or another CNI must actually enforce NetworkPolicy; merely accepting NetworkPolicy resources is insufficient. Use a default dynamic StorageClass. Allow public HTTPS to GitHub and required image registries. Budget at least 4 CPUs and 8 GiB for a small deployment plus workload capacity.

The API has no cluster credentials. The worker and controller are trusted management services with cluster-wide namespace and workload permissions. Kubernetes RBAC cannot restrict namespace creation to a prefix. Do not install these management roles into a cluster containing unrelated sensitive workloads.

The chart starts PostgreSQL and MinIO with persistent volumes; these single-instance dependencies are not highly available. They are for a small-team installation. See operations before expanding service availability commitments.

## Disposable kind installation

Install Docker, Go 1.26.5, kubectl, Helm 3.18+, and kind 0.30+. Docker must be running in Linux-container mode. On Windows, use WSL2 with Docker integration or a remote Linux cluster.

```bash
kind create cluster --name ci-preview-test --image kindest/node:v1.34.0 --config deploy/kind.yaml
kubectl apply --server-side -f https://raw.githubusercontent.com/projectcalico/calico/v3.31.2/manifests/calico.yaml
kubectl wait --for=condition=Ready nodes --all --timeout=300s
kubectl rollout status daemonset/calico-node -n kube-system --timeout=300s
```

Run the complete disposable acceptance scenario:

```bash
bash scripts/cluster-test.sh
```

The script builds and loads the image, creates random credentials, installs the chart, and checks real jobs, artifacts, preview HTTP, restart persistence, failure, cancellation, and expiration. It requires the context `kind-ci-preview-test` and a fresh `ci-platform` namespace. It never deletes an unrelated cluster. When finished, explicitly remove this disposable cluster if no longer needed:

```bash
kind delete cluster --name ci-preview-test
```

## Existing cluster

Build and push the application image to your registry, create `ci-platform`, run `cictl init | kubectl create -f -` once, and install Helm as shown in the root README. The default credentials authorize only `octocat/Hello-World` in tenant `default`, using an untrusted worker with two slots.

Run `kubectl get pods,pvc,jobs -n ci-platform`. Both PVCs must bind, the bucket initialization Job must complete, and the API must become ready. Do not bypass failing readiness checks. Common causes are an unavailable image, missing storage, insufficient memory, or a failed object-store initialization.

Existing cluster images must be pullable by Kubernetes. If your platform image is private, configure imagePullSecrets on the management ServiceAccounts and on workload namespaces through your cluster image-credential provider. For the simplest installation, use a public platform image or preload it on each node. Private source repositories are separately supported through a GitHub App.

## First use on Windows PowerShell

```powershell
go build -o bin/cictl.exe ./cmd/cictl
$env:API_URL = 'http://127.0.0.1:8080'
$encoded = kubectl get secret ci-secrets -n ci-platform -o jsonpath='{.data.tenant-token}'
$env:API_TOKEN = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($encoded))
./bin/cictl.exe submit --file config/demo.pipeline.yml --repo octocat/Hello-World --sha 7fd1a60b01f91b314f599c7452940e383a528cfb --key first-job
./bin/cictl.exe pipelines
```

Keep the port-forward in a separate terminal. Tokens can also be read through `API_TOKEN_FILE`, which is preferable for automation. Never commit credentials or copy tokens into issue reports.

## Preview exposure

For an internal preview, use an image pinned to `@sha256:...`, an unprivileged HTTP port, and `exposure: internal`. Add an environment to a job and submit with `--pr NUMBER`. The URL is cluster-internal. Authorized caller/ingress namespaces must have the label `ci-preview/ingress=true`.

For public previews, install your ingress controller, label its namespace as above, configure wildcard DNS `*.PREVIEW_DOMAIN` to its address, and set Helm values `controller.previewDomain` and `controller.ingressClass`. Supply an existing wildcard TLS Secret in `ci-platform` using `controller.tlsSecret`; the controller copies it into each preview namespace. Use a separate preview domain, not the authenticated control-plane origin.

The controller verifies application readiness, not external DNS or Internet reachability. Verify public routing and TLS yourself before distributing URLs. API exposure is intentionally internal by default; place an authenticated HTTPS reverse proxy/ingress in front of it for remote clients and GitHub webhooks. Do not send bearer tokens over a public plaintext HTTP endpoint.
