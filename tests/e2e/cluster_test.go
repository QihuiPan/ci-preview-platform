package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestRealClusterLifecycle(t *testing.T) {
	base := os.Getenv("E2E_API_URL")
	if base == "" {
		t.Skip("E2E_API_URL is required for real Kubernetes integration")
	}
	token := os.Getenv("E2E_API_TOKEN")
	previewImage := os.Getenv("E2E_PREVIEW_IMAGE")
	if token == "" || !strings.Contains(previewImage, "@sha256:") {
		t.Fatal("token and immutable preview image are required")
	}
	client := &http.Client{Timeout: 40 * time.Second}
	call := func(method, path, key string, payload any, status int) []byte {
		t.Helper()
		data, _ := json.Marshal(payload)
		r, e := http.NewRequest(method, base+path, bytes.NewReader(data))
		if e != nil {
			t.Fatal(e)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")
		res, e := client.Do(r)
		for retry := 0; e != nil && retry < 20; retry++ {
			time.Sleep(500 * time.Millisecond)
			r.Body, _ = r.GetBody()
			res, e = client.Do(r)
		}
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d: %s", method, path, res.StatusCode, b)
		}
		return b
	}
	waitPipeline := func(id string, wanted domain.State) domain.PipelineView {
		t.Helper()
		deadline := time.Now().Add(4 * time.Minute)
		for time.Now().Before(deadline) {
			var v domain.PipelineView
			json.Unmarshal(call("GET", "/v1/pipelines/"+id, "", nil, 200), &v)
			if v.Pipeline.Status == wanted {
				return v
			}
			if v.Pipeline.Status == domain.StateFailed && wanted != domain.StateFailed {
				for _, a := range v.Attempts {
					if a.LogKey != "" {
						t.Logf("Attempt %s logs: %s", a.ID, call("GET", "/v1/attempts/"+a.ID+"/objects/logs", "", nil, 200))
					}
				}
				t.Fatalf("pipeline failed: %+v", v)
			}
			time.Sleep(2 * time.Second)
		}
		t.Fatalf("pipeline did not reach %s; last state: %s", wanted, call("GET", "/v1/pipelines/"+id, "", nil, 200))
		return domain.PipelineView{}
	}
	in := control.Submission{Repo: "octocat/Hello-World", CommitSHA: "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d", PRNumber: 42, Spec: domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{"test": {Image: "alpine:3.21", Command: []string{"sh", "-ec", `test -f README; test ! -f /var/run/secrets/kubernetes.io/serviceaccount/token; mkdir -p artifacts; echo real-container-success | tee artifacts/result.txt`}, Resources: domain.Resources{CPU: 1, Memory: 128}, Environment: &domain.Environment{TTLMinutes: 2, Exposure: "internal", Image: previewImage, Port: 8080, HealthPath: "/"}}}}}
	var submitted struct {
		Pipeline domain.PipelineView `json:"pipeline"`
	}
	in.Spec.Jobs["network"] = domain.JobSpec{Image: "curlimages/curl:8.12.1", Needs: []string{"test"}, Command: []string{"sh", "-ec", `if curl -k --connect-timeout 2 --max-time 3 https://kubernetes.default.svc/version; then echo "Unexpected Kubernetes API access"; exit 1; fi; echo "Network isolation verified"`}, Resources: domain.Resources{CPU: 1, Memory: 128}}
	json.Unmarshal(call("POST", "/v1/pipelines", "cluster-success", in, 201), &submitted)
	call("POST", "/v1/pipelines", "cluster-success", in, 200)
	v := waitPipeline(submitted.Pipeline.Pipeline.ID, domain.StateSucceeded)
	if len(v.Attempts) != 2 {
		t.Fatal("expected two real attempts")
	}
	var testAttempt string
	for _, j := range v.Jobs {
		if j.Name == "test" {
			testAttempt = j.AttemptIDs[0]
		}
	}
	logs := call("GET", "/v1/attempts/"+testAttempt+"/objects/logs", "", nil, 200)
	if !bytes.Contains(logs, []byte("real-container-success")) {
		t.Fatal("execution logs were not stored", string(logs))
	}
	if len(call("GET", "/v1/attempts/"+testAttempt+"/objects/artifacts.tar", "", nil, 200)) < 512 {
		t.Fatal("artifact archive was not persisted")
	}
	var preview domain.Preview
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var out struct {
			Previews []domain.Preview `json:"previews"`
		}
		json.Unmarshal(call("GET", "/v1/previews", "", nil, 200), &out)
		for _, p := range out.Previews {
			if p.PRNumber == 42 && p.Actual == domain.StateActive {
				preview = p
			}
		}
		if preview.URL != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if preview.URL == "" {
		t.Fatal("preview never became ready")
	}
	kubectl(t, "run", "preview-probe", "-n", "ci-platform", "--image=curlimages/curl:8.12.1", "--restart=Never", "--rm", "-i", "--", "curl", "--fail", "--silent", "--max-time", "15", preview.URL)
	kubectl(t, "rollout", "restart", "deployment/ci-api", "-n", "ci-platform")
	kubectl(t, "rollout", "status", "deployment/ci-api", "-n", "ci-platform", "--timeout=180s")
	waitPipeline(v.Pipeline.ID, domain.StateSucceeded)
	// A genuine nonzero process exit must propagate to the API.
	in.PRNumber = 0
	j := in.Spec.Jobs["test"]
	j.Environment = nil
	j.Command = []string{"sh", "-c", "echo expected-failure; exit 7"}
	in.Spec.Jobs["test"] = j
	json.Unmarshal(call("POST", "/v1/pipelines", "cluster-failure", in, 201), &submitted)
	waitPipeline(submitted.Pipeline.Pipeline.ID, domain.StateFailed)
	j.Command = []string{"sh", "-c", "echo waiting; sleep 120"}
	in.Spec.Jobs["test"] = j
	json.Unmarshal(call("POST", "/v1/pipelines", "cluster-cancel", in, 201), &submitted)
	waitPipeline(submitted.Pipeline.Pipeline.ID, domain.StateRunning)
	call("POST", "/v1/pipelines/"+submitted.Pipeline.Pipeline.ID+"/cancel", "", map[string]any{}, 200)
	waitPipeline(submitted.Pipeline.Pipeline.ID, domain.StateCancelled)
	deadline = time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		var out struct {
			Previews []domain.Preview `json:"previews"`
		}
		json.Unmarshal(call("GET", "/v1/previews", "", nil, 200), &out)
		if len(out.Previews) == 0 {
			t.Log("Verified real execution, artifacts, preview HTTP, restart recovery, failure, cancellation, and TTL cleanup")
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatal("preview TTL cleanup did not converge")
}
func kubectl(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	b, e := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if e != nil {
		t.Fatal(fmt.Sprintf("kubectl %v: %v: %s", args, e, b))
	}
	t.Log(string(b))
}
