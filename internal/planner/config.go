package planner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"sigs.k8s.io/yaml"
)

var RepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var CommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var DigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Parse decodes repository YAML without silently accepting misspelled fields.
func Parse(data []byte) (domain.PipelineSpec, error) {
	var spec domain.PipelineSpec
	if len(data) > 1<<20 {
		return spec, errors.New("pipeline configuration exceeds 1 MiB")
	}
	raw, err := yaml.YAMLToJSONStrict(data)
	if err != nil {
		return spec, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&spec); err != nil {
		return spec, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return spec, errors.New("expected one configuration document")
	}
	return spec, nil
}

// Executable applies runtime bounds; trust is exclusively an operator policy.
func Executable(spec *domain.PipelineSpec, trusted bool) error {
	if len(spec.Jobs) > 32 {
		return errors.New("at most 32 jobs are allowed")
	}
	environments := 0
	builders := 0
	for name, j := range spec.Jobs {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("invalid job name %q", name)
		}
		j.Trusted = trusted
		if j.Type == "" {
			j.Type = "command"
		}
		if j.TimeoutSeconds == 0 {
			j.TimeoutSeconds = 900
		}
		if j.TimeoutSeconds < 10 || j.TimeoutSeconds > 3600 {
			return errors.New("timeout_seconds must be between 10 and 3600")
		}
		if j.MaxAttempts == 0 {
			j.MaxAttempts = 3
		}
		if j.MaxAttempts < 1 || j.MaxAttempts > 5 {
			return errors.New("max_attempts must be between 1 and 5")
		}
		if j.Resources.CPU < 1 || j.Resources.CPU > 8 || j.Resources.Memory < 64 || j.Resources.Memory > 16384 {
			return errors.New("each job requires 1-8 CPUs and 64-16384 MiB")
		}
		switch j.Type {
		case "command":
			if j.Image == "" || strings.HasPrefix(j.Image, "-") || strings.ContainsAny(j.Image, " \n\r\t") || len(j.Command) == 0 {
				return errors.New("command jobs require an image and an argv command")
			}
			if j.Buildkit != nil {
				return errors.New("buildkit settings require type buildkit")
			}
		case "buildkit":
			builders++
			if !trusted {
				return errors.New("image publishing is disabled for untrusted repositories and forks")
			}
			if j.Buildkit == nil {
				return errors.New("buildkit settings are required")
			}
			b := j.Buildkit
			if b.Context == "" {
				b.Context = "."
			}
			if b.Dockerfile == "" {
				b.Dockerfile = "Dockerfile"
			}
			for _, v := range []string{b.Context, b.Dockerfile} {
				if strings.HasPrefix(v, "/") || strings.Contains(v, "\\") || path.Clean(v) == ".." || strings.HasPrefix(path.Clean(v), "../") {
					return errors.New("build paths must stay inside the checkout")
				}
			}
			if b.Destination == "" || strings.ContainsAny(b.Destination, " ,\n\r\t@") || strings.HasPrefix(b.Destination, "-") {
				return errors.New("buildkit destination must be an OCI image name without a digest")
			}
			j.Capabilities = append(j.Capabilities, "buildkit-rootless")
		default:
			return errors.New("supported job types are command and buildkit")
		}
		if e := j.Environment; e != nil {
			environments++
			if e.TTLMinutes < 1 || e.TTLMinutes > 10080 {
				return errors.New("preview TTL must be 1-10080 minutes")
			}
			if e.Exposure != "internal" && e.Exposure != "public" {
				return errors.New("preview exposure must be internal or public")
			}
			if e.Port == 0 {
				e.Port = 8080
			}
			if e.Port < 1024 || e.Port > 65535 {
				return errors.New("preview port must be between 1024 and 65535")
			}
			if e.HealthPath == "" {
				e.HealthPath = "/"
			}
			if !strings.HasPrefix(e.HealthPath, "/") || strings.ContainsAny(e.HealthPath, "\r\n") {
				return errors.New("health_path must be an absolute HTTP path")
			}
			if e.Image != "" {
				pieces := strings.Split(e.Image, "@")
				if len(pieces) != 2 || !DigestPattern.MatchString(pieces[1]) {
					return errors.New("preview image must be pinned with @sha256:digest")
				}
			}
		}
		spec.Jobs[name] = j
	}
	if environments > 1 || builders > 1 {
		return errors.New("one image build and one preview per pipeline are supported")
	}
	for _, j := range spec.Jobs {
		if j.Environment != nil && j.Environment.Image == "" && builders != 1 {
			return errors.New("a preview requires a pinned image or an image build")
		}
	}
	_, err := Validate(*spec)
	return err
}
