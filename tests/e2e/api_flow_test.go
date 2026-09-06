package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/api"
	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestPipelineToPreviewFlow(t *testing.T) {
	now := time.Now()
	store := control.New(control.Config{
		LeaseTTL: time.Minute, WorkerTTL: time.Minute, DefaultTenantLimit: 2,
		PreviewBaseDomain: "preview.test", PreviewDeleteDelay: time.Second,
	})
	server := httptest.NewServer(api.NewServer(store, "secret", slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	postJSON(t, server.URL+"/v1/workers/register", map[string]any{
		"id": "worker-e2e", "pool": "trusted", "capabilities": []string{"linux-amd64"},
		"capacity": 1, "trusted": true,
	}, http.StatusOK, nil)

	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"test": {
			Image: "golang:1.26.5", Command: []string{"go", "test", "./..."},
			Capabilities: []string{"linux-amd64"}, Trusted: true,
			Resources: domain.Resources{CPU: 1, Memory: 128},
		},
		"preview": {
			Image: "alpine:3", Command: []string{"true"}, Needs: []string{"test"},
			Capabilities: []string{"linux-amd64"}, Trusted: true,
			Resources:   domain.Resources{CPU: 1, Memory: 128},
			Environment: &domain.Environment{TTLMinutes: 60, Exposure: "public"},
		},
	}}
	var view domain.PipelineView
	postJSON(t, server.URL+"/v1/pipelines", map[string]any{
		"tenant": "acme", "repo": "acme/widget", "commit_sha": "abc123",
		"trigger": "manual", "pr_number": 42, "spec": spec,
	}, http.StatusCreated, &view)

	for range 2 {
		lease, err := store.ScheduleOne(now)
		if err != nil {
			t.Fatalf("ScheduleOne() error = %v", err)
		}
		postJSON(t, server.URL+"/v1/attempts/"+lease.Attempt.ID+"/heartbeat", map[string]any{
			"lease_token": lease.LeaseToken,
		}, http.StatusOK, nil)
		postJSON(t, server.URL+"/v1/attempts/"+lease.Attempt.ID+"/complete", map[string]any{
			"lease_token": lease.LeaseToken, "success": true, "message": "e2e success",
		}, http.StatusOK, nil)
	}

	response, err := http.Get(server.URL + "/v1/pipelines/" + view.Pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pipeline status request = %s", response.Status)
	}
	var final domain.PipelineView
	if err := json.NewDecoder(response.Body).Decode(&final); err != nil {
		t.Fatal(err)
	}
	if final.Pipeline.Status != domain.StateSucceeded {
		t.Fatalf("pipeline status = %s, want SUCCEEDED", final.Pipeline.Status)
	}
	preview, err := store.GetPreview("acme/widget", 42)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Desired != domain.StateActive || preview.URL == "" {
		t.Fatalf("preview = %+v, want active preview URL", preview)
	}

	encodedRepo := url.PathEscape("acme/widget")
	previewResponse, err := http.Get(server.URL + "/v1/previews/" + encodedRepo + "/42")
	if err != nil {
		t.Fatal(err)
	}
	defer previewResponse.Body.Close()
	if previewResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(previewResponse.Body)
		t.Fatalf("preview status = %s; body = %s", previewResponse.Status, body)
	}
}

func postJSON(t *testing.T, endpoint string, payload any, wantStatus int, target any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s status = %d, want %d; body = %s", endpoint, response.StatusCode, wantStatus, responseBody)
	}
	if target != nil {
		if err := json.NewDecoder(response.Body).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
}
