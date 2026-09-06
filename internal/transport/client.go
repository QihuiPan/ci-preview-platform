package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	URL, Token string
	HTTP       *http.Client
}

func Env() (*Client, error) {
	token := os.Getenv("API_TOKEN")
	if file := os.Getenv("API_TOKEN_FILE"); file != "" {
		b, e := os.ReadFile(file)
		if e != nil {
			return nil, e
		}
		token = strings.TrimSpace(string(b))
	}
	base := strings.TrimRight(os.Getenv("API_URL"), "/")
	if base == "" || token == "" {
		return nil, fmt.Errorf("API_URL and API_TOKEN or API_TOKEN_FILE are required")
	}
	return &Client{URL: base, Token: token, HTTP: &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Do(ctx context.Context, method, path string, payload, target any) error {
	var data []byte
	if payload != nil {
		var e error
		data, e = json.Marshal(payload)
		if e != nil {
			return e
		}
	}
	return c.Raw(ctx, method, path, data, "", target)
}
func (c *Client) Raw(ctx context.Context, method, path string, data []byte, lease string, target any) error {
	r, e := http.NewRequestWithContext(ctx, method, c.URL+path, bytes.NewReader(data))
	if e != nil {
		return e
	}
	r.Header.Set("Authorization", "Bearer "+c.Token)
	r.Header.Set("Content-Type", "application/json")
	if lease != "" {
		r.Header.Set("X-Lease-Token", lease)
	}
	res, e := c.HTTP.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return &HTTPError{res.StatusCode}
	}
	if target != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(target)
	}
	return nil
}

type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return fmt.Sprintf("control plane returned HTTP %d", e.Status) }
