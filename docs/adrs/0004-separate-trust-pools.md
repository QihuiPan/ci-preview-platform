# ADR 0004 Separate Trust Pools

## Status

Accepted

## Context

Fork pull requests contain untrusted code and must not inherit repository deployment credentials or privileged build capabilities. A boolean policy applied after assignment is too late.

## Decision

The operator repository policy determines trust, and the authenticated worker principal determines its pool, capabilities and capacity. The scheduler requires an exact trust match before issuing a lease. Forks are always untrusted and cannot publish images. A production deployment must also isolate node pools, identities, networks, secrets, and object-store prefixes.

## Consequences

Trust is part of scheduling eligibility and is visible in tests. Version 0.2 removes unauthenticated registration and ignores caller-provided trust. Management bearer tokens remain operator secrets; workload mTLS and attested worker identities are outside this release.
