# Operations, backup, and upgrades

## Health and diagnosis

/healthz proves the API process responds. /readyz checks PostgreSQL and the S3 bucket. A running pod is not equivalent to a ready service.

Use kubectl get pods,pvc,jobs -n ci-platform, kubectl logs deployment/ci-api -n ci-platform, and the corresponding worker/controller logs. Inspect a pipeline with cictl show ID. Queue reasons distinguish dependencies, quota, resource capacity, and missing eligible workers. Never paste Secret resources or authentication headers into issue reports.

Workers and the controller intentionally fail closed when the API is unavailable. Kubernetes activeDeadlineSeconds independently bounds job lifetime. A restarted worker adopts the current attempt namespace, while orphan cleanup removes inactive namespaces after a grace period. One running worker process per configured worker ID is mandatory.

## Credentials and tenant setup

The ci-secrets Secret contains auth.json, state-key, database-url, database credentials, object-store credentials, and independently generated administrator, tenant, worker, controller, and webhook tokens. Only the API mounts the policy; workers/controllers receive their individual token.

To add a tenant, update auth.json with a tenant principal and explicit repository policy. Never trust a caller-supplied tenant/trusted field. Add workers using unique IDs and tokens, resource capacity, pool, trusted flag and capability set. Update the workers Helm list with a matching tokenKey. The default tenant running limit is two jobs; increasing worker slots alone does not override it.

Policy is loaded at startup. After updating the Secret, restart API and affected management deployments. For bearer-token rotation, temporarily keep old and new principal tokens, roll callers to the new token, then remove the old token and restart API. Do not reuse worker IDs across simultaneous processes.

STATE_KEY rotation is not an ordinary token rotation: existing snapshots must be decrypted and re-encrypted in a controlled migration. Do not change or regenerate it on upgrade. If compromised, stop the service and perform a tested offline key migration before resuming.

## Backups and restore

Back up PostgreSQL using pg_dump/your managed database tooling, the object bucket, and the exact state encryption key in a separate protected store. Encrypt backups, restrict access, and test restores in an isolated namespace/cluster. Kubernetes Secret backup contains sensitive credentials; never commit it.

A restore needs matching database data, schema version, and STATE_KEY. Object history may be restored separately; absent objects do not justify fabricating successful artifacts. After restore, old leases expire and retry only within configured budgets. Old external namespaces may need ownership-checked reconciliation.

The bundled chart retains PostgreSQL and MinIO PVCs on Helm uninstall using helm.sh/resource-policy: keep. It does not back them up. Never delete these PVCs to fix an application rollout. MinIO expires attempt objects after seven days by default; retained database metadata may outlive an object near the retention boundary.

## Upgrade

1. Review CHANGELOG and supported schema compatibility.
2. Back up database, state key, and object storage; test recovery first.
3. Build a new immutable platform image tag/digest.
4. Run helm upgrade with the same release, namespace, existing Secret and PVCs.
5. Wait for readiness, run a canary job, inspect logs/artifacts and a preview.
6. Roll back only when the prior binary can read the persisted schema. Never overwrite a newer schema with an older snapshot.

Version 0.1 had no durable runtime state. Its in-memory state cannot be migrated. A 0.2 installation uses the new complete credentials and chart, not the old simulator Compose parameters.

## Retention and availability

Current completed pipeline retention is seven days, with current preview owners retained. PR clocks and delivery IDs survive cleanup to fence delayed events. Audit records and dedupe state require capacity monitoring. Watch PostgreSQL row size, database growth, transaction latency, API 429/503 rates, lease expiry, disk usage, and orphan namespaces.

The single-row state model is intentionally limited. Scale only after measuring your queue/history size. The bundled database/object store are not HA. There is no automatic disaster recovery or guaranteed SLO.

## Uninstall

Stop admission, cancel active pipelines, and allow or explicitly request preview cleanup before uninstalling the management services. Helm uninstall does not delete dynamically created preview/job namespaces. Inspect namespaces with ci-preview/kind labels and verify ownership before any manual deletion. Retained PVCs and Secret backups remain recoverable until you explicitly delete them.
