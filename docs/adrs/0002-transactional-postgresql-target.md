# ADR 0002 Transactional PostgreSQL Target

## Status

Accepted for production target; reference adapter implemented

## Context

Webhook deduplication, job assignment, quota checks, cancellation, and completion require atomic state transitions. A queue alone cannot prove these invariants after redelivery or process failure.

## Decision

PostgreSQL is the durable source of truth. The target schema uses a primary key for delivery IDs and a partial unique index for one active attempt per job. Scheduling should use row locking or single-leader ownership. Version 0.1.0 keeps an in-memory adapter so the state machine and race tests run without infrastructure.

## Consequences

The executable demo is not restart durable. A production adapter must preserve the same atomic method boundaries, store only token hashes, expose migration rollback policy, and pass the existing contract tests against PostgreSQL.
