# Supported scope and limitations

The supported runtime is a small-team Kubernetes installation with PostgreSQL and S3/MinIO. The old in-memory simulator is not a deployment mode. The memory backend exists for tests.

## Deliberate implementation limits

- PostgreSQL commits one encrypted, versioned state snapshot under a row lock. This provides restart recovery, replica coordination, and atomic lease fencing, but serializes mutations and rewrites the snapshot. It is not a high-throughput scheduler architecture. Retained history and PR tombstones increase snapshot size.
- Admission is bounded to 1000 nonterminal jobs and 32 jobs per pipeline. Historical pipelines are pruned after seven days, except pipelines still owning previews. Delivery IDs and PR clocks are retained; operators must monitor state growth.
- Scheduling uses equal tenant round-robin, not weighted fair queuing. Workers are explicitly configured, one process per unique worker ID. Automatic worker registration, warm-pool scaling, KEDA, and cluster autoscaling are not provided.
- Kubernetes containers and NetworkPolicy are not a complete hostile-tenant security boundary. Use dedicated nodes/clusters or an independently configured sandbox runtime for adversarial tenants. Manager credentials are highly privileged.
- Jobs use independent checkouts; automatic cross-job artifact restore and dependency cache hydration are not implemented. The cache helper package remains a tested library, not a working distributed cache service.
- Preview delivery is direct Kubernetes reconciliation, not Argo CD/GitOps. The controller is a singleton with Recreate rollout strategy; do not scale it above one replica. External effects are at least once, while state observations are generation-fenced.
- One preview and one image build are supported per pipeline. Public preview readiness proves the application is available inside Kubernetes, not that public DNS, ingress, certificates, or a firewall are correctly configured.
- BuildKit attestation flags and registry publishing are implemented, but a real registry/App credential test requires operator credentials. Unsigned execution records are not independently verifiable signatures. No signing key, KMS integration, or trust-verification gate is included.
- Logs and artifacts are bounded and published after execution. There is no live streaming, full-text search, GUI, or pagination beyond the latest 200 pipelines in list responses.
- GitHub check delivery is best-effort and retried; an API crash between remote creation and local recording can create a duplicate check run. Very large backlogs beyond the latest 200 retained pipelines need operator replay. GitHub App permissions and installation coverage must be configured by the owner.
- The bundled PostgreSQL and MinIO are single-instance deployments. No automatic off-cluster backup, multi-region failover, SLO, or production capacity claim is made.
- The platform image must be pullable on worker nodes. The default network policy blocks private-network registries and arbitrary HTTP; adapt a reviewed policy if your environment needs them.

## External setup

A usable local installation does not require GitHub App credentials: manual submission of public repository commits works. Automatic webhook builds and private checkout require your App installation and private key. Public previews require your domain, ingress, TLS, and suitable image. Nothing in the repository provisions cloud infrastructure or incurs cloud spend automatically.

The project remains in its owner's existing GitHub visibility setting. A private repository requires collaborator access before another person can clone it.
