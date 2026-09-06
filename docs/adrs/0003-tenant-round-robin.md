# ADR 0003 Tenant Round Robin

## Status

Accepted

## Context

Global FIFO permits one tenant with a large burst to monopolize every worker. Global priority permits unbounded priority inflation. A first release needs understandable behavior that is easy to measure and test.

## Decision

Select the next tenant by round-robin rotation, enforce its concurrent-attempt quota, then choose priority and FIFO order within that tenant. Priority is limited to `-10` through `10`. Jobs without an eligible worker remain queued with an explicit reason.

## Consequences

Small tenants continue receiving slots during a large burst. The policy is not weighted and does not account for dominant resource fairness. Future weighting must preserve starvation tests and persist rotation state across scheduler leaders.
