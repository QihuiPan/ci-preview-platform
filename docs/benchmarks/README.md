# Benchmark Results

## Local reference run

The scheduler microbenchmark creates a one-job pipeline, selects it across three tenants, issues a lease, and commits the result. Run it with:

```bash
go test -run=^$ -bench=BenchmarkScheduleAndComplete -benchmem ./benchmarks
```

Recorded on 2026-09-06 with Go 1.26.5 on Windows AMD64 and an AMD Ryzen 9 9950X with 16 cores and 32 logical processors:

```text
BenchmarkScheduleAndComplete-32    100    4558 ns/op    837 B/op    11 allocs/op
```

The run used `-benchtime=100x` and an otherwise idle local development session. This is a reproducible reference, not a statistically complete capacity claim. Do not compare results from different power profiles or active workloads as if they were equivalent.

The separate load acceptance test leases 100 jobs across three tenants and verifies that the prefix distribution never differs by more than one slot; its final distribution is 34, 33, and 33. This in-memory test proves policy behavior, not production throughput. A production capacity claim still requires durable PostgreSQL, multiple worker processes, queue-wait histograms, hardware and warm-pool disclosure, and cost accounting.
