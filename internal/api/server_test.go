package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/auth"
	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	githubapp "github.com/QihuiPan/ci-preview-platform/internal/github"
	"github.com/QihuiPan/ci-preview-platform/internal/persistence"
)

func fixture() (*Server, *persistence.Memory) {
	backend := &persistence.Memory{Store: control.New(control.Config{})}
	policy := auth.Config{Principals: []auth.Principal{{Token: strings.Repeat("a", 32), Role: "admin"}, {Token: strings.Repeat("t", 32), Role: "tenant", Tenant: "acme"}, {Token: strings.Repeat("x", 32), Role: "tenant", Tenant: "other"}, {Token: strings.Repeat("w", 32), Role: "worker", Worker: &domain.Worker{ID: "worker", Pool: "untrusted", Capacity: 2, Resources: domain.Resources{CPU: 4, Memory: 4096}}}}, Repositories: map[string]auth.Repository{"acme/widget": {Tenant: "acme"}}}
	return New(Options{Backend: backend, Auth: policy, GitHub: &githubapp.App{}, WebhookSecret: "secret", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}), backend
}
func request(s *Server, method, path, token, key string, payload any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(payload)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+strings.Repeat(token, 32))
	}
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func submission() control.Submission {
	return control.Submission{Repo: "acme/widget", CommitSHA: strings.Repeat("a", 40), Spec: domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{"test": {Image: "alpine:3.21", Command: []string{"true"}, Resources: domain.Resources{CPU: 1, Memory: 128}, Trusted: true}}}}
}
func TestAuthorizationAndAuthoritativeTrust(t *testing.T) {
	s, _ := fixture()
	for _, tc := range []struct {
		method, path, token string
		status              int
	}{{"GET", "/healthz", "", 200}, {"GET", "/readyz", "", 200}, {"GET", "/v1/pipelines", "", 401}, {"GET", "/metrics", "t", 403}, {"GET", "/v1/workers/not-worker/assignments", "w", 403}, {"POST", "/v1/workers/register", "t", 403}, {"POST", "/v1/pipelines", "x", 403}} {
		w := request(s, tc.method, tc.path, tc.token, "req", submission())
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	w := request(s, "POST", "/v1/pipelines", "t", "create", submission())
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	var out struct {
		Pipeline domain.PipelineView `json:"pipeline"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.Pipeline.Jobs[0].Spec.Trusted {
		t.Fatal("client escalated trust")
	}
	if request(s, "GET", "/v1/pipelines/"+out.Pipeline.Pipeline.ID, "x", "", nil).Code != 403 {
		t.Fatal("cross-tenant read allowed")
	}
}
func TestIdempotencyConflictAndImmutableRevision(t *testing.T) {
	s, _ := fixture()
	in := submission()
	if w := request(s, "POST", "/v1/pipelines", "t", "same", in); w.Code != 201 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(s, "POST", "/v1/pipelines", "t", "same", in); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	in.CommitSHA = strings.Repeat("b", 40)
	if w := request(s, "POST", "/v1/pipelines", "t", "same", in); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	in.CommitSHA = "main"
	if w := request(s, "POST", "/v1/pipelines", "t", "new", in); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
}
func TestWorkerCompletionFence(t *testing.T) {
	s, b := fixture()
	request(s, "POST", "/v1/workers/register", "w", "", nil)
	request(s, "POST", "/v1/pipelines", "t", "new", submission())
	var lease domain.AttemptLease
	b.Transact(context.Background(), true, "", func(st *control.Store, now time.Time) error { var e error; lease, e = st.ScheduleOne(now); return e })
	path := "/v1/attempts/" + lease.Attempt.ID
	if w := request(s, "POST", path+"/complete", "w", "", completion{LeaseToken: "wrong", Success: true}); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(s, "POST", path+"/heartbeat", "w", "", completion{LeaseToken: lease.LeaseToken}); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	finish := completion{LeaseToken: lease.LeaseToken, Success: true, Message: "done"}
	for range 2 {
		if w := request(s, "POST", path+"/complete", "w", "", finish); w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if w := request(s, "GET", "/v1/pipelines/"+lease.Pipeline.ID, "t", "", nil); strings.Contains(w.Body.String(), lease.LeaseToken) {
		t.Fatal("lease secret leaked")
	}
}
func TestWebhookBadSignatureAndStrictJSON(t *testing.T) {
	s, _ := fixture()
	w := request(s, "POST", "/v1/webhooks/github", "", "", map[string]any{})
	if w.Code != 401 {
		t.Fatal(w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "/v1/pipelines", strings.NewReader(`{"unknown":true}`))
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 32))
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
}
