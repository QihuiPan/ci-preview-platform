# Known Limitations

Version 0.1.0 deliberately implements the correctness kernel before cloud integrations.

- State is held in memory. Restarting the API loses deliveries, pipelines, workers, attempts, and previews.
- A single scheduler loop provides leadership. Horizontal replicas would need PostgreSQL row locking or external leader election.
- Tenant fairness state is process-local and is not weighted. The current policy is one round-robin turn per tenant, priority then FIFO inside a tenant.
- Worker capacity is expressed as concurrent slots. CPU and memory requests are retained in job specifications but are not yet bin-packed.
- The worker command simulates isolated success and never executes the submitted image or command.
- GitHub webhooks currently compile a built-in default plan; the gateway does not yet fetch a repository-owned configuration file or publish GitHub check runs.
- Artefact and log APIs are not implemented. Cache helpers validate keys and individual archive entries only.
- NATS or Redis Streams and S3 or MinIO adapters are not wired into the executable runtime.
- Preview reconciliation updates an internal desired/actual record; it does not commit GitOps manifests, contact Argo CD, manage DNS, or inspect Kubernetes.
- Manual pipeline and worker endpoints do not have authentication or authorization.
- Prometheus metrics are counters only. SLO histograms, bounded tenant dimensions, tracing, and persistent audit events remain to be added.
- Terraform and Helm assets require an existing Kubernetes context and a prebuilt image. They have no cloud-provider opinion.

These limitations are release blockers for production use, but they do not weaken the automated tests for DAG, deduplication, fairness, lease, cancellation, cache-path, and preview-close invariants.
