# Verification report

## 0.2 development verification

The local Windows environment has passed Go compilation, go vet, unit tests, the 100-job scheduler fairness regression, and Helm lint. PostgreSQL and real-cluster tests are gated by TEST_DATABASE_URL and E2E_API_URL; when these variables are absent they explicitly skip, not pass as simulated integration evidence.

The first Linux CI run on development commit dfab628 passed race-enabled tests, actual PostgreSQL restart/concurrency/rollback tests, and the container build:
https://github.com/QihuiPan/ci-preview-platform/actions/runs/34028740992

The initial cluster run failed during kind tool installation permissions, before exercising application behavior. That setup issue was corrected in e2b96e9. Cluster acceptance remains under verification; final results will be recorded here before release.

## Reproduce

- go test ./...
- go vet ./...
- TEST_DATABASE_URL=postgres://... go test -race -v ./internal/persistence
- helm lint deploy/helm/ci-preview-platform
- bash scripts/cluster-test.sh in the documented disposable kind context
- go test -run '^$' -bench . -benchtime=100x -benchmem ./benchmarks

See workflow logs for the exact runner, dependency versions, outcomes and timing. A passing unit test is not evidence of Internet DNS/TLS, a user's GitHub App permissions, registry publishing, or sustained multi-tenant production load.
