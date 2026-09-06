package benchmarks

import (
	"strconv"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func BenchmarkScheduleAndComplete(b *testing.B) {
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	store := control.New(control.Config{
		LeaseTTL: time.Minute, WorkerTTL: time.Hour, DefaultTenantLimit: b.N + 1,
	})
	_, err := store.RegisterWorker(domain.Worker{
		ID: "benchmark-worker", Pool: "benchmark", Capacity: b.N + 1,
		Capabilities: map[string]bool{"linux-amd64": true},
	}, now)
	if err != nil {
		b.Fatal(err)
	}
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"test": {Capabilities: []string{"linux-amd64"}, Resources: domain.Resources{CPU: 1, Memory: 128}},
	}}

	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		b.StopTimer()
		_, err := store.CreatePipeline("tenant-"+strconv.Itoa(index%3), "acme/widget", strconv.Itoa(index), "benchmark", 0, spec, now)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		lease, err := store.ScheduleOne(now)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := store.CompleteAttempt(lease.Attempt.ID, lease.LeaseToken, true, "ok", now); err != nil {
			b.Fatal(err)
		}
	}
}
