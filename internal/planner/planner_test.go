package planner

import (
	"reflect"
	"strings"
	"testing"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestValidateReturnsDeterministicTopologicalOrder(t *testing.T) {
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"preview": {Needs: []string{"image"}},
		"lint":    {},
		"image":   {Needs: []string{"test"}},
		"test":    {},
	}}
	got, err := Validate(spec)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	want := []string{"lint", "test", "image", "preview"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate() = %v, want %v", got, want)
	}
}

func TestValidateRejectsCycle(t *testing.T) {
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"a": {Needs: []string{"b"}},
		"b": {Needs: []string{"a"}},
	}}
	_, err := Validate(spec)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Validate() error = %v, want cycle error", err)
	}
}

func TestValidateRejectsUnknownDependency(t *testing.T) {
	spec := domain.PipelineSpec{Version: 1, Jobs: map[string]domain.JobSpec{
		"test": {Needs: []string{"missing"}},
	}}
	_, err := Validate(spec)
	if err == nil || !strings.Contains(err.Error(), "unknown dependency") {
		t.Fatalf("Validate() error = %v, want unknown dependency error", err)
	}
}
