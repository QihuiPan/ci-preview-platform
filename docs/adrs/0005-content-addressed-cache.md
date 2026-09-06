# ADR 0005 Content Addressed Cache

## Status

Accepted

## Context

Mutable cache names allow unrelated toolchains or lockfiles to collide, while unsafe archive extraction can overwrite files or exhaust storage.

## Decision

Cache keys hash repository, toolchain, lockfile digest, and cache version with explicit separators. Archive entries are normalized and rejected when absolute, traversal-based, drive-qualified, negative-sized, or over the per-object limit. Production extraction must add aggregate size, file count, link, device, and compression-ratio controls.

## Consequences

Cache identity is deterministic and immutable. Changing any input deliberately produces a cold cache. Garbage collection needs a separate reachability and retention policy.
