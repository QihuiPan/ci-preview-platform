# ADR 0006 Preview Deletion Tombstone

## Status

Accepted

## Context

A pull request can close while its final build attempt is completing. If completion blindly creates a preview, the environment can appear after closure and remain orphaned.

## Decision

The close event records a durable PR clock and closed generation, cancels active work, and marks any preview DELETING. Preview activation checks the PR generation. The controller reports deletion only once the namespace is absent; the PR clock survives resource cleanup to reject later delivery of an older event.

## Consequences

Close-versus-complete has a deterministic safe result. Reopening requires a new generation and explicit policy rather than silently erasing the tombstone.
