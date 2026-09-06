package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

// App uses short-lived installation tokens. Private keys never reach job containers.
type App struct {
	ID     int64
	Key    *rsa.PrivateKey
	HTTP   *http.Client
	mu     sync.Mutex
	tokens map[int64]installationToken
}
type installationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewApp(id int64, keyPEM []byte) (*App, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("invalid GitHub App PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
		if e != nil {
			return nil, e
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("GitHub App requires an RSA private key")
		}
	}
	return &App{ID: id, Key: key, HTTP: &http.Client{Timeout: 20 * time.Second}, tokens: map[int64]installationToken{}}, nil
}
func (a *App) jwt() (string, error) {
	enc := base64.RawURLEncoding.EncodeToString
	claims, _ := json.Marshal(map[string]any{"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(8 * time.Minute).Unix(), "iss": strconv.FormatInt(a.ID, 10)})
	raw := enc([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc(claims)
	sum := sha256.Sum256([]byte(raw))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.Key, crypto.SHA256, sum[:])
	return raw + "." + enc(sig), err
}
func (a *App) request(ctx context.Context, method, path, token string, payload any, target any) error {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	r, e := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, bytes.NewReader(body))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/vnd.github+json")
	r.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	r.Header.Set("User-Agent", "ci-preview-platform")
	r.Header.Set("Content-Type", "application/json")
	res, e := a.HTTP.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("GitHub returned HTTP %d", res.StatusCode)
	}
	if target != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(target)
	}
	return nil
}
func (a *App) Token(ctx context.Context, id int64) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t, ok := a.tokens[id]; ok && time.Now().Add(time.Minute).Before(t.ExpiresAt) {
		return t.Token, nil
	}
	jwt, e := a.jwt()
	if e != nil {
		return "", e
	}
	var t installationToken
	e = a.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", id), jwt, nil, &t)
	if e != nil {
		return "", e
	}
	a.tokens[id] = t
	return t.Token, nil
}
func (a *App) Config(ctx context.Context, id int64, repo, sha string) ([]byte, error) {
	t, e := a.Token(ctx, id)
	if e != nil {
		return nil, e
	}
	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Size     int    `json:"size"`
	}
	e = a.request(ctx, "GET", "/repos/"+repo+"/contents/.ci-preview.yml?ref="+url.QueryEscape(sha), t, nil, &content)
	if e != nil {
		return nil, e
	}
	if content.Encoding != "base64" || content.Size > 1<<20 {
		return nil, errors.New("repository configuration is not a bounded text file")
	}
	return base64.StdEncoding.DecodeString(content.Content)
}
func (a *App) Check(ctx context.Context, p domain.Pipeline) (int64, error) {
	t, e := a.Token(ctx, p.InstallationID)
	if e != nil {
		return 0, e
	}
	payload := map[string]any{"name": "CI Preview Platform", "head_sha": p.CommitSHA, "external_id": p.ID, "status": "in_progress", "output": map[string]string{"title": string(p.Status), "summary": "Pipeline: " + p.ID}}
	switch p.Status {
	case domain.StateSucceeded, domain.StateFailed, domain.StateCancelled:
		payload["status"] = "completed"
		conclusion := "failure"
		if p.Status == domain.StateSucceeded {
			conclusion = "success"
		}
		if p.Status == domain.StateCancelled {
			conclusion = "cancelled"
		}
		payload["conclusion"] = conclusion
		payload["completed_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	method := "POST"
	path := "/repos/" + p.Repo + "/check-runs"
	if p.CheckRunID != 0 {
		method = "PATCH"
		path += "/" + strconv.FormatInt(p.CheckRunID, 10)
	}
	e = a.request(ctx, method, path, t, payload, &out)
	return out.ID, e
}
