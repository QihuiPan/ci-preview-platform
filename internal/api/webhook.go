package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	githubapp "github.com/QihuiPan/ci-preview-platform/internal/github"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
)

type githubPayload struct {
	Action       string `json:"action"`
	After        string `json:"after"`
	Deleted      bool   `json:"deleted"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number    int       `json:"number"`
		UpdatedAt time.Time `json:"updated_at"`
		Head      struct {
			SHA        string `json:"sha"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if s.WebhookSecret == "" || s.GitHub == nil {
		writeError(w, 503, "GitHub App integration is not configured")
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if e != nil {
		writeError(w, 413, "Webhook exceeds 1 MiB")
		return
	}
	if e = githubapp.VerifySignature(s.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")); e != nil {
		writeError(w, 401, "Invalid webhook signature")
		return
	}
	var payload githubPayload
	if json.Unmarshal(body, &payload) != nil {
		writeError(w, 400, "Invalid webhook JSON")
		return
	}
	repo := payload.Repository.FullName
	policy, ok := s.Auth.Repositories[repo]
	if !ok || policy.InstallationID == 0 || policy.InstallationID != payload.Installation.ID {
		writeError(w, 403, "Repository installation is not authorized")
		return
	}
	in := control.Submission{RequestID: "github:" + r.Header.Get("X-GitHub-Delivery"), Tenant: policy.Tenant, Repo: repo, SourceRepo: repo, InstallationID: policy.InstallationID, Trigger: r.Header.Get("X-GitHub-Event")}
	if r.Header.Get("X-GitHub-Delivery") == "" || len(in.RequestID) > 200 {
		writeError(w, 400, "A bounded delivery ID is required")
		return
	}
	switch in.Trigger {
	case "push":
		if payload.Deleted {
			writeJSON(w, 202, map[string]string{"status": "ignored"})
			return
		}
		in.CommitSHA = payload.After
	case "pull_request":
		if payload.Action != "opened" && payload.Action != "reopened" && payload.Action != "synchronize" && payload.Action != "closed" {
			writeJSON(w, 202, map[string]string{"status": "ignored"})
			return
		}
		in.CommitSHA = payload.PullRequest.Head.SHA
		in.SourceRepo = payload.PullRequest.Head.Repository.FullName
		in.PRNumber = payload.PullRequest.Number
		in.EventTime = payload.PullRequest.UpdatedAt
		in.Closed = payload.Action == "closed"
		in.Fork = in.SourceRepo != repo
		if in.PRNumber < 1 || in.PRNumber > 99999999 || in.EventTime.IsZero() {
			writeError(w, 400, "PR number and updated_at are required")
			return
		}
	default:
		writeJSON(w, 202, map[string]string{"status": "ignored"})
		return
	}
	if !in.Closed {
		if !planner.RepositoryPattern.MatchString(in.SourceRepo) || !planner.CommitPattern.MatchString(in.CommitSHA) {
			writeError(w, 400, "Invalid immutable source identity")
			return
		}
		if in.Fork && !policy.AllowForks {
			writeJSON(w, 202, map[string]string{"status": "ignored", "reason": "fork builds are disabled"})
			return
		}
		config, e := s.GitHub.Config(r.Context(), policy.InstallationID, in.SourceRepo, in.CommitSHA)
		if e != nil {
			s.fail(w, e)
			return
		}
		in.Spec, e = planner.Parse(config)
		if e == nil {
			e = planner.Executable(&in.Spec, policy.Trusted && !in.Fork)
		}
		if e == nil {
			e = validateDestinations(in.Spec, policy)
		}
		if e != nil {
			writeError(w, 422, e.Error())
			return
		}
	}
	var v domain.PipelineView
	var duplicate bool
	e = s.Backend.Transact(r.Context(), true, "github delivery "+in.RequestID, func(st *control.Store, now time.Time) error {
		var err error
		v, duplicate, err = st.Submit(in, now)
		return err
	})
	if e != nil {
		s.fail(w, e)
		return
	}
	status := 202
	if duplicate {
		status = 200
	}
	writeJSON(w, status, map[string]any{"pipeline": v, "duplicate": duplicate})
}
