package control

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestCreatePipelineForDeliveryIsIdempotent(t *testing.T) {
	store := newTestStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	first, duplicate, err := store.CreatePipelineForDelivery("delivery-1", "tenant", "owner/repo", "abc", "pull_request", 7, singleJobSpec(), now)
	if err != nil || duplicate {
		t.Fatalf("first delivery = duplicate %v, error %v", duplicate, err)
	}
	second, duplicate, err := store.CreatePipelineForDelivery("delivery-1", "tenant", "owner/repo", "abc", "pull_request", 7, singleJobSpec(), now)
	if err != nil || !duplicate {
		t.Fatalf("second delivery = duplicate %v, error %v", duplicate, err)
	}
	if first.Pipeline.ID != second.Pipeline.ID {
		t.Fatalf("duplicate created pipeline %q; first was %q", second.Pipeline.ID, first.Pipeline.ID)
	}
}

func TestSchedulerRotatesAcrossTenants(t *testing.T) {
	store := newTestStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	registerTestWorker(t, store, "worker", 10, now)
	for _, tenant := range []string{"alpha", "beta", "gamma"} {
		if _, err := store.CreatePipeline(tenant, tenant+"/repo", "abc", "manual", 0, singleJobSpec(), now); err != nil {
			t.Fatalf("CreatePipeline(%q) error = %v", tenant, err)
		}
	}
	for index, want := range []string{"alpha", "beta", "gamma"} {
		lease, err := store.ScheduleOne(now.Add(time.Duration(index) * time.Millisecond))
		if err != nil {
			t.Fatalf("ScheduleOne(%d) error = %v", index, err)
		}
		if lease.Pipeline.Tenant != want {
			t.Fatalf("ScheduleOne(%d) tenant = %q, want %q", index, lease.Pipeline.Tenant, want)
		}
	}
}

func TestTenantQuotaDoesNotBlockAnotherTenant(t *testing.T) {
	store := New(Config{
		LeaseTTL: time.Minute, WorkerTTL: time.Minute, DefaultTenantLimit: 1,
	})
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	registerTestWorker(t, store, "worker", 3, now)
	for _, tenant := range []string{"alpha", "alpha", "beta"} {
		if _, err := store.CreatePipeline(tenant, tenant+"/repo", "abc", "manual", 0, singleJobSpec(), now); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ScheduleOne(now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ScheduleOne(now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Pipeline.Tenant != "alpha" || second.Pipeline.Tenant != "beta" {
		t.Fatalf("scheduled tenants = %q, %q; want alpha, beta", first.Pipeline.Tenant, second.Pipeline.Tenant)
	}
}

func TestTrustedJobUsesTrustedWorker(t *testing.T) {
	store := newTestStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	registerTestWorker(t, store, "a-untrusted", 1, now)
	_, err := store.RegisterWorker(domain.Worker{
		ID: "b-trusted", Pool: "trusted", Capacity: 1, Trusted: true,
		Capabilities: map[string]bool{"linux-amd64": true},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	spec := singleJobSpec()
	job := spec.Jobs["test"]
	job.Trusted = true
	spec.Jobs["test"] = job
	if _, err := store.CreatePipeline("tenant", "owner/repo", "abc", "manual", 0, spec, now); err != nil {
		t.Fatal(err)
	}
	lease, err := store.ScheduleOne(now)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Attempt.WorkerID != "b-trusted" {
		t.Fatalf("worker ID = %q, want b-trusted", lease.Attempt.WorkerID)
	}
}

func TestExpiredLeaseRetriesAndRejectsStaleCompletion(t *testing.T) {
	store := newTestStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	registerTestWorker(t, store, "worker-a", 1, now)
	registerTestWorker(t, store, "worker-b", 1, now)
	view, err := store.CreatePipeline("tenant", "owner/repo", "abc", "manual", 0, singleJobSpec(), now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ScheduleOne(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.HeartbeatAttempt(first.Attempt.ID, first.LeaseToken, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	retryTime := now.Add(12 * time.Second)
	store.Reconcile(retryTime)
	second, err := store.ScheduleOne(retryTime)
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempt.Number != 2 {
		t.Fatalf("retry attempt number = %d, want 2", second.Attempt.Number)
	}
	if _, err := store.CompleteAttempt(first.Attempt.ID, first.LeaseToken, true, "late result", retryTime); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("stale completion error = %v, want ErrStaleLease", err)
	}
	if _, err := store.CompleteAttempt(second.Attempt.ID, second.LeaseToken, true, "ok", retryTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	final, err := store.GetPipeline(view.Pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Pipeline.Status != domain.StateSucceeded {
		t.Fatalf("pipeline status = %s, want SUCCEEDED", final.Pipeline.Status)
	}
}

func TestCancelCompletionRaceHasOneTerminalOutcome(t *testing.T) {
	store := newTestStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	registerTestWorker(t, store, "worker", 1, now)
	view, err := store.CreatePipeline("tenant", "owner/repo", "abc", "manual", 0, singleJobSpec(), now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.ScheduleOne(now)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, _ = store.CancelJob(view.Jobs[0].ID, now.Add(time.Second))
	}()
	go func() {
		defer wait.Done()
		<-start
		_, _ = store.CompleteAttempt(lease.Attempt.ID, lease.LeaseToken, true, "ok", now.Add(time.Second))
	}()
	close(start)
	wait.Wait()

	final, err := store.GetPipeline(view.Pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Pipeline.Status != domain.StateSucceeded && final.Pipeline.Status != domain.StateCancelled {
		t.Fatalf("pipeline status = %s, want one terminal outcome", final.Pipeline.Status)
	}
	if final.Jobs[0].Status != final.Pipeline.Status {
		t.Fatalf("job status %s disagrees with pipeline status %s", final.Jobs[0].Status, final.Pipeline.Status)
	}
}

func TestClosedPreviewTombstonePreventsLateActivation(t *testing.T) {
	store := newTestStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	registerTestWorker(t, store, "worker", 1, now)
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"preview": {Environment: &domain.Environment{TTLMinutes: 60}, Resources: domain.Resources{CPU: 1, Memory: 128}},
	}}
	if _, err := store.CreatePipeline("tenant", "owner/repo", "abc", "pull_request", 42, spec, now); err != nil {
		t.Fatal(err)
	}
	lease, err := store.ScheduleOne(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkPreviewDeletingForDelivery("close-delivery", "owner/repo", 42, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteAttempt(lease.Attempt.ID, lease.LeaseToken, true, "late", now.Add(2*time.Second)); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("CompleteAttempt() error = %v, want ErrStaleLease", err)
	}
	preview, err := store.GetPreview("owner/repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Desired != domain.StateDeleting {
		t.Fatalf("preview desired state = %s, want DELETING", preview.Desired)
	}
}

func newTestStore() *Store {
	return New(Config{
		LeaseTTL: 10 * time.Second, WorkerTTL: time.Minute, DefaultTenantLimit: 10,
		PreviewBaseDomain: "preview.test", PreviewDeleteDelay: 5 * time.Second,
	})
}

func registerTestWorker(t *testing.T, store *Store, id string, capacity int, now time.Time) {
	t.Helper()
	_, err := store.RegisterWorker(domain.Worker{
		ID: id, Pool: "untrusted", Capacity: capacity,
		Capabilities: map[string]bool{"linux-amd64": true},
	}, now)
	if err != nil {
		t.Fatalf("RegisterWorker() error = %v", err)
	}
}

func singleJobSpec() domain.PipelineSpec {
	return domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"test": {Capabilities: []string{"linux-amd64"}, Resources: domain.Resources{CPU: 1, Memory: 128}},
	}}
}
