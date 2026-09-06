// Package objectstore implements bounded, content-addressed S3 object operations.
package objectstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type S3 struct {
	Endpoint, Bucket, Region, AccessKey, SecretKey string
	HTTP                                           *http.Client
}

func (s *S3) Validate() error {
	u, e := url.Parse(s.Endpoint)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Path != "" {
		return errors.New("S3_ENDPOINT must be an HTTP origin")
	}
	if s.Bucket == "" || strings.Contains(s.Bucket, "/") || s.AccessKey == "" || s.SecretKey == "" {
		return errors.New("S3 bucket and credentials are required")
	}
	if s.Region == "" {
		s.Region = "us-east-1"
	}
	return nil
}
func hash(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func mac(key []byte, v string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(v))
	return h.Sum(nil)
}
func (s *S3) request(ctx context.Context, method, key string, body []byte) (*http.Response, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return nil, errors.New("invalid object key")
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return nil, err
	}
	u.Path = "/" + s.Bucket + "/" + key
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	stamp := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	digest := hash(body)
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", digest)
	signed := "host;x-amz-content-sha256;x-amz-date"
	canonical := method + "\n" + u.EscapedPath() + "\n\n" + "host:" + u.Host + "\n" + "x-amz-content-sha256:" + digest + "\n" + "x-amz-date:" + stamp + "\n\n" + signed + "\n" + digest
	scope := day + "/" + s.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hash([]byte(canonical))
	signingKey := mac(mac(mac(mac([]byte("AWS4"+s.SecretKey), day), s.Region), "s3"), "aws4_request")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+hex.EncodeToString(mac(signingKey, toSign)))
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("object storage returned HTTP %d", res.StatusCode)
	}
	return res, nil
}
func (s *S3) Put(ctx context.Context, prefix string, data []byte) (string, error) {
	key := prefix + "/" + hash(data)
	r, e := s.request(ctx, http.MethodPut, key, data)
	if e != nil {
		return "", e
	}
	r.Body.Close()
	return key, nil
}
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	r, e := s.request(ctx, http.MethodGet, key, nil)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
	if len(b) > 16<<20 {
		return nil, errors.New("object exceeds 16 MiB")
	}
	return b, e
}
func (s *S3) Ping(ctx context.Context) error {
	r, e := s.request(ctx, http.MethodHead, "", nil)
	if e == nil {
		r.Body.Close()
	}
	return e
}
