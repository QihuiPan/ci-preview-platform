package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/auth"
	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	githubapp "github.com/QihuiPan/ci-preview-platform/internal/github"
	"github.com/QihuiPan/ci-preview-platform/internal/objectstore"
	"github.com/QihuiPan/ci-preview-platform/internal/persistence"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
)

type Options struct {
	Backend       persistence.Backend
	Auth          auth.Config
	WebhookSecret string
	GitHub        *githubapp.App
	Objects       *objectstore.S3
	Logger        *slog.Logger
}
type Server struct {
	Options
	handler http.Handler
}

var errForbidden = errors.New("access denied")

func New(o Options) *Server {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	s := &Server{Options: o}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /readyz", s.ready)
	m.HandleFunc("POST /v1/webhooks/github", s.webhook)
	routes := map[string]string{
		"GET /metrics": "admin", "GET /v1/pipelines": "admin tenant", "POST /v1/pipelines": "admin tenant", "GET /v1/pipelines/{id}": "admin tenant", "POST /v1/pipelines/{id}/cancel": "admin tenant", "POST /v1/jobs/{id}/cancel": "admin tenant", "GET /v1/previews": "admin tenant",
		"POST /v1/workers/register": "worker", "POST /v1/workers/{id}/heartbeat": "worker", "GET /v1/workers/{id}/assignments": "worker", "POST /v1/attempts/{id}/heartbeat": "worker", "POST /v1/attempts/{id}/complete": "worker", "GET /v1/attempts/{id}/source-token": "worker", "PUT /v1/attempts/{id}/objects/{name}": "worker", "GET /v1/attempts/{id}/objects/{name}": "admin tenant", "GET /v1/internal/previews": "controller", "POST /v1/internal/previews/observe": "controller",
	}
	routes["GET /v1/internal/active-attempts"] = "controller"
	for route, roles := range routes {
		m.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			p, ok := s.Auth.Authenticate(r.Header.Get("Authorization"))
			if !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeError(w, 401, "A valid bearer token is required")
				return
			}
			if !strings.Contains(" "+roles+" ", " "+p.Role+" ") {
				writeError(w, 403, "Access denied")
				return
			}
			s.dispatch(w, r, p, route)
		})
	}
	slots := make(chan struct{}, 64)
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			writeError(w, 429, "Server is busy; retry with backoff")
			return
		}
		m.ServeHTTP(w, r)
	})
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }
func allowed(p auth.Principal, tenant string) bool {
	return p.Role == "admin" || p.Role == "tenant" && p.Tenant == tenant
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if e := s.Backend.Ping(ctx); e != nil {
		writeError(w, 503, "Database is unavailable")
		return
	}
	if s.Objects != nil {
		if e := s.Objects.Ping(ctx); e != nil {
			writeError(w, 503, "Object storage is unavailable")
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ready"})
}
func (s *Server) fail(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, errForbidden):
		writeError(w, 403, "Access denied")
	case errors.Is(e, control.ErrNotFound):
		writeError(w, 404, "Resource not found")
	case errors.Is(e, control.ErrStaleLease), errors.Is(e, control.ErrConflict):
		writeError(w, 409, e.Error())
	case errors.Is(e, control.ErrCapacity):
		w.Header().Set("Retry-After", "10")
		writeError(w, 429, e.Error())
	default:
		s.Logger.Error("operation failed", "error", e)
		writeError(w, 503, "Operation could not be committed; retry with the same request ID")
	}
}
func (s *Server) normalize(in *control.Submission, p auth.Principal) error {
	policy, ok := s.Auth.Repositories[in.Repo]
	if !ok || !allowed(p, policy.Tenant) {
		return errForbidden
	}
	in.Tenant = policy.Tenant
	in.Trigger = "manual"
	in.Closed = false
	in.InstallationID = policy.InstallationID
	in.EventTime = time.Time{}
	if in.SourceRepo == "" {
		in.SourceRepo = in.Repo
	}
	in.Fork = in.SourceRepo != in.Repo
	if !planner.RepositoryPattern.MatchString(in.SourceRepo) || !planner.CommitPattern.MatchString(in.CommitSHA) || in.PRNumber < 0 || in.PRNumber > 99999999 {
		return errors.New("valid source_repo, 40-character commit_sha and nonnegative PR number are required")
	}
	if in.Fork && !policy.AllowForks {
		return errForbidden
	}
	if e := planner.Executable(&in.Spec, policy.Trusted && !in.Fork); e != nil {
		return e
	}
	return validateDestinations(in.Spec, policy)
}
func validateDestinations(spec domain.PipelineSpec, p auth.Repository) error {
	for _, j := range spec.Jobs {
		if b := j.Buildkit; b != nil {
			if p.ImagePrefix == "" || !strings.HasPrefix(b.Destination, p.ImagePrefix+"/") {
				return errors.New("image destination is outside repository image_prefix")
			}
		}
	}
	return nil
}

type completion struct {
	LeaseToken string            `json:"lease_token"`
	Success    bool              `json:"success"`
	Message    string            `json:"message"`
	Digest     string            `json:"digest"`
	LogKey     string            `json:"log_key"`
	Artefacts  []domain.Artefact `json:"artefacts"`
}
type observation struct {
	Preview domain.Preview `json:"preview"`
	Actual  domain.State   `json:"actual"`
	URL     string         `json:"url"`
	Error   string         `json:"error"`
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, p auth.Principal, route string) {
	var input control.Submission
	var finish completion
	var obs observation
	if route == "POST /v1/pipelines" {
		if !decode(w, r, &input) {
			return
		}
		input.RequestID = r.Header.Get("Idempotency-Key")
		if input.RequestID == "" || len(input.RequestID) > 200 {
			writeError(w, 400, "Idempotency-Key of 1-200 characters is required")
			return
		}
		if e := s.normalize(&input, p); e != nil {
			if errors.Is(e, errForbidden) {
				s.fail(w, e)
			} else {
				writeError(w, 400, e.Error())
			}
			return
		}
	}
	if strings.HasPrefix(route, "POST /v1/attempts/") {
		if !decode(w, r, &finish) {
			return
		}
		if len(finish.Message) > 4096 || len(finish.Artefacts) > 8 || finish.Digest != "" && !planner.DigestPattern.MatchString(finish.Digest) {
			writeError(w, 400, "Invalid completion metadata")
			return
		}
		prefix := "attempts/" + r.PathValue("id") + "/"
		if finish.LogKey != "" && !validObjectKey(finish.LogKey, prefix+"logs/") {
			writeError(w, 400, "Invalid log key")
			return
		}
		for _, a := range finish.Artefacts {
			if !validObjectName(a.Name) || !validObjectKey(a.Key, prefix+a.Name+"/") || a.Size < 0 || a.Size > 16<<20 {
				writeError(w, 400, "Invalid artifact reference")
				return
			}
		}
	}
	if route == "POST /v1/internal/previews/observe" {
		if !decode(w, r, &obs) {
			return
		}
		if obs.Actual != domain.StateActive && obs.Actual != domain.StatePending && obs.Actual != domain.StateDeleted || len(obs.Error) > 4096 {
			writeError(w, 400, "Invalid preview observation")
			return
		}
	}
	write := r.Method == "POST"
	action := ""
	if write {
		action = p.Role + ":" + p.Tenant + " " + r.Method + " " + r.URL.Path
	}
	status := 200
	var out any = map[string]string{"status": "ok"}
	var pipeline domain.Pipeline
	var key string
	err := s.Backend.Transact(r.Context(), write, action, func(st *control.Store, now time.Time) error {
		id := r.PathValue("id")
		if strings.Contains(route, "/workers/{id}") && p.Worker.ID != id {
			return errForbidden
		}
		var attempt domain.Attempt
		var job domain.Job
		if strings.Contains(route, "/attempts/{id}") {
			var e error
			attempt, job, pipeline, e = st.AttemptView(id)
			if e != nil {
				return e
			}
			if p.Role == "worker" {
				if attempt.WorkerID != p.Worker.ID {
					return errForbidden
				}
			} else if !allowed(p, pipeline.Tenant) {
				return errForbidden
			}
		}
		switch route {
		case "POST /v1/pipelines":
			v, dup, e := st.Submit(input, now)
			out = map[string]any{"pipeline": v, "duplicate": dup}
			if !dup {
				status = 201
			}
			return e
		case "GET /v1/pipelines":
			tenant := p.Tenant
			if p.Role == "admin" {
				tenant = ""
			}
			out = map[string]any{"pipelines": st.PipelineList(tenant)}
		case "GET /v1/pipelines/{id}", "POST /v1/pipelines/{id}/cancel":
			v, e := st.GetPipeline(id)
			if e != nil {
				return e
			}
			if !allowed(p, v.Pipeline.Tenant) {
				return errForbidden
			}
			if r.Method == "POST" {
				for _, j := range v.Jobs {
					if _, e = st.CancelJob(j.ID, now); e != nil {
						return e
					}
				}
				v, e = st.GetPipeline(id)
			}
			out = v
			return e
		case "POST /v1/jobs/{id}/cancel":
			j, e := st.JobView(id)
			if e != nil {
				return e
			}
			if !allowed(p, j.Tenant) {
				return errForbidden
			}
			out, e = st.CancelJob(id, now)
			return e
		case "POST /v1/workers/register":
			v, e := st.RegisterWorker(*p.Worker, now)
			out = v
			return e
		case "POST /v1/workers/{id}/heartbeat":
			return st.HeartbeatWorker(id, now)
		case "GET /v1/workers/{id}/assignments":
			v, e := st.AssignedLeases(id)
			out = map[string]any{"assignments": v}
			return e
		case "POST /v1/attempts/{id}/heartbeat":
			v, e := st.HeartbeatAttempt(id, finish.LeaseToken, now)
			out = v
			return e
		case "POST /v1/attempts/{id}/complete":
			if finish.Success && job.Spec.Buildkit != nil && finish.Digest == "" {
				return control.ErrConflict
			}
			v, e := st.Finish(id, finish.LeaseToken, finish.Success, finish.Message, finish.Digest, finish.LogKey, finish.Artefacts, now)
			out = v
			return e
		case "GET /v1/attempts/{id}/source-token", "PUT /v1/attempts/{id}/objects/{name}":
			if attempt.LeaseToken != r.Header.Get("X-Lease-Token") || !now.Before(attempt.LeaseExpires) || (attempt.Status != domain.StateRunning && attempt.Status != domain.StateLeased) {
				return control.ErrStaleLease
			}
		case "GET /v1/attempts/{id}/objects/{name}":
			if r.PathValue("name") == "logs" {
				key = attempt.LogKey
			} else {
				for _, a := range attempt.Artefacts {
					if a.Name == r.PathValue("name") {
						key = a.Key
					}
				}
			}
			if key == "" {
				return control.ErrNotFound
			}
		case "GET /v1/previews", "GET /v1/internal/previews":
			v := []domain.Preview{}
			for _, preview := range st.PreviewList() {
				if p.Role == "controller" || allowed(p, preview.Tenant) {
					v = append(v, preview)
				}
			}
			out = map[string]any{"previews": v}
		case "POST /v1/internal/previews/observe":
			return st.ObservePreview(obs.Preview, obs.Actual, obs.URL, obs.Error)
		case "GET /metrics":
			out = st.Metrics()
		case "GET /v1/internal/active-attempts":
			out = map[string]any{"attempt_ids": st.ActiveAttemptIDs()}
		}
		return nil
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	// External I/O is intentionally outside the database transaction.
	switch route {
	case "GET /metrics":
		m := out.(control.Metrics)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "ci_leases_issued_total %d\nci_leases_expired_total %d\nci_jobs_completed_total %d\nci_jobs_failed_total %d\nci_jobs_cancelled_total %d\nci_webhook_accepted_total %d\nci_webhook_duplicate_total %d\n", m.LeasesIssued, m.LeasesExpired, m.JobsCompleted, m.JobsFailed, m.JobsCancelled, m.WebhookAccepted, m.WebhookDuplicate)
		return
	case "GET /v1/attempts/{id}/source-token":
		token := ""
		if s.GitHub != nil && pipeline.InstallationID != 0 && !pipeline.Fork {
			token, err = s.GitHub.Token(r.Context(), pipeline.InstallationID)
			if err != nil {
				s.fail(w, err)
				return
			}
		}
		out = map[string]string{"token": token}
	case "PUT /v1/attempts/{id}/objects/{name}":
		name := r.PathValue("name")
		if !validObjectName(name) {
			writeError(w, 400, "Unsupported artifact name")
			return
		}
		if s.Objects == nil {
			writeError(w, 503, "Object storage is not configured")
			return
		}
		limit := int64(16 << 20)
		if name == "logs" {
			limit = 1 << 20
		}
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if e != nil {
			writeError(w, 413, "Artifact exceeds size limit")
			return
		}
		key, e = s.Objects.Put(r.Context(), "attempts/"+r.PathValue("id")+"/"+name, body)
		if e != nil {
			s.fail(w, e)
			return
		}
		sum := sha256.Sum256(body)
		out = domain.Artefact{Name: name, Key: key, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: int64(len(body))}
		status = 201
	case "GET /v1/attempts/{id}/objects/{name}":
		if s.Objects == nil {
			writeError(w, 503, "Object storage is not configured")
			return
		}
		b, e := s.Objects.Get(r.Context(), key)
		if e != nil {
			s.fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("name")+`"`)
		w.Write(b)
		return
	}
	writeJSON(w, status, out)
}
func validObjectName(name string) bool {
	return name == "logs" || name == "artifacts.tar" || name == "provenance.json" || name == "build-metadata.json"
}
func validObjectKey(key, prefix string) bool {
	return strings.HasPrefix(key, prefix) && len(strings.TrimPrefix(key, prefix)) == 64 && planner.DigestPattern.MatchString("sha256:"+strings.TrimPrefix(key, prefix))
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		writeError(w, 400, "Invalid JSON: "+e.Error())
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		writeError(w, 400, "Expected one JSON value")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}
