package auth

import (
	"strings"
	"testing"
)

func TestConstantTimeTokenLookupAndConfigValidation(t *testing.T) {
	c := Config{Principals: []Principal{{Token: strings.Repeat("a", 32), Role: "admin"}}, Repositories: map[string]Repository{"owner/repo": {Tenant: "tenant"}}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, header := range []string{"", strings.Repeat("a", 32), "Bearer wrong", "Basic " + strings.Repeat("a", 32)} {
		if _, ok := c.Authenticate(header); ok {
			t.Fatal("accepted invalid authorization")
		}
	}
	p, ok := c.Authenticate("Bearer " + strings.Repeat("a", 32))
	if !ok || p.Token != "" {
		t.Fatal("invalid sanitized identity")
	}
	c.Principals = append(c.Principals, c.Principals[0])
	if e := c.Validate(); e == nil {
		t.Fatal("duplicate credential accepted")
	}
}
