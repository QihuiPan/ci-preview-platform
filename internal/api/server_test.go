package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
)

func TestHealthAndReadiness(t *testing.T) {
	server, _ := newTestServer()
	for _, path := range []string{"/healthz", "/readyz"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, response.Code)
		}
	}
}

func TestGitHubWebhookVerifiesAndDeduplicates(t *testing.T) {
	server, _ := newTestServer()
	body := []byte(`{"action":"opened","repository":{"full_name":"acme/widget"},"pull_request":{"number":12,"head":{"sha":"abc123","repo":{"fork":false}}}}`)

	first := signedWebhookRequest(body, "delivery-1")
	firstResponse := httptest.NewRecorder()
	server.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first webhook status = %d, want 202; body = %s", firstResponse.Code, firstResponse.Body.String())
	}
	var firstPayload struct {
		Duplicate bool `json:"duplicate"`
		Pipeline  struct {
			Pipeline struct {
				ID string `json:"id"`
			} `json:"pipeline"`
		} `json:"pipeline"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}

	second := signedWebhookRequest(body, "delivery-1")
	secondResponse := httptest.NewRecorder()
	server.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("duplicate webhook status = %d, want 200", secondResponse.Code)
	}
	var secondPayload map[string]any
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if duplicate, _ := secondPayload["duplicate"].(bool); !duplicate {
		t.Fatalf("duplicate response = %v, want true", secondPayload["duplicate"])
	}
	if firstPayload.Pipeline.Pipeline.ID == "" {
		t.Fatal("first webhook did not create a pipeline ID")
	}
}

func TestGitHubWebhookRejectsBadSignature(t *testing.T) {
	server, _ := newTestServer()
	request := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github", bytes.NewReader([]byte(`{}`)))
	request.Header.Set("X-Hub-Signature-256", "sha256=00")
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-GitHub-Event", "push")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d, want 401", response.Code)
	}
}

func TestManualPipelineRejectsCycle(t *testing.T) {
	server, _ := newTestServer()
	body := []byte(`{
		"tenant":"acme","repo":"acme/widget","commit_sha":"abc","spec":{"version":1,"jobs":{
			"a":{"needs":["b"],"resources":{"cpu":1,"memory_mb":128}},
			"b":{"needs":["a"],"resources":{"cpu":1,"memory_mb":128}}
		}}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/pipelines", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("cyclic pipeline status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}

func newTestServer() (*Server, *control.Store) {
	store := control.New(control.Config{
		LeaseTTL: 10 * time.Second, WorkerTTL: time.Minute, DefaultTenantLimit: 2,
		PreviewBaseDomain: "preview.test", PreviewDeleteDelay: time.Second,
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(store, "secret", logger)
	server.now = func() time.Time { return time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC) }
	return server, store
}

func signedWebhookRequest(body []byte, deliveryID string) *http.Request {
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write(body)
	request := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github", bytes.NewReader(body))
	request.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	request.Header.Set("X-GitHub-Delivery", deliveryID)
	request.Header.Set("X-GitHub-Event", "pull_request")
	return request
}
