# API and CLI

All /v1 routes except the signed GitHub webhook require an Authorization: Bearer token header. Health and readiness are unauthenticated. Metrics require an administrator token. No API response exposes stored lease tokens except assignments to their configured worker.

| Method and route | Role | Purpose |
| --- | --- | --- |
| POST /v1/pipelines | tenant/admin | Submit a normalized pipeline; Idempotency-Key required |
| GET /v1/pipelines | tenant/admin | Latest 200 visible pipelines |
| GET /v1/pipelines/{id} | tenant/admin | Plan, jobs, attempts, result references |
| POST /v1/pipelines/{id}/cancel | tenant/admin | Cancel all unfinished jobs |
| POST /v1/jobs/{id}/cancel | tenant/admin | Cancel a job and dependent aggregate |
| GET /v1/previews | tenant/admin | Visible desired and actual previews |
| GET /v1/attempts/{id}/objects/{name} | tenant/admin | Download committed logs/artifacts |
| POST /v1/workers/register | worker | Register the server-configured identity |
| GET /v1/workers/{id}/assignments | owning worker | Poll current leases |
| POST /v1/workers/{id}/heartbeat | owning worker | Renew worker presence |
| POST /v1/attempts/{id}/heartbeat | owning worker | Renew with lease_token |
| POST /v1/attempts/{id}/complete | owning worker | Commit result with lease_token |
| GET /v1/attempts/{id}/source-token | owning worker | Short-lived checkout credential; X-Lease-Token required |
| PUT /v1/attempts/{id}/objects/{name} | owning worker | Persist bounded content; X-Lease-Token required |
| GET /v1/internal/previews | controller | Reconciliation input |
| POST /v1/internal/previews/observe | controller | Generation-fenced observation |
| GET /v1/internal/active-attempts | controller | Orphan cleanup input |

The submission body uses repo, commit_sha, optional source_repo, pr_number, and spec. Tenant, trust, trigger and installation identity are server-authoritative. Unknown fields are rejected. Retry the same body with the same Idempotency-Key after network errors; a conflicting body returns 409. A semantic revision replay may return its original pipeline.

Successful creation returns 201 with {pipeline: PipelineView, duplicate: false}. Replay returns 200. GitHub deliveries return 202 or 200. Authorization failures are 401/403; missing objects are 404; stale leases and state conflicts are 409; queue backpressure is 429 with Retry-After; dependency failures are 503. Do not retry a 409 completion as a fresh result.

Supported object names: logs, artifacts.tar, build-metadata.json, provenance.json. Objects are authenticated downloads, not public presigned URLs. JSON request bodies are limited to 1 MiB; artifact uploads are bounded separately.

## CLI

Set API_URL and either API_TOKEN or API_TOKEN_FILE. Build with go build -o bin/cictl ./cmd/cictl.

- cictl submit --file CONFIG --repo OWNER/REPO --sha FULL_SHA --key STABLE_ID [--pr NUMBER]
- Add --rerun and a new key only when intentionally starting a new run of an existing revision.
- cictl pipelines
- cictl show PIPELINE_ID
- cictl cancel PIPELINE_ID
- cictl previews
- cictl logs ATTEMPT_ID
- cictl artifact ATTEMPT_ID artifacts.tar

The CLI writes JSON or requested artifact bytes to stdout and diagnostics to stderr. Protect downloaded artifacts and inspect archives before extraction. Omit --key only for a new logical submission; the generated key is printed to stderr for retries.
