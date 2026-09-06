# Fault and recovery evidence

Automated regressions cover stale lease rejection, expired lease retry budgets, cancel-versus-complete races, capacity release after parallel failure, duplicate request conflict, durable PR close clocks, snapshot restoration, and TTL non-resurrection.

The PostgreSQL integration test uses two independent backend pools with 20 concurrent submissions, asserts one logical pipeline, injects a rollback, closes/reopens the connection, and rejects decryption with a different state key. It requires a disposable TEST_DATABASE_URL database and must never point at production.

The real-cluster acceptance test executes successful and failing containers, collects logs/artifacts, checks preview HTTP, restarts the API, cancels a running pipeline and waits for TTL cleanup. See test-report.md for actual completed runs. The test does not claim a full chaos campaign or an SLA measurement.

An infrastructure outage can leave an uploaded but uncommitted object or a terminating namespace. Lease fencing protects committed state; controller reconciliation and object lifecycle expiry clean up external leftovers.
