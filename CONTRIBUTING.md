# Contributing

Use Go 1.26.8. Run gofmt, go vet ./..., and go test ./... before submitting a change. Linux CI also runs race detection and real PostgreSQL tests; cluster acceptance uses a disposable kind cluster with enforcing Calico policies.

Every change commit must include an English CHANGELOG.md entry explaining added behavior, changed compatibility or fixed defects. All code comments, annotations, examples and documentation must be English.

Preserve API authorization and repository trust boundaries. Add a regression test for a correctness or security fix. Do not introduce a simulator fallback into the deployment runtime or claim a skipped integration test as passed.

Never commit App private keys, bearer tokens, database credentials, registry config, Terraform state or generated Secrets. Use synthetic values in tests and keep a security report private.

Describe the actual verification performed and its limits. Changes to persistence, controller ownership, key handling or destructive cleanup require explicit upgrade and recovery notes.
