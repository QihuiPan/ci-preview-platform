package kube

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestUntrustedPodHasNoManagementCredentials(t *testing.T) {
	r := Runner{Image: "platform:test", GitImage: "alpine/git:2.49.1"}
	l := domain.AttemptLease{Attempt: domain.Attempt{ID: "attempt_123"}, Job: domain.Job{Spec: domain.JobSpec{Image: "alpine:3", Command: []string{"true"}, Resources: domain.Resources{CPU: 1, Memory: 128}}}, Pipeline: domain.Pipeline{Repo: "a/b", CommitSHA: strings.Repeat("a", 40)}}
	p := r.Pod(l, false)
	raw, _ := json.Marshal(p)
	for _, forbidden := range []string{"hostPath", "privileged", "hostNetwork", "registry", "API_TOKEN", "secretKeyRef", "Unconfined"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("unexpected privileged configuration", forbidden)
		}
	}
	if !strings.Contains(string(raw), `"automountServiceAccountToken":false`) {
		t.Fatal("workload token was not disabled")
	}
	if !strings.Contains(string(raw), `"readOnlyRootFilesystem":true`) {
		t.Fatal("root filesystem is writable")
	}
	if !strings.Contains(string(raw), "safe.directory /workspace") || strings.Contains(string(raw), "safe.directory *") {
		t.Fatal("checkout must trust only its group-writable workspace mount")
	}
}
func TestPreviewUsesReadinessAndPinnedImage(t *testing.T) {
	c := PreviewController{Domain: "preview.example", IngressClass: "nginx"}
	p := domain.Preview{Namespace: "preview-a", PipelineID: "p", Image: "app@sha256:" + strings.Repeat("a", 64), Port: 8080, HealthPath: "/healthz", Exposure: "public"}
	objects := c.Manifests(p)
	raw, _ := json.Marshal(objects)
	for _, required := range []string{"readinessProbe", "/healthz", "app@sha256:", "preview-a.preview.example", "automountServiceAccountToken"} {
		if !strings.Contains(string(raw), required) {
			t.Fatal("missing preview guarantee", required)
		}
	}
}
func TestNetworkPolicyRejectsPrivateNetworks(t *testing.T) {
	raw, _ := json.Marshal(Boundaries("job", 0))
	for _, cidr := range []string{"10.0.0.0/8", "169.254.0.0/16", "192.168.0.0/16"} {
		if !strings.Contains(string(raw), cidr) {
			t.Fatal("missing network exclusion", cidr)
		}
	}
}

func TestPreviewReadinessRejectsOldAvailableReplica(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		observed, replicas, updated, available int
		want                                   bool
	}{
		{"ready", 2, 1, 1, 1, true},
		{"unobserved", 1, 1, 1, 1, false},
		{"old replica still available", 2, 2, 1, 1, false},
		{"new image not ready", 2, 1, 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Object{"metadata": Object{"generation": 2}, "status": Object{"observedGeneration": tc.observed, "replicas": tc.replicas, "updatedReplicas": tc.updated, "availableReplicas": tc.available}}
			got, e := deploymentReady(o)
			if e != nil || got != tc.want {
				t.Fatal("unexpected readiness", got, e)
			}
		})
	}
}
