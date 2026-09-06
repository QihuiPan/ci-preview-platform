# ADR 0002 Transactional PostgreSQL runtime

## Status

Accepted for the small-team 0.2 runtime; supersedes the 0.1 target-only schema.

## Decision

Use one versioned encrypted state snapshot locked with SELECT FOR UPDATE for each mutation. Compute time from PostgreSQL after acquiring the lock, apply deterministic transitions, persist before acknowledgment, and keep all remote I/O outside the transaction. Use a bounded pool and transaction timeout. Protect lease secrets using AES-256-GCM and a separately backed-up key.

## Consequences

This makes restart recovery, concurrent API coordination, duplicate handling, cancellation, and lease fencing executable today. It serializes all writes and scales with retained state size. It is not an enterprise throughput design. A future normalized-row migration needs explicit schema conversion and concurrency tests, not a cosmetic replacement of SQL files.

The runtime schema is created in internal/persistence/backend.go; migrations/001_initial_schema.sql mirrors it for review. Startup migration uses a PostgreSQL advisory lock.
