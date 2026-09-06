package control

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestSnapshotPreservesLeaseAndPRClock(t *testing.T) {
	s := newTestStore()
	now := time.Now()
	registerTestWorker(t, s, "worker", 1, now)
	in := Submission{RequestID: "open", Tenant: "tenant", Repo: "owner/repo", CommitSHA: strings.Repeat("a", 40), PRNumber: 1, EventTime: now, Spec: singleJobSpec()}
	v, _, e := s.Submit(in, now)
	if e != nil {
		t.Fatal(e)
	}
	l, e := s.ScheduleOne(now)
	if e != nil {
		t.Fatal(e)
	}
	data, e := s.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	restored, e := Restore(s.config, data)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = restored.CompleteAttempt(l.Attempt.ID, l.LeaseToken, true, "restored", now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	result, _ := restored.GetPipeline(v.Pipeline.ID)
	if result.Pipeline.Status != domain.StateSucceeded {
		t.Fatal(result.Pipeline.Status)
	}
	in.RequestID = "close"
	in.Closed = true
	in.EventTime = now.Add(time.Minute)
	if _, _, e = restored.Submit(in, now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	in.Closed = false
	in.RequestID = "delayed"
	in.EventTime = now.Add(30 * time.Second)
	out, dup, e := restored.Submit(in, now.Add(time.Hour))
	if e != nil || !dup || out.Pipeline.ID != "" {
		t.Fatal("late event resurrected a closed PR", e, out)
	}
}
func TestFailureReleasesParallelSlots(t *testing.T) {
	s := newTestStore()
	now := time.Now()
	registerTestWorker(t, s, "worker", 2, now)
	spec := singleJobSpec()
	spec.Jobs["other"] = spec.Jobs["test"]
	s.CreatePipeline("t", "r/r", "a", "manual", 0, spec, now)
	first, _ := s.ScheduleOne(now)
	second, _ := s.ScheduleOne(now)
	if _, e := s.CompleteAttempt(first.Attempt.ID, first.LeaseToken, false, "failed", now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	a, _, _, _ := s.AttemptView(second.Attempt.ID)
	if a.Status != domain.StateCancelled {
		t.Fatal("parallel attempt not cancelled")
	}
	if used := s.usedResourcesLocked("t", ""); used.CPU != 0 {
		t.Fatal("capacity leak", used)
	}
}
func TestRetryBudgetAndIdempotencyMismatch(t *testing.T) {
	s := newTestStore()
	now := time.Now()
	registerTestWorker(t, s, "worker", 1, now)
	spec := singleJobSpec()
	j := spec.Jobs["test"]
	j.MaxAttempts = 1
	spec.Jobs["test"] = j
	in := Submission{RequestID: "one", Tenant: "t", Repo: "r/r", CommitSHA: "a", Spec: spec}
	v, _, _ := s.Submit(in, now)
	s.ScheduleOne(now)
	s.Reconcile(now.Add(11 * time.Second))
	final, _ := s.GetPipeline(v.Pipeline.ID)
	if final.Pipeline.Status != domain.StateFailed {
		t.Fatal(final.Pipeline.Status)
	}
	in.CommitSHA = "different"
	if _, _, e := s.Submit(in, now); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
}
func TestPreviewRequiresObservedReadiness(t *testing.T) {
	s := newTestStore()
	now := time.Now()
	registerTestWorker(t, s, "worker", 1, now)
	spec := singleJobSpec()
	j := spec.Jobs["test"]
	j.Environment = &domain.Environment{TTLMinutes: 1, Image: "example/image@sha256:" + strings.Repeat("a", 64)}
	spec.Jobs["test"] = j
	s.Submit(Submission{RequestID: "pr", Repo: "r/r", Tenant: "t", CommitSHA: "a", PRNumber: 1, EventTime: now, Spec: spec}, now)
	l, _ := s.ScheduleOne(now)
	s.CompleteAttempt(l.Attempt.ID, l.LeaseToken, true, "done", now)
	p, e := s.GetPreview("r/r", 1)
	if e != nil || p.Actual != domain.StatePending || p.URL != "" {
		t.Fatal(p, e)
	}
	if e = s.ObservePreview(p, domain.StateActive, "https://real.example", ""); e != nil {
		t.Fatal(e)
	}
	s.Reconcile(now.Add(2 * time.Minute))
	p, _ = s.GetPreview("r/r", 1)
	s.ObservePreview(p, domain.StateDeleted, "", "")
	s.Reconcile(now.Add(2 * time.Minute))
	s.EnsurePreviews(now.Add(2 * time.Minute))
	if _, e = s.GetPreview("r/r", 1); !errors.Is(e, ErrNotFound) {
		t.Fatal("expired preview was recreated", e)
	}
}
