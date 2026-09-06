# Lease recovery

1. Inspect the pipeline, attempt status, worker logs and last heartbeat. Do not copy the lease token into logs or tickets.
2. Confirm API readiness and PostgreSQL/object-store reachability. Check worker identity, capacity, and trust/capability eligibility.
3. A lost worker stops renewing its lease. After expiry, the scheduler marks the attempt LOST and retries only if the attempt budget remains. Job timeout or budget exhaustion fails the pipeline.
4. A stale completion must return 409. Never force it into SUCCEEDED or reuse its token for another attempt.
5. The pod deadline bounds execution even if both managers are unavailable. After recovery, the controller removes inactive job namespaces after its grace period.
6. If cleanup is stuck, inspect namespace finalizers and events. Verify ci-preview/owner before manual intervention. Do not remove unrelated finalizers blindly.

Repeated expiry commonly indicates API outages, insufficient cluster capacity, blocked image pulls, or an overloaded worker manager. Fix the cause before resubmitting a new logical pipeline.
