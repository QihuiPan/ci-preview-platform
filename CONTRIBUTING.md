# Contributing

## Development requirements

- Use Go 1.26 or newer.
- Keep source comments, annotations, commit messages, API messages, documentation, and changelog entries in English.
- Add an English `CHANGELOG.md` entry for every user-visible implementation update.
- Do not commit credentials, webhook payloads containing private data, generated binaries, or local benchmark profiles.

## Required checks

Run these commands before opening a pull request:

```bash
go fmt ./cmd/... ./internal/... ./tests/... ./benchmarks/...
go vet ./...
go test -race ./...
go build ./cmd/...
```

Changes to state transitions, leases, cancellation, fairness, or preview reconciliation must include a deterministic failure-case test. Changes to public behavior must update the API documentation and changelog.
