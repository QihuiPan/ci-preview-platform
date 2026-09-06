# ADR 0001 Lease Token Compare and Set

## Status

Accepted

## Context

A worker can pause after receiving a job, lose connectivity, and later resume after the scheduler has already issued a retry. Accepting the old worker's result would overwrite newer truth and can publish duplicate artefacts or previews.

## Decision

Every attempt receives a random token and deadline. Heartbeat and completion require the attempt ID and token. Completion succeeds only while that exact attempt is active and unexpired. Retry creates a new attempt ID, number, token, and deadline. Normal pipeline reads omit tokens.

## Consequences

Late or partitioned workers receive a conflict and must discard unpublished output. Tokens become short-lived credentials and require confidential transport, redaction, hashing at rest, and rotation through retry.
