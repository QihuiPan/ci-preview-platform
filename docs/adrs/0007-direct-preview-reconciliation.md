# ADR 0007 Direct preview reconciliation

## Status

Accepted.

## Decision

Use a singleton Kubernetes reconciler for preview namespaces, workload ServiceAccounts, quotas, network policies, Deployments, Services and optional Ingress. Keep desired state in PostgreSQL and accept observations only for the matching pipeline and generation. Deploy immutable image digests; report ACTIVE after application readiness.

## Consequences

A user can run previews without installing an additional GitOps repository, Argo CD, or DNS automation service. External effects are idempotently retried and may temporarily lag desired state. Public DNS/TLS is operator configuration. The singleton and ownership checks are required; this is not active-active external reconciliation. A GitOps adapter remains future work.
