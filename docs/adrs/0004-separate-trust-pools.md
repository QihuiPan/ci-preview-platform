# ADR 0004 Separate Trust Pools

## Status

Accepted

## Context

Fork pull requests contain untrusted code and must not inherit repository deployment credentials or privileged build capabilities. A boolean policy applied after assignment is too late.

## Decision

Jobs declare whether trusted execution is required, and workers advertise their trust pool and capabilities. The scheduler checks both before issuing a lease. Fork-generated default plans do not request protected trust. A production deployment must also isolate node pools, identities, networks, secrets, and object-store prefixes.

## Consequences

Trust becomes part of scheduling eligibility and is visible in tests. The version 0.1.0 worker registration endpoint is unauthenticated, so its trust claim is suitable only for local development until workload identity is added.
