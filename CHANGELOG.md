# Changelog

All notable changes to this project are documented in this file. The format follows Keep a Changelog, and releases use Semantic Versioning.

## Unreleased

### Changed

- No unreleased changes.

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
