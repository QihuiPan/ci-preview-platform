# Test Report

## Release candidate

- Version: 0.1.0
- Date: 2026-09-06
- Go: 1.26.5
- Local platform: Windows AMD64
- CPU: AMD Ryzen 9 9950X, 16 cores, 32 logical processors

## Static and build verification

| Check | Result |
| --- | --- |
| `go fmt` across commands, internal packages, tests, and benchmarks | Passed |
| `go vet` across commands, internal packages, tests, and benchmarks | Passed |
| `go build ./cmd/...` | Passed |
| Non-ASCII source and documentation scan | Passed with no matches |
| `TODO`, `FIXME`, and `XXX` scan | Passed with no matches |

## Automated tests

The complete local suite passed five consecutive times with randomized package test order:

```text
go test -shuffle=on -count=5 ./cmd/... ./internal/... ./tests/... ./benchmarks/...
```

Representative package statement coverage from a fresh single run:

| Package | Coverage |
| --- | ---: |
| `internal/api` | 43.8% |
| `internal/cache` | 72.7% |
| `internal/control` | 67.6% |
| `internal/github` | 76.9% |
| `internal/planner` | 85.1% |

The end-to-end and load packages validate behavior across public package boundaries and therefore report no local statements of their own.

## Load and fairness evidence

`TestHundredConcurrentJobsRemainFair` creates 100 runnable jobs for three tenants, leases all 100 concurrently to one synthetic capacity pool, and checks every assignment prefix. The difference between the most- and least-served tenant never exceeds one slot; the final distribution is 34, 33, and 33.

## Manual process test

The compiled API and worker simulator were started as separate local processes. A three-stage pipeline from `config/example.pipeline.json` completed in dependency order:

```text
test:SUCCEEDED,image:SUCCEEDED,preview:SUCCEEDED
pipeline:SUCCEEDED
preview desired/actual:ACTIVE/ACTIVE
preview URL:https://preview-acme-widget-184.preview.local
metrics HTTP status:200
```

The processes were stopped after verification.

## Race detector

The local portable Windows toolchain does not include a C compiler, so `go test -race` cannot run in this environment. `.github/workflows/ci.yml` makes the Linux race-enabled suite a required implementation check on every push and pull request. A passing GitHub Actions run is therefore the release gate for race instrumentation.

## Container verification

The local Docker Desktop backend could not start because its host-owned socket was locked, so no local container build result is claimed. The Docker process started for the check was shut down without changing its data. The GitHub Actions release gate validates the Compose model and builds the control-plane image on Linux.

## Benchmark

The scheduler reference result is documented in [`benchmarks/README.md`](benchmarks/README.md). The microbenchmark is not presented as a production throughput or SLO claim.
