package persistence

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestPostgresRestartConcurrencyRollback(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is required for PostgreSQL integration")
	}
	ctx := context.Background()
	key := make([]byte, 32)
	config := control.Config{}
	first, e := Open(ctx, dsn, key, config)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	second, e := Open(ctx, dsn, key, config)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	_, e = first.pool.Exec(ctx, "DELETE FROM control_state")
	if e != nil {
		t.Fatal(e)
	}
	if e = first.migrate(ctx); e != nil {
		t.Fatal(e)
	}
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{"test": {Resources: domain.Resources{CPU: 1, Memory: 64}}}}
	in := control.Submission{RequestID: "shared", Repo: "r/r", Tenant: "t", CommitSHA: "a", Spec: spec}
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := first
			if i%2 == 0 {
				p = second
			}
			e := p.Transact(ctx, true, "concurrency test", func(s *control.Store, now time.Time) error {
				v, _, e := s.Submit(in, now)
				if e == nil {
					ids <- v.Pipeline.ID
				}
				return e
			})
			if e != nil {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	close(ids)
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatal("duplicate logical pipelines", unique)
	}
	e = first.Transact(ctx, true, "rollback", func(s *control.Store, now time.Time) error {
		s.CreatePipeline("t", "r/r", "b", "test", 0, spec, now)
		return errors.New("rollback")
	})
	if e == nil {
		t.Fatal("expected rollback")
	}
	first.Close()
	reopened, e := Open(ctx, dsn, key, config)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	e = reopened.Transact(ctx, false, "", func(s *control.Store, _ time.Time) error {
		if len(s.PipelineList("")) != 1 {
			t.Fatal("state did not survive restart or rollback")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	badKey := make([]byte, 32)
	badKey[0] = 1
	wrong, e := Open(ctx, dsn, badKey, config)
	if e != nil {
		t.Fatal(e)
	}
	defer wrong.Close()
	if e = wrong.Transact(ctx, false, "", func(*control.Store, time.Time) error { return nil }); e == nil {
		t.Fatal("wrong state key was accepted")
	}
}
