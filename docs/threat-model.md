# Threat model

## Assets and boundaries

Protect source code, GitHub installation tokens, registry credentials, bearer tokens, lease secrets, artifacts, scheduling capacity, and preview isolation. Boundaries are external caller -> API, API -> PostgreSQL/S3/GitHub, manager -> Kubernetes, trusted builder -> registry, and untrusted workload -> other cluster resources.

## Implemented controls

| Threat | Implemented control |
| --- | --- |
| Forged event | HMAC-SHA256 signature verification before parsing or external fetch |
| Cross-tenant access | Role authentication and repository/tenant authorization on jobs, pipelines, previews and artifact downloads |
| Trust escalation | Server-owned worker identity and repository policy; exact trusted/untrusted pool matching |
| Mutable source | Full immutable commit SHA, repository allowlist, authenticated init-only checkout |
| Replay and stale completion | Request-body digest, delivery dedupe, random leases, deadlines, idempotent matching results |
| Close/reopen races | Durable PR clock, conservative equal-time close, generation-fenced observations |
| Resource exhaustion | Admission cap, job limits, tenant/worker budgets, pod quotas, deadlines, bounded request/object sizes |
| Credential exposure | Tokenless job ServiceAccount; no manager token in jobs; registry credentials only in trusted builders |
| Cross-namespace probing | Default-deny ingress, restricted public-HTTPS/DNS egress, metadata/private-range exclusion |
| Artifact overwrite/traversal | Attempt-scoped content hashes; regular-file-only bounded archive collection; no automatic extraction |
| Persisted lease disclosure | AES-256-GCM encrypted state with a separately managed key |
| Accidental cleanup | Deterministic namespaces, ownership checks, controller grace period |

## Residual risks

The worker/controller identities have cluster-wide management privileges. A compromised manager can compromise this dedicated execution cluster. This release does not provide workload mTLS, attestation-based worker identity, hardened VM sandboxes, hardware isolation, or admission verification of signed artifacts.

Rootless BuildKit needs an explicit Pod Security/seccomp/AppArmor exception and no-process-sandbox mode. Only operator-trusted repositories may reach that pool. Non-root containers alone are not sufficient for adversarial multi-tenancy. Use sandboxed runtimes and dedicated nodes/clusters for mutually untrusted organizations.

NetworkPolicy enforcement depends on the CNI. The default blocks private registries and private service access; broadening it changes the threat model. External HTTPS remains available to repository code, so any secret intentionally placed in a build could be exfiltrated. Do not supply protected secrets to command jobs or forks.

The auth Secret contains plaintext bearer credentials at the Kubernetes boundary, protected by Secret RBAC and cluster encryption-at-rest configuration. Database leases are encrypted; this does not encrypt source, logs, object data, or backups. Configure S3/PVC/database encryption and TLS according to your environment.

Webhooks and public API endpoints must use HTTPS. Public previews must use a separate origin and domain from the authenticated API. Readiness is not an Internet connectivity test. Retention, ingress, DNS, image security updates, secret rotation and backups remain operator responsibilities.

Security tests are regression evidence, not an independent audit or a claim that arbitrary hostile public submissions are safe.
