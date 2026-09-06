# CI Preview Platform

[![CI](https://github.com/QihuiPan/ci-preview-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/QihuiPan/ci-preview-platform/actions/workflows/ci.yml)
[![Cluster acceptance](https://github.com/QihuiPan/ci-preview-platform/actions/workflows/cluster.yml/badge.svg)](https://github.com/QihuiPan/ci-preview-platform/actions/workflows/cluster.yml)

A self-hosted CI service for small teams: submit a repository commit and a YAML pipeline, run real Kubernetes containers, retain logs and artifacts, and deploy an expiring pull-request preview.

The 0.2 runtime replaces the original simulator. PostgreSQL persists scheduling decisions and encrypted leases. Workers execute actual commands; an independent controller reports a preview as active only after Kubernetes reports its application ready.

## Start here

- [Installation and first successful job](docs/installation.md)
- [Repository pipeline configuration](docs/pipelines.md)
- [GitHub App setup](docs/github-app.md)
- [API and CLI reference](docs/api.md)
- [Operations, backup, and upgrades](docs/operations.md)
- [Security boundaries](docs/threat-model.md) and [supported scope](docs/known-limitations.md)
- [Verification results](docs/test-report.md) and [changelog](CHANGELOG.md)

You need a dedicated Linux Kubernetes cluster with an enforcing NetworkPolicy CNI, persistent storage, Go 1.26.5, Docker, kubectl, and Helm 3.18+. The control plane is not a hosted service: you supply your cluster, image registry, and optional public DNS/TLS. No cloud account or paid service is created by the installer.

For a disposable local demonstration, use the kind instructions in the installation guide. For an existing suitable cluster:

```bash
git clone https://github.com/QihuiPan/ci-preview-platform.git
cd ci-preview-platform
go build -o bin/cictl ./cmd/cictl
docker build -t YOUR_REGISTRY/ci-preview-platform:0.2.0 .
docker push YOUR_REGISTRY/ci-preview-platform:0.2.0

kubectl create namespace ci-platform
# Run once only. This generates random secrets; never regenerate the state key on upgrade.
bin/cictl init --repo octocat/Hello-World | kubectl create -f -
helm upgrade --install ci deploy/helm/ci-preview-platform \
  --namespace ci-platform --set image=YOUR_REGISTRY/ci-preview-platform:0.2.0 \
  --wait --wait-for-jobs --timeout 10m
kubectl port-forward -n ci-platform service/ci-api 8080:8080
```

In a second shell, export the tenant token without printing it, then submit the included working example:

```bash
export API_URL=http://127.0.0.1:8080
export API_TOKEN="$(kubectl get secret ci-secrets -n ci-platform -o jsonpath='{.data.tenant-token}' | base64 --decode)"
bin/cictl submit --file config/demo.pipeline.yml --repo octocat/Hello-World \
  --sha 7fd1a60b01f91b314f599c7452940e383a528cfb --key first-job
bin/cictl pipelines
```

Use the returned pipeline ID with `cictl show ID`; use an attempt ID with `cictl logs ATTEMPT_ID`. A real container must exit successfully before the pipeline becomes `SUCCEEDED`.

## Included

- Strict YAML DAG planning, immutable commit checkout, repository allowlists, role/tenant authentication.
- Tenant round-robin scheduling, concurrency/CPU/memory budgets, worker capabilities and separate trust pools.
- Random lease tokens, renewal, bounded infrastructure retries, cancellation and stale-result fencing.
- Isolated non-root job pods, bounded logs and artifact archives, content-addressed S3/MinIO storage.
- Optional trusted rootless BuildKit publishing with immutable digests and OCI SBOM/provenance attestations.
- Readiness-driven Kubernetes preview deployments, quotas, network policies, ingress, TTL cleanup, and durable PR-close tombstones.
- GitHub App signature verification, repository-owned configuration, delivery deduplication and check-run updates.
- Persistent installation, operator CLI, recovery runbooks, unit/race/database/cluster tests.

## Supported scope

This is a small-team self-hosted release, not an audited hostile-multi-tenant SaaS or an implementation of every stretch goal in the original blueprint. In particular, automatic worker autoscaling, weighted tenant scheduling, Argo CD/GitOps delivery, artifact signing, and a browser dashboard are not included. Read the explicit [limits and deployment assumptions](docs/known-limitations.md) before running untrusted submissions.

All source comments, examples, documentation, and change entries are in English. Every change commit must update `CHANGELOG.md`.
