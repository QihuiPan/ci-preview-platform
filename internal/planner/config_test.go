package planner

import (
	"strings"
	"testing"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestStrictYAML(t *testing.T) {
	for _, raw := range []string{"version: 1\nunknown: true\njobs: {}", "version: 1\nversion: 2\njobs: {}"} {
		if _, e := Parse([]byte(raw)); e == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}
func TestRuntimePolicies(t *testing.T) {
	base := func() domain.PipelineSpec {
		return domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{"test": {Image: "alpine:3", Command: []string{"true"}, Resources: domain.Resources{CPU: 1, Memory: 128}, Trusted: true}}}
	}
	spec := base()
	if e := Executable(&spec, false); e != nil {
		t.Fatal(e)
	}
	if spec.Jobs["test"].Trusted {
		t.Fatal("trust escalation")
	}
	for _, j := range []domain.JobSpec{{Type: "buildkit", Buildkit: &domain.Buildkit{Destination: "r/r"}, Resources: domain.Resources{CPU: 1, Memory: 128}}, {Image: "alpine", Command: []string{"true"}, Resources: domain.Resources{CPU: 0, Memory: 128}}, {Image: "alpine", Command: []string{"true"}, Resources: domain.Resources{CPU: 1, Memory: 128}, Environment: &domain.Environment{TTLMinutes: 1, Exposure: "public", Image: "nginx:latest"}}} {
		spec = base()
		spec.Jobs["test"] = j
		if e := Executable(&spec, false); e == nil {
			t.Fatal("unsafe job accepted", j)
		}
	}
	spec = base()
	j := spec.Jobs["test"]
	j.Environment = &domain.Environment{TTLMinutes: 1, Exposure: "internal", Image: "nginx@sha256:" + strings.Repeat("a", 64)}
	spec.Jobs["test"] = j
	if e := Executable(&spec, false); e != nil {
		t.Fatal(e)
	}
}
