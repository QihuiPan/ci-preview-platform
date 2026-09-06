# Scheduler benchmark

Reproduce with:

```bash
go test -run '^$' -bench . -benchtime=100x -benchmem ./benchmarks
```

Local sample on 2026-09-06: Windows/amd64, Go 1.26.5, AMD Ryzen 9 9950X, 100 iterations: 7,811 ns/op, 885 B/op, 11 allocations/op for the in-memory schedule-and-complete transition. Pipeline creation is outside the timed section. History grows through the bounded sample.

This is a microbenchmark, not end-to-end throughput, PostgreSQL transaction latency, build latency, or a promise about 100 simultaneous Kubernetes jobs. The separate fairness regression distributes 100 queued jobs across three tenants as 34/33/33. Real-cluster acceptance exercises a small lifecycle and does not replace capacity testing on the target infrastructure.

The encrypted snapshot model rewrites state on mutation; measure retained state size and database latency before scaling beyond a small team.
