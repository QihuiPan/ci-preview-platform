# Architecture

## Purpose

The platform separates durable control-plane decisions from untrusted workload execution. The API accepts intent, the planner validates an immutable dependency graph, the scheduler chooses eligible work, workers act only under a short lease, and the reconciler repairs expired or undesired state.

Version 0.1.0 executes these rules in one Go process with a concurrency-safe in-memory adapter. The separation between `internal/api`, `internal/planner`, and `internal/control` keeps the transition to transactional PostgreSQL and independent scheduler/controller processes explicit.

## Component boundaries

### Event and API gateway

The gateway validates request size and shape. GitHub events require a `sha256` HMAC and delivery ID. Delivery deduplication and pipeline creation occur under one store lock, which models the transaction required in PostgreSQL. Unsupported event types are acknowledged without creating work.

### Planner

The planner treats the submitted configuration as immutable. It rejects unsupported versions, empty plans, self-dependencies, unknown dependencies, repeated dependencies, cycles, negative resources, and priorities outside `-10` through `10`. A sorted Kahn traversal produces a deterministic order for persistence and inspection.

### Scheduler

The scheduler considers only queued jobs whose dependencies have succeeded. It rotates across sorted tenants after the last selected tenant, then applies priority and FIFO order within a tenant. Before issuing a lease, it checks the tenant concurrent-attempt limit, worker heartbeat freshness, free slots, capabilities, and trust requirement.

The current process has one scheduler loop. A PostgreSQL implementation must select candidates with row locking or advisory leadership so multiple replicas cannot create two active attempts. The schema enforces one active attempt per job with a partial unique index.

### Worker protocol

Workers register their pool, capabilities, trust, capacity, and heartbeat. Assignments contain a random lease token that is omitted from normal pipeline reads. The first attempt heartbeat moves a job from `LEASED` to `RUNNING`; subsequent heartbeats extend the deadline. Completion succeeds only when the attempt is still active, the token matches, and the deadline has not passed.

The included worker simulates an isolated executor and intentionally does not execute repository commands. A production worker must create a dedicated Kubernetes pod and broker logs, artefacts, cache access, and termination without running workload code in the control plane.

### Preview reconciler

A successful job with an environment request activates one preview per repository and pull request. A close event creates or updates a `DELETING` tombstone before cancelling active work. This ordering prevents a delayed successful worker from recreating an environment after closure. TTL expiry uses the same deletion path.

Version 0.1.0 models desired and actual state in memory. A production controller should write an isolated namespace manifest to a GitOps repository, observe Argo CD and Kubernetes, revoke exposure first, and retain the tombstone until the namespace and owned artefacts are absent.

## State transitions

```text
Job:
QUEUED -> LEASED -> RUNNING -> SUCCEEDED
   |         |          +----> FAILED
   |         +---------------> LOST -> QUEUED with a new attempt
   +-------------------------> CANCELLED

Preview:
absent -> ACTIVE -> DELETING -> absent
            |
            +---- TTL expiry or pull-request close
```

An attempt that becomes `LOST`, `CANCELLED`, `FAILED`, or `SUCCEEDED` cannot return to an active state. A retry always creates a new attempt ID and token.

## Data ownership

| Entity | Owner | Key invariant |
| --- | --- | --- |
| Webhook delivery | Event gateway | One delivery ID maps to at most one logical result. |
| Pipeline | Planner | Commit and configuration remain immutable. |
| Job | Scheduler | Name is unique within a pipeline. |
| Attempt | Worker protocol | At most one active attempt exists per job. |
| Worker | Worker registry | Expired heartbeat removes scheduling eligibility. |
| Preview | Preview reconciler | One generation exists per repository and pull request. |
| Artefact | Artefact service | Digest identifies immutable content. |

## Observability

Every request produces a structured log with method, path, and duration. `/metrics` exports monotonic counters for webhook admission and replay, lease issue and expiry, stale completions, job outcomes, cancellation, and preview creation/deletion. Health and readiness endpoints do not depend on mutable workload state.

Production expansion should add OpenTelemetry spans, queue-wait and scheduler-decision histograms partitioned by bounded tenant labels, oldest-runnable-job gauges, worker startup duration, preview reconciliation latency, and persistent audit events.

## Availability and persistence

The in-memory adapter is process-local and intentionally cannot meet a control-plane availability SLO. The target PostgreSQL schema in `migrations/001_initial_schema.sql` supplies unique delivery keys, immutable plan storage, the active-attempt constraint, runnable and lease-expiry indexes, previews, artefacts, workers, and audit events.

The recommended production transaction for scheduling is:

1. select one runnable tenant using persisted fairness state;
2. lock a candidate job with `FOR UPDATE SKIP LOCKED`;
3. verify quota and worker capacity in the same transaction;
4. insert the attempt and hashed lease token;
5. transition the job to `LEASED`;
6. commit before publishing or returning the assignment.
