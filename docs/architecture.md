# Architecture

The API validates requests and repository policy before admission. It never executes repository commands and has no Kubernetes ServiceAccount token.

```text
GitHub App / cictl
        |
        v
Authenticated API -> PostgreSQL encrypted state + audit
        |                     |
        |                scheduler transaction
        v                     v
S3 / MinIO             worker-specific leases
                              |
                      isolated Kubernetes pods
                              |
                       fenced completion
                              |
                    desired preview generation
                              |
                    singleton preview controller
                              |
                  namespace / deployment / ingress
```

## Transactions

Each state mutation locks the single control_state row, reads database wall-clock time after obtaining the lock, restores the versioned snapshot, applies deterministic transitions, encrypts the result with AES-256-GCM, records an audit action when appropriate, and commits before acknowledgement. The pool is bounded to eight connections, transactions to ten seconds. GitHub, S3 and Kubernetes operations stay outside the row lock.

The encryption key must be backed up separately from PostgreSQL. Losing it loses recoverability of the persisted state. This small-team design favors explicit correctness over throughput; see ADR 0002 and the scope limits.

## Lifecycles

Jobs transition QUEUED -> LEASED -> RUNNING -> SUCCEEDED/FAILED. Expired leases become LOST and requeue work only while their attempt budget remains. Timeout or budget exhaustion fails the job. Cancelled/failed branches release all active attempt capacity. A random secret plus attempt identity and deadline fences heartbeat and completion. Replayed matching completion is idempotent.

A PR has a durable event timestamp, head revision, closed flag, and generation. Close wins ties. Old deliveries cannot recreate a closed preview. New revisions cancel active predecessor work and request namespace deletion. A succeeding current revision becomes deployable once old deletion converges.

Preview desired state and actual state are separate. The API creates PENDING records without URLs. The controller applies bounded resources and observes Deployment readiness before reporting ACTIVE. Deletion is acknowledged only after the namespace is absent. Observations must match generation, owner pipeline, and desired state. TTL cleanup does not depend on webhook delivery.

## Isolation and artifacts

Every attempt has its own namespace, emptyDir checkout and temporary storage, resource limits, default-deny ingress, restricted egress, and a tokenless workload ServiceAccount. A trusted BuildKit exception is isolated to its own namespace. Management credentials never enter repository command containers.

A read-only helper collects bounded regular-file archives after the command exits. The worker uploads objects with keys scoped to attempt and SHA-256 content. The API authorizes object access through the owning tenant and committed attempt metadata. Failed or stale uploads may leave unreferenced objects; bucket lifecycle expiry bounds their retention.

## Reconciliation

API replicas coordinate through PostgreSQL. Worker IDs are fixed operator identities; run exactly one process per ID. The preview controller runs as one replica. It also removes abandoned job namespaces whose attempts are no longer active after a grace period. Ownership labels are checked before namespace modification or deletion.

Checks, public routing, backups, autoscaling, and storage availability are separate operational concerns; do not infer them from a SUCCEEDED job.
