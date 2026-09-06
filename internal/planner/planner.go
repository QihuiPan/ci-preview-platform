package planner

import (
	"errors"
	"fmt"
	"sort"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

const (
	minPriority = -10
	maxPriority = 10
)

// Validate checks schema constraints and returns a deterministic topological order.
func Validate(spec domain.PipelineSpec) ([]string, error) {
	if spec.Version != 1 {
		return nil, fmt.Errorf("unsupported pipeline version %d", spec.Version)
	}
	if len(spec.Jobs) == 0 {
		return nil, errors.New("pipeline must contain at least one job")
	}

	names := make([]string, 0, len(spec.Jobs))
	indegree := make(map[string]int, len(spec.Jobs))
	dependents := make(map[string][]string, len(spec.Jobs))
	for name, job := range spec.Jobs {
		if name == "" {
			return nil, errors.New("job name cannot be empty")
		}
		if job.Priority < minPriority || job.Priority > maxPriority {
			return nil, fmt.Errorf("job %q priority must be between %d and %d", name, minPriority, maxPriority)
		}
		if job.Resources.CPU < 0 || job.Resources.Memory < 0 {
			return nil, fmt.Errorf("job %q resources cannot be negative", name)
		}
		names = append(names, name)
		indegree[name] = 0
	}

	for name, job := range spec.Jobs {
		seen := make(map[string]bool, len(job.Needs))
		for _, dependency := range job.Needs {
			if dependency == name {
				return nil, fmt.Errorf("job %q cannot depend on itself", name)
			}
			if _, ok := spec.Jobs[dependency]; !ok {
				return nil, fmt.Errorf("job %q has unknown dependency %q", name, dependency)
			}
			if seen[dependency] {
				return nil, fmt.Errorf("job %q repeats dependency %q", name, dependency)
			}
			seen[dependency] = true
			indegree[name]++
			dependents[dependency] = append(dependents[dependency], name)
		}
	}

	sort.Strings(names)
	ready := make([]string, 0, len(names))
	for _, name := range names {
		if indegree[name] == 0 {
			ready = append(ready, name)
		}
	}

	order := make([]string, 0, len(names))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)
		sort.Strings(dependents[name])
		for _, dependent := range dependents[name] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Strings(ready)
			}
		}
	}

	if len(order) != len(names) {
		return nil, errors.New("pipeline dependency graph contains a cycle")
	}
	return order, nil
}
