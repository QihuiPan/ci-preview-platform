package load

import (
	"fmt"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestHundredConcurrentJobsRemainFair(t *testing.T) {
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	store := control.New(control.Config{
		LeaseTTL: time.Minute, WorkerTTL: time.Minute, DefaultTenantLimit: 100,
	})
	_, err := store.RegisterWorker(domain.Worker{
		ID: "load-worker", Pool: "load", Capacity: 100,
		Capabilities: map[string]bool{"linux-amd64": true},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"test": {Capabilities: []string{"linux-amd64"}, Resources: domain.Resources{CPU: 1, Memory: 128}},
	}}
	tenants := []string{"alpha", "beta", "gamma"}
	for index := range 100 {
		tenant := tenants[index%len(tenants)]
		if _, err := store.CreatePipeline(tenant, tenant+"/repo", fmt.Sprintf("%040d", index), "load", 0, spec, now); err != nil {
			t.Fatal(err)
		}
	}

	counts := map[string]int{}
	leasing := make([]domain.AttemptLease, 0, 100)
	for index := range 100 {
		lease, err := store.ScheduleOne(now.Add(time.Duration(index) * time.Microsecond))
		if err != nil {
			t.Fatalf("ScheduleOne(%d) error = %v", index, err)
		}
		leasing = append(leasing, lease)
		counts[lease.Pipeline.Tenant]++
		minimum, maximum := counts[tenants[0]], counts[tenants[0]]
		for _, tenant := range tenants[1:] {
			if counts[tenant] < minimum {
				minimum = counts[tenant]
			}
			if counts[tenant] > maximum {
				maximum = counts[tenant]
			}
		}
		if maximum-minimum > 1 {
			t.Fatalf("tenant counts diverged at assignment %d: %#v", index+1, counts)
		}
	}
	if counts["alpha"] != 34 || counts["beta"] != 33 || counts["gamma"] != 33 {
		t.Fatalf("final tenant distribution = %#v, want 34/33/33", counts)
	}
	for _, lease := range leasing {
		if _, err := store.CompleteAttempt(lease.Attempt.ID, lease.LeaseToken, true, "load success", now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
}
