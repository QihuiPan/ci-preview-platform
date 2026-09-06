# Threat Model

## Scope and assets

The protected assets are repository source, installation tokens, signing secrets, protected deployment credentials, cache and artefact integrity, tenant scheduling capacity, preview network boundaries, audit evidence, and the correctness of commit checks.

The trust boundaries are GitHub to the event gateway, API clients to the control plane, control plane to workers, workers to Kubernetes and object storage, trusted versus untrusted worker pools, and preview namespaces to shared cluster services.

## Adversaries

- An external sender can forge or replay webhook traffic.
- A fork contributor can submit arbitrary code and configuration without repository secrets.
- A compromised worker can delay, replay, or falsify a completion.
- One tenant can flood the queue to consume shared capacity.
- A malicious cache archive can overwrite files outside its extraction directory or exhaust storage.
- A preview workload can probe other namespaces or cloud metadata.
- An operator mistake can leave previews, exposure, or artefacts after pull-request closure.

## Controls implemented in version 0.1.0

| Threat | Control |
| --- | --- |
| Forged webhook | Constant-time `sha256` HMAC verification; endpoint disabled without a secret. |
| Webhook replay | Atomic delivery-ID deduplication. |
| Invalid build graph | Unknown dependency, duplicate dependency, self-edge, cycle, and priority validation. |
| Tenant starvation | Tenant round-robin before bounded priority and FIFO selection. |
| Resource monopoly | Configurable concurrent-attempt quota per tenant. |
| Stale worker result | Random lease token, deadline, immutable attempt number, and CAS completion. |
| Worker pool escape | Capability and trust requirements checked before lease issue. |
| Cache traversal | Slash normalization, clean-path validation, drive-path rejection, and size cap. |
| Late preview creation | Close-event deletion tombstone created before cancellation. |
| Container privilege | Non-root, read-only root filesystem, no Linux capabilities, runtime-default seccomp. |

## Required production controls

Version 0.1.0 is not approved for public untrusted execution. Production deployment requires:

- GitHub App installation-token exchange with least-privilege repository permissions;
- mutually authenticated workload identity for worker routes;
- authorization on tenant, repository, worker, attempt, log, artefact, and preview operations;
- durable PostgreSQL transactions and append-only audit storage;
- token hashing at rest and redaction in logs and traces;
- per-installation admission limits and request rate limits;
- dedicated trusted and untrusted Kubernetes node pools;
- namespace Pod Security admission, ResourceQuota, LimitRange, default-deny NetworkPolicy, and metadata-service blocking;
- rootless BuildKit where possible and no privileged builders for fork events;
- short-lived object-store credentials constrained to one attempt prefix;
- verified image digests, SBOMs, provenance attestations, and admission policy;
- safe archive extraction with total expanded-size, file-count, symlink, hard-link, device, and compression-ratio limits;
- DNS and ingress ownership checks plus exposure revocation before preview deletion;
- secret rotation, backup, restore, incident response, and tenant offboarding procedures.

## Security acceptance tests

1. A webhook with an invalid signature returns `401` and creates no delivery or pipeline.
2. A repeated valid delivery returns the original pipeline and does not duplicate jobs.
3. A fork plan cannot lease a job that requires a trusted worker or protected capability.
4. A completion after lease expiry returns `409` after a new attempt is issued.
5. Cancellation and completion racing at the same barrier produce exactly one terminal pipeline state.
6. Traversal and oversized cache entries are rejected before extraction.
7. A pull-request close tombstone prevents a late preview completion from activating exposure.
