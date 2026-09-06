# Changelog

All notable changes to this project are documented in this file. The format follows Keep a Changelog, and releases use Semantic Versioning.

## Unreleased

### Added

- Enforced changelog updates for ordinary change commits in CI.
- Added standalone CLI help and version commands for first-time users.
- Added signed GitHub App delivery integration, object-store signing, authentication, and transactional test-adapter regression coverage; cluster acceptance now also probes Kubernetes API egress isolation.
- Added complete installation, pipeline, GitHub App, API, security, backup, upgrade, and recovery documentation with a runnable public-repository example.
- Added manifest and artifact-boundary regression tests and control-plane ingress isolation.
- Added encrypted PostgreSQL transactions, repository-scoped bearer authentication, immutable YAML plans, GitHub App integration, actual Kubernetes execution, rootless image builds, S3 artifacts, and readiness-driven preview reconciliation.
- Added the tenant CLI, self-contained Helm installation, and real PostgreSQL and kind acceptance suites. This development candidate is not release-verified yet.

### Fixed

- Replaced the vulnerable prebuilt MinIO server default with a reproducible build of the upstream security-fixed source revision, and refreshed the BuildKit, kubectl, and MinIO client versions.
- Fixed clean-cluster installation by explicitly loading projected ServiceAccount credentials and importing a declarative MinIO lifecycle configuration instead of unsupported CLI flags.
- Updated the Go toolchain to 1.26.8 and the text dependency to its security-patched version; added a reachable-vulnerability CI gate.
- Restricted the trusted rootless builder's setuid-helper exception to SETUID/SETGID, leaving ordinary and fork job profiles unchanged.
- Made result metadata and terminal state one locked operation and reject an incorrect state encryption key at startup.
- Fixed the acceptance runner's installation permissions and aligned benchmark worker trust with the isolated execution policy.
- Fixed resource leaks from failed parallel branches, bounded lease retries, fenced stale PR events, and removed simulated execution and simulated preview success.

### Changed

- Recorded the successful Linux race, build, Compose validation, and container build release gate for version 0.1.0.

## 0.1.0 - 2026-09-06

### Added

- Added the Go control-plane API, structured logging, health probes, readiness probes, and Prometheus text metrics.
- Added GitHub webhook HMAC verification, atomic delivery deduplication, pull-request close handling, and default push and pull-request pipeline plans.
- Added deterministic DAG validation, tenant round-robin scheduling, priority-bounded FIFO selection, tenant quotas, worker capability matching, and trusted worker pools.
- Added lease issuance, heartbeat renewal, expiry reconciliation, retry attempts, cancellation, and stale-result rejection.
- Added preview activation, TTL reconciliation, close-event tombstones, and deletion behavior.
- Added cache-key and archive-path security helpers.
- Added unit, end-to-end, 100-job fairness-load, and scheduler benchmark coverage for correctness and failure cases, with race execution configured in Linux CI.
- Added a PostgreSQL target schema, Docker and Compose builds, a hardened Helm chart, Terraform deployment resources, architecture decisions, a threat model, runbooks, and API documentation.

### Known limitations

- The executable runtime uses an in-memory store; process restart loses control-plane state.
- The worker is a safe simulator and does not launch Kubernetes or BuildKit pods.
- Preview reconciliation models desired and actual state but does not yet write a GitOps repository or cluster resources.
