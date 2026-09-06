# ADR 0006 Preview Deletion Tombstone

## Status

Accepted

## Context

A pull request can close while its final build attempt is completing. If completion blindly creates a preview, the environment can appear after closure and remain orphaned.

## Decision

The close event first records a `DELETING` preview tombstone, then cancels active work. Preview activation checks for the tombstone and cannot replace it. Reconciliation removes the record only after a grace period; a production controller must wait until exposure and owned resources are absent.

## Consequences

Close-versus-complete has a deterministic safe result. Reopening requires a new generation and explicit policy rather than silently erasing the tombstone.
