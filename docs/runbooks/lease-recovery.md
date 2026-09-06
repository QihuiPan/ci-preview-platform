# Lease Recovery Runbook

## Trigger

Use this runbook when `ci_leases_expired_total` rises, queue age grows, or workers report `409 state_conflict` while completing attempts.

## Immediate checks

1. Confirm `/healthz` and `/readyz` return `200`.
2. Check worker registration and heartbeat frequency against `WORKER_TTL`.
3. Compare attempt deadlines with API and worker clocks.
4. Inspect structured logs for `job leased`, `reconciliation completed`, and completion conflicts.
5. Confirm worker capacity and capabilities still match queued jobs.

## Safe recovery

Do not edit attempt state or reuse an old lease token. The reconciler marks an expired active attempt `LOST`, returns its job to `QUEUED`, and creates a new numbered attempt on the next scheduler turn. Allow the new attempt to finish. A recovered old worker must treat `409` as final and discard any unpublished result.

If no eligible worker exists, register or repair a matching worker rather than changing the job requirements after planning. Pipeline configuration is immutable.

## Escalation evidence

Capture pipeline and attempt IDs, worker IDs, deadlines, worker and API clock offsets, lease TTL, heartbeat interval, expired-lease counter delta, and the first stale-completion response. Never capture lease-token values.
