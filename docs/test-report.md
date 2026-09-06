# Verification report

## 0.2.0 release verification

The executable runtime was verified at commit bcc9e3826beafca8e93e431b56a590f5fa3ceabd on September 6, 2026. The release preparation commit changes documentation only.

| Gate | Result | Evidence |
| --- | --- | --- |
| Linux CI | Passed | [Completed CI run](https://github.com/QihuiPan/ci-preview-platform/actions/runs/34031506553) |
| Fresh two-node kind installation and actual lifecycle | Passed | [Completed cluster acceptance run](https://github.com/QihuiPan/ci-preview-platform/actions/runs/34031506585) |
| Local Terraform configuration | Passed formatting and validation | Terraform 1.16.1, signed Helm provider 3.3.0; no Terraform apply was performed |

Linux CI ran static analysis, reachable Go vulnerability scanning, race-enabled tests with a real PostgreSQL 17.11 service, command compilation, Compose validation, and a container build. The Go 1.26.8 vulnerability scan reported no reachable vulnerabilities in this project's Go packages. This is not a vulnerability scan of every external container image or a security audit.

The real cluster lifecycle completed in 161.151 seconds after image building and Helm installation. It verified immutable Git checkout, two actual command containers, Kubernetes API egress denial, committed logs and artifact downloads through MinIO, preview application readiness and HTTP response, API rollout/restart persistence, actual process exit code 7, cancellation after a container began executing, removal of its namespace, and eventual preview TTL cleanup. The disposable cluster was removed afterward.

PostgreSQL tests used two independent backend pools and 20 concurrent submissions to verify one logical pipeline, rollback, reopen/recovery, and rejection of an incorrect encryption key. Unit regressions cover lease fencing, bounded retries, cross-tenant authorization, PR event ordering, artifact boundaries, preview rollout readiness, and 100-job in-memory scheduler fairness.

## Local and release assets

Local Windows checks passed compilation, unit tests, go vet, the reachable Go vulnerability scan, and Helm lint. Database and cluster tests explicitly skip locally without TEST_DATABASE_URL and E2E_API_URL; their successful integration evidence comes from the Linux workflows above.

The release includes cross-compiled Windows, Linux, and macOS CLI binaries for AMD64 and ARM64, a packaged Helm chart, source archive, setup notes, and SHA-256 checksums. Windows AMD64 and Linux AMD64 under WSL2 passed version/help execution smoke tests. No macOS or ARM device execution test is claimed.

## Reproduce

- go test ./...
- go vet ./...
- go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
- TEST_DATABASE_URL=postgres://... go test -race -v ./internal/persistence
- helm lint deploy/helm/ci-preview-platform
- bash scripts/cluster-test.sh in the documented disposable kind context
- terraform -chdir=infra/terraform init -backend=false
- terraform -chdir=infra/terraform validate

Use disposable resources for integration testing, never a production database. Read the installation guide for the enforcing CNI and image prerequisites.

## Not verified by these gates

A passing workflow is not evidence of customer GitHub App permissions, private registry publishing with customer credentials, public DNS/TLS, macOS/ARM execution, hostile-tenant isolation, high availability, or sustained production throughput. BuildKit publishing and App adapters exist, but their real credential-dependent setup remains an operator acceptance task. Read known-limitations.md before deployment.

Earlier clean-install, checkout-fixture, and test port-forward failures were corrected and are documented in CHANGELOG.md. They are not counted as passing runs.
