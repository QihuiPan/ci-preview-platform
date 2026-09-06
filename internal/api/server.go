package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	githubwebhook "github.com/QihuiPan/ci-preview-platform/internal/github"
)

const maxRequestBytes = 1 << 20

// Server exposes the control-plane HTTP API.
type Server struct {
	store         *control.Store
	webhookSecret string
	logger        *slog.Logger
	now           func() time.Time
	handler       http.Handler
}

// NewServer creates a production-shaped API with health, readiness, and metrics endpoints.
func NewServer(store *control.Store, webhookSecret string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{store: store, webhookSecret: webhookSecret, logger: logger, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /readyz", server.ready)
	mux.HandleFunc("GET /metrics", server.metrics)
	mux.HandleFunc("POST /v1/webhooks/github", server.githubWebhook)
	mux.HandleFunc("POST /v1/pipelines", server.createPipeline)
	mux.HandleFunc("GET /v1/pipelines/{id}", server.getPipeline)
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", server.cancelJob)
	mux.HandleFunc("POST /v1/workers/register", server.registerWorker)
	mux.HandleFunc("POST /v1/workers/{id}/heartbeat", server.workerHeartbeat)
	mux.HandleFunc("GET /v1/workers/{id}/assignments", server.workerAssignments)
	mux.HandleFunc("POST /v1/attempts/{id}/heartbeat", server.attemptHeartbeat)
	mux.HandleFunc("POST /v1/attempts/{id}/complete", server.completeAttempt)
	mux.HandleFunc("GET /v1/previews/{repo}/{pr}", server.getPreview)
	server.handler = server.logging(mux)
	return server
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.handler.ServeHTTP(writer, request)
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) metrics(writer http.ResponseWriter, _ *http.Request) {
	metrics := s.store.Metrics()
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(writer,
		"# HELP ci_webhook_accepted_total GitHub deliveries accepted by the control plane.\n"+
			"# TYPE ci_webhook_accepted_total counter\nci_webhook_accepted_total %d\n"+
			"# HELP ci_webhook_duplicate_total Duplicate GitHub deliveries returned without new side effects.\n"+
			"# TYPE ci_webhook_duplicate_total counter\nci_webhook_duplicate_total %d\n"+
			"# HELP ci_leases_issued_total Job execution leases issued to workers.\n"+
			"# TYPE ci_leases_issued_total counter\nci_leases_issued_total %d\n"+
			"# HELP ci_leases_expired_total Active leases reconciled after their deadline.\n"+
			"# TYPE ci_leases_expired_total counter\nci_leases_expired_total %d\n"+
			"# HELP ci_stale_completions_total Worker completions rejected by lease compare-and-set.\n"+
			"# TYPE ci_stale_completions_total counter\nci_stale_completions_total %d\n"+
			"# HELP ci_jobs_completed_total Jobs completed successfully.\n"+
			"# TYPE ci_jobs_completed_total counter\nci_jobs_completed_total %d\n"+
			"# HELP ci_jobs_failed_total Jobs completed with failure.\n"+
			"# TYPE ci_jobs_failed_total counter\nci_jobs_failed_total %d\n"+
			"# HELP ci_jobs_cancelled_total Jobs cancelled before completion.\n"+
			"# TYPE ci_jobs_cancelled_total counter\nci_jobs_cancelled_total %d\n"+
			"# HELP ci_previews_created_total Preview records activated after successful pipelines.\n"+
			"# TYPE ci_previews_created_total counter\nci_previews_created_total %d\n"+
			"# HELP ci_previews_deleted_total Preview records removed after reconciliation.\n"+
			"# TYPE ci_previews_deleted_total counter\nci_previews_deleted_total %d\n",
		metrics.WebhookAccepted, metrics.WebhookDuplicate, metrics.LeasesIssued, metrics.LeasesExpired,
		metrics.StaleCompletions, metrics.JobsCompleted, metrics.JobsFailed, metrics.JobsCancelled,
		metrics.PreviewsCreated, metrics.PreviewsDeleted,
	)
}

func (s *Server) githubWebhook(writer http.ResponseWriter, request *http.Request) {
	if s.webhookSecret == "" {
		writeError(writer, http.StatusServiceUnavailable, "webhook_secret_missing", "GitHub webhook verification is not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxRequestBytes))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_body", "Request body is invalid or too large")
		return
	}
	if err := githubwebhook.VerifySignature(s.webhookSecret, body, request.Header.Get("X-Hub-Signature-256")); err != nil {
		writeError(writer, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}
	deliveryID := request.Header.Get("X-GitHub-Delivery")
	event := request.Header.Get("X-GitHub-Event")
	var payload githubPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", "Webhook body must be valid JSON")
		return
	}
	repo := payload.Repository.FullName
	if repo == "" {
		writeError(writer, http.StatusBadRequest, "missing_repository", "Webhook repository.full_name is required")
		return
	}
	tenant := strings.SplitN(repo, "/", 2)[0]
	now := s.now()

	switch event {
	case "pull_request":
		if payload.PullRequest.Number == 0 {
			writeError(writer, http.StatusBadRequest, "missing_pull_request", "Webhook pull_request.number is required")
			return
		}
		if payload.Action == "closed" {
			duplicate, err := s.store.MarkPreviewDeletingForDelivery(deliveryID, repo, payload.PullRequest.Number, now)
			if err != nil {
				s.writeStoreError(writer, err)
				return
			}
			writeJSON(writer, statusForDuplicate(duplicate), map[string]any{"duplicate": duplicate, "preview_state": domain.StateDeleting})
			return
		}
		if payload.Action != "opened" && payload.Action != "reopened" && payload.Action != "synchronize" {
			writeJSON(writer, http.StatusAccepted, map[string]string{"status": "ignored", "reason": "pull request action is not buildable"})
			return
		}
		spec := defaultPipelineSpec(!payload.PullRequest.Head.Repository.Fork, true)
		view, duplicate, err := s.store.CreatePipelineForDelivery(deliveryID, tenant, repo, payload.PullRequest.Head.SHA, event, payload.PullRequest.Number, spec, now)
		if err != nil {
			s.writeStoreError(writer, err)
			return
		}
		writeJSON(writer, statusForDuplicate(duplicate), map[string]any{"duplicate": duplicate, "pipeline": view})
	case "push":
		view, duplicate, err := s.store.CreatePipelineForDelivery(deliveryID, tenant, repo, payload.After, event, 0, defaultPipelineSpec(true, false), now)
		if err != nil {
			s.writeStoreError(writer, err)
			return
		}
		writeJSON(writer, statusForDuplicate(duplicate), map[string]any{"duplicate": duplicate, "pipeline": view})
	default:
		writeJSON(writer, http.StatusAccepted, map[string]string{"status": "ignored", "reason": "event type is not supported"})
	}
}

func (s *Server) createPipeline(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Tenant    string              `json:"tenant"`
		Repo      string              `json:"repo"`
		CommitSHA string              `json:"commit_sha"`
		Trigger   string              `json:"trigger"`
		PRNumber  int                 `json:"pr_number"`
		Spec      domain.PipelineSpec `json:"spec"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.Tenant == "" || input.Repo == "" || input.CommitSHA == "" {
		writeError(writer, http.StatusBadRequest, "missing_fields", "tenant, repo, and commit_sha are required")
		return
	}
	if input.Trigger == "" {
		input.Trigger = "manual"
	}
	view, err := s.store.CreatePipeline(input.Tenant, input.Repo, input.CommitSHA, input.Trigger, input.PRNumber, input.Spec, s.now())
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, view)
}

func (s *Server) getPipeline(writer http.ResponseWriter, request *http.Request) {
	view, err := s.store.GetPipeline(request.PathValue("id"))
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func (s *Server) cancelJob(writer http.ResponseWriter, request *http.Request) {
	job, err := s.store.CancelJob(request.PathValue("id"), s.now())
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, job)
}

func (s *Server) registerWorker(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		ID           string   `json:"id"`
		Pool         string   `json:"pool"`
		Capabilities []string `json:"capabilities"`
		Capacity     int      `json:"capacity"`
		Trusted      bool     `json:"trusted"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	capabilities := make(map[string]bool, len(input.Capabilities))
	for _, capability := range input.Capabilities {
		capabilities[capability] = true
	}
	worker, err := s.store.RegisterWorker(domain.Worker{
		ID: input.ID, Pool: input.Pool, Capabilities: capabilities, Capacity: input.Capacity, Trusted: input.Trusted,
	}, s.now())
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, worker)
}

func (s *Server) workerHeartbeat(writer http.ResponseWriter, request *http.Request) {
	if err := s.store.HeartbeatWorker(request.PathValue("id"), s.now()); err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) workerAssignments(writer http.ResponseWriter, request *http.Request) {
	assignments, err := s.store.AssignedLeases(request.PathValue("id"))
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"assignments": assignments})
}

func (s *Server) attemptHeartbeat(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		LeaseToken string `json:"lease_token"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	attempt, err := s.store.HeartbeatAttempt(request.PathValue("id"), input.LeaseToken, s.now())
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, attempt)
}

func (s *Server) completeAttempt(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		LeaseToken string `json:"lease_token"`
		Success    bool   `json:"success"`
		Message    string `json:"message"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	attempt, err := s.store.CompleteAttempt(request.PathValue("id"), input.LeaseToken, input.Success, input.Message, s.now())
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, attempt)
}

func (s *Server) getPreview(writer http.ResponseWriter, request *http.Request) {
	prNumber, err := strconv.Atoi(request.PathValue("pr"))
	if err != nil || prNumber <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid_pull_request", "pr must be a positive integer")
		return
	}
	preview, err := s.store.GetPreview(request.PathValue("repo"), prNumber)
	if err != nil {
		s.writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, preview)
}

func (s *Server) writeStoreError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, control.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, control.ErrStaleLease), errors.Is(err, control.ErrConflict):
		writeError(writer, http.StatusConflict, "state_conflict", err.Error())
	default:
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
	}
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		start := time.Now()
		next.ServeHTTP(writer, request)
		s.logger.Info("http request", "method", request.Method, "path", request.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func statusForDuplicate(duplicate bool) int {
	if duplicate {
		return http.StatusOK
	}
	return http.StatusAccepted
}

type githubPayload struct {
	Action     string `json:"action"`
	After      string `json:"after"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number int `json:"number"`
		Head   struct {
			SHA        string `json:"sha"`
			Repository struct {
				Fork bool `json:"fork"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

func defaultPipelineSpec(trusted, includePreview bool) domain.PipelineSpec {
	jobs := map[string]domain.JobSpec{
		"test": {
			Image: "golang:1.26.5", Command: []string{"go", "test", "./..."},
			Capabilities: []string{"linux-amd64"}, Trusted: trusted,
			Resources: domain.Resources{CPU: 1, Memory: 1024},
		},
		"image": {
			Image: "moby/buildkit:rootless", Command: []string{"buildctl-daemonless.sh", "build"},
			Needs: []string{"test"}, Capabilities: []string{"linux-amd64", "buildkit-rootless"}, Trusted: trusted,
			Resources: domain.Resources{CPU: 2, Memory: 2048},
		},
	}
	if includePreview {
		jobs["preview"] = domain.JobSpec{
			Image: "alpine:3", Command: []string{"true"}, Needs: []string{"image"},
			Capabilities: []string{"linux-amd64"}, Trusted: trusted,
			Resources:   domain.Resources{CPU: 1, Memory: 128},
			Environment: &domain.Environment{TTLMinutes: 24 * 60, Exposure: "public"},
		}
	}
	return domain.PipelineSpec{Version: 1, Jobs: jobs}
}
