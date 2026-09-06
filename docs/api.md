# API Reference

## Conventions

Requests and responses use JSON unless otherwise noted. Error responses have this form:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "Human-readable English explanation"
  }
}
```

Request bodies are limited to 1 MiB and reject unknown fields. The development API does not yet authenticate manual pipeline and worker routes; do not expose it publicly.

## Submit a pipeline

`POST /v1/pipelines`

```json
{
  "tenant": "acme",
  "repo": "acme/widget",
  "commit_sha": "0123456789abcdef",
  "trigger": "manual",
  "pr_number": 184,
  "spec": {
    "version": 1,
    "jobs": {
      "test": {
        "image": "golang:1.26.5",
        "command": ["go", "test", "./..."],
        "capabilities": ["linux-amd64"],
        "resources": {"cpu": 1, "memory_mb": 1024}
      }
    }
  }
}
```

The response is `201 Created` with the pipeline, jobs, and an empty attempt list. Invalid graphs return `400 Bad Request`.

## Receive a GitHub webhook

`POST /v1/webhooks/github`

Required headers:

- `X-Hub-Signature-256: sha256=<hex hmac>`
- `X-GitHub-Delivery: <unique delivery id>`
- `X-GitHub-Event: pull_request` or `push`

New supported deliveries return `202 Accepted`. A replay of the same delivery ID returns `200 OK` and the original logical result with `"duplicate": true`. Pull-request actions `opened`, `reopened`, and `synchronize` create a default pipeline. `closed` marks the preview for deletion and cancels non-terminal work.

## Read a pipeline

`GET /v1/pipelines/{id}`

The response contains the immutable pipeline identity, ordered jobs, attempt history, timestamps, queue reason, and current states. Lease tokens are never present in this response.

## Register and heartbeat a worker

`POST /v1/workers/register`

```json
{
  "id": "worker-1",
  "pool": "trusted",
  "capabilities": ["linux-amd64", "buildkit-rootless"],
  "capacity": 4,
  "trusted": true
}
```

Registration is idempotent by worker ID and refreshes its heartbeat. Use `POST /v1/workers/{id}/heartbeat` with an empty JSON object to remain eligible.

## Poll assignments

`GET /v1/workers/{id}/assignments`

The response contains active assignments for that worker. Each assignment includes a lease token and deadline. Treat the token as a short-lived credential and never log it.

## Heartbeat and complete an attempt

`POST /v1/attempts/{id}/heartbeat`

```json
{"lease_token": "lease_<random>"}
```

`POST /v1/attempts/{id}/complete`

```json
{
  "lease_token": "lease_<random>",
  "success": true,
  "message": "isolated execution completed"
}
```

A wrong, expired, cancelled, or superseded token returns `409 Conflict`. Workers must stop publishing logs or artefacts after this response.

## Cancel a job

`POST /v1/jobs/{id}/cancel`

Cancellation applies to the pipeline in version 0.1.0. All non-terminal jobs become `CANCELLED`, active attempts are invalidated, and a related preview is marked for deletion.

## Read a preview

`GET /v1/previews/{repo}/{pr}`

Encode the slash in a repository full name as `%2F`, for example `/v1/previews/acme%2Fwidget/184`. The response includes namespace, URL, generation, desired and actual state, expiry, and last update.
