package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
	"github.com/QihuiPan/ci-preview-platform/internal/planner"
)

type Principal struct {
	Token  string         `json:"token"`
	Role   string         `json:"role"`
	Tenant string         `json:"tenant,omitempty"`
	Worker *domain.Worker `json:"worker,omitempty"`
}
type Repository struct {
	Tenant         string `json:"tenant"`
	Trusted        bool   `json:"trusted"`
	AllowForks     bool   `json:"allow_forks"`
	InstallationID int64  `json:"installation_id"`
	ImagePrefix    string `json:"image_prefix,omitempty"`
}
type Config struct {
	Principals   []Principal           `json:"principals"`
	Repositories map[string]Repository `json:"repositories"`
}

func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	seen := map[string]bool{}
	workers := map[string]bool{}
	admin := false
	for _, p := range c.Principals {
		if len(p.Token) < 32 || seen[p.Token] {
			return errors.New("tokens must be unique and at least 32 characters")
		}
		seen[p.Token] = true
		switch p.Role {
		case "admin":
			admin = true
		case "tenant":
			if p.Tenant == "" {
				return errors.New("tenant principal requires a tenant")
			}
		case "controller":
		case "worker":
			if p.Worker == nil || p.Worker.ID == "" || workers[p.Worker.ID] || p.Worker.Capacity < 1 || p.Worker.Capacity > 64 || p.Worker.Resources.CPU < 1 || p.Worker.Resources.Memory < 64 {
				return errors.New("worker principal requires unique identity, bounded capacity and resources")
			}
			workers[p.Worker.ID] = true
		default:
			return errors.New("invalid principal role")
		}
	}
	if !admin {
		return errors.New("at least one administrator is required")
	}
	for repo, policy := range c.Repositories {
		if !planner.RepositoryPattern.MatchString(repo) || policy.Tenant == "" {
			return errors.New("invalid repository policy")
		}
	}
	return nil
}
func (c Config) Authenticate(header string) (Principal, bool) {
	if !strings.HasPrefix(header, "Bearer ") {
		return Principal{}, false
	}
	hash := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
	var found Principal
	ok := false
	for _, p := range c.Principals {
		candidate := sha256.Sum256([]byte(p.Token))
		if subtle.ConstantTimeCompare(hash[:], candidate[:]) == 1 {
			found = p
			ok = true
		}
	}
	found.Token = ""
	return found, ok
}
