# ADR 0005 Content-addressed artifacts and cache boundary

## Status

Accepted, narrowed to the implemented runtime.

## Decision

Runtime logs and artifacts are stored under attempts/ATTEMPT_ID/NAME/SHA256. The API authorizes access using committed attempt metadata and the owning tenant. Collect bounded regular files only; never automatically extract arbitrary archives into another job.

The internal/cache package defines cache keys and validates archive paths, but distributed cache restore is not wired into command jobs. Do not describe the helper as a functioning cache service.

## Consequences

Retries cannot overwrite another attempt's results. Content-addressed writes are safely repeatable. Uncommitted uploads can become orphans and are expired by the bucket lifecycle policy. Image registry caching and cross-job artifact transfer require a separate supported workflow before being advertised.
