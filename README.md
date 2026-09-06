# Distributed CI Build and Preview Platform

[![CI](https://github.com/QihuiPan/ci-preview-platform/actions/workflows/ci.yml/badge.svg)](https://github.com/QihuiPan/ci-preview-platform/actions/workflows/ci.yml)

This repository is an executable reference control plane for multi-tenant CI scheduling and ephemeral pull-request environments. It focuses on the failure cases that distinguish a production-shaped scheduler from a happy-path queue: duplicate delivery, dependency validation, fair admission, lease expiry, cancellation races, stale completion, worker eligibility, and preview deletion.

Version 0.1.0 provides a runnable Go API and worker simulator, deterministic unit and end-to-end tests, a PostgreSQL target schema, a hardened container image, Helm and Terraform deployment assets, architecture decisions, a threat model, and operator runbooks.

## What works

- GitHub `sha256` webhook signature verification and atomic delivery-ID deduplication.
- Immutable pipeline plans with unknown-dependency and cycle rejection.
- Tenant round-robin scheduling, priority-bounded FIFO ordering, concurrent-job quotas, capability matching, trust-pool matching, and worker heartbeat expiry.
- Random lease tokens, lease renewal, automatic retry after expiry, and compare-and-set completion that rejects stale workers.
- Pipeline cancellation with one terminal result under cancel-versus-complete races.
- Pull-request preview activation, TTL expiry, close-event tombstones, and eventual deletion.
- JSON health, readiness, pipeline, worker, attempt, and preview APIs plus Prometheus text metrics and structured logs.
- Cache-key generation and archive-entry validation against traversal and oversized objects.

The local runtime intentionally uses an in-memory state adapter so the concurrency rules are easy to run and test. The PostgreSQL schema is included in [`migrations/001_initial_schema.sql`](migrations/001_initial_schema.sql); wiring the runtime to that schema, launching real BuildKit pods, and applying preview resources through GitOps are documented limitations rather than hidden claims.

## Architecture

```text
GitHub webhook                 Manual pipeline API
       |                               |
       +----------> Event/API gateway -+
                              |
                              v
                    Validated pipeline DAG
                              |
                              v
                  Fair, quota-aware scheduler
                              |
                       random lease token
                              |
                              v
                    isolated worker pool
                              |
                     CAS heartbeat/result
                              |
             +----------------+----------------+
             |                                 |
       pipeline status                  preview reconciler
                                               |
                                      desired/actual state
```

The core invariants are:

1. A GitHub delivery ID creates at most one logical pipeline.
2. A job becomes runnable only after every declared dependency succeeds.
3. A tenant cannot exceed its concurrent-attempt quota or permanently starve another tenant.
4. Only an eligible worker with capacity and matching trust/capabilities receives a lease.
5. Only the current, unexpired attempt token can commit a result.
6. A closed pull request cannot be reactivated by a delayed worker completion.

See [`docs/architecture.md`](docs/architecture.md) for component boundaries and [`docs/adrs`](docs/adrs) for design decisions.

## Quick start

Requirements:

- Go 1.26 or newer, or Docker with Compose.
- PowerShell examples below use PowerShell 7 or Windows PowerShell 5.1.

Start the API:

```powershell
$env:GITHUB_WEBHOOK_SECRET = "local-development-secret"
go run ./cmd/api
```

Start a trusted worker simulator in a second terminal:

```powershell
go run ./cmd/worker -id worker-local-1 -pool trusted -trusted=true
```

Submit the example three-job pipeline in a third terminal:

```powershell
$spec = Get-Content -Raw ./config/example.pipeline.json | ConvertFrom-Json
$request = @{
  tenant = "acme"
  repo = "acme/widget"
  commit_sha = "0123456789abcdef"
  trigger = "manual"
  pr_number = 184
  spec = $spec
} | ConvertTo-Json -Depth 20

$pipeline = Invoke-RestMethod -Method Post -Uri http://localhost:8080/v1/pipelines -ContentType application/json -Body $request
$pipeline.pipeline.id
```

The scheduler leases `test`, `image`, and `preview` in dependency order. Inspect the final pipeline after approximately ten seconds:

```powershell
Invoke-RestMethod http://localhost:8080/v1/pipelines/$($pipeline.pipeline.id) | ConvertTo-Json -Depth 20
Invoke-RestMethod http://localhost:8080/v1/previews/acme%2Fwidget/184 | ConvertTo-Json -Depth 10
```

Docker Compose provides the same two-process demo:

```bash
docker compose up --build
```

## API summary

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `POST` | `/v1/webhooks/github` | Verify, deduplicate, and translate supported GitHub events. |
| `POST` | `/v1/pipelines` | Submit an explicit immutable pipeline plan. |
| `GET` | `/v1/pipelines/{id}` | Read the DAG, jobs, attempts, and aggregate state. |
| `POST` | `/v1/jobs/{id}/cancel` | Persist cancellation and invalidate an active attempt. |
| `POST` | `/v1/workers/register` | Register or refresh pool, capabilities, trust, and capacity. |
| `POST` | `/v1/workers/{id}/heartbeat` | Keep a registered worker eligible. |
| `GET` | `/v1/workers/{id}/assignments` | Poll current lease-protected assignments. |
| `POST` | `/v1/attempts/{id}/heartbeat` | Start or renew the current attempt lease. |
| `POST` | `/v1/attempts/{id}/complete` | Commit success or failure when the lease still matches. |
| `GET` | `/v1/previews/{repo}/{pr}` | Read preview desired and actual state. Encode `/` in the repository name as `%2F`. |
| `GET` | `/healthz`, `/readyz`, `/metrics` | Probe health and export operational counters. |

Detailed request and response examples are in [`docs/api.md`](docs/api.md).

## Verification

```bash
go fmt ./cmd/... ./internal/... ./tests/... ./benchmarks/...
go vet ./...
go test -race -cover ./...
go test -bench=. -benchmem ./benchmarks
go build ./cmd/...
```

The test suite covers webhook replay, graph validation, tenant rotation, quota enforcement, capability selection, worker loss, stale completion, cancellation races, malicious archive names, and the pipeline-to-preview API flow.

## Security model

- The webhook endpoint is disabled until `GITHUB_WEBHOOK_SECRET` is configured.
- Lease tokens are returned only through worker assignment responses and are omitted from normal pipeline reads.
- Trusted jobs require trusted workers; fork-generated webhook plans do not require protected trust.
- The deployment runs as a non-root user with a read-only root filesystem, no Linux capabilities, a runtime-default seccomp profile, and restricted ingress/egress.
- The control plane never executes repository commands. The included worker is a simulator and deliberately does not execute the command field.

See [`docs/threat-model.md`](docs/threat-model.md) and [`SECURITY.md`](SECURITY.md) before exposing this service outside a development environment.

## Repository map

```text
cmd/api/                 HTTP control plane and scheduler loops
cmd/worker/              non-executing worker simulator
internal/api/            transport, webhook mapping, metrics
internal/control/        state machine, fairness, leases, reconciliation
internal/planner/        DAG validation and deterministic ordering
internal/github/         webhook signature verification
internal/cache/          content-addressed key and archive validation
tests/e2e/               control-plane-to-preview flow
benchmarks/              scheduler microbenchmark
migrations/              PostgreSQL production target schema
deploy/helm/             Kubernetes deployment chart
infra/terraform/         namespace, secret, and Helm release
docs/adrs/               architecture decisions
docs/runbooks/           failure recovery procedures
```

## Release discipline

Every implementation update must include an English entry in [`CHANGELOG.md`](CHANGELOG.md). Releases follow semantic versioning. Pull requests should keep comments, annotations, commit messages, API text, and operator documentation in English.

## License

Licensed under the MIT License. See [`LICENSE`](LICENSE).
