package objectstore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentAddressedAuthenticatedRoundTrip(t *testing.T) {
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=access/") {
			t.Error("unsigned storage request")
		}
		switch r.Method {
		case "PUT":
			b, _ := io.ReadAll(r.Body)
			if hash(b) != r.Header.Get("X-Amz-Content-Sha256") {
				t.Error("payload hash mismatch")
			}
			objects[r.URL.Path] = b
		case "GET":
			w.Write(objects[r.URL.Path])
		case "HEAD":
			w.WriteHeader(200)
		}
	}))
	defer server.Close()
	s := &S3{Endpoint: server.URL, Bucket: "artifacts", AccessKey: "access", SecretKey: "secret"}
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	key, e := s.Put(ctx, "attempts/a/logs", []byte("hello"))
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Put(ctx, "attempts/a/logs", []byte("hello"))
	if e != nil || again != key {
		t.Fatal("non-content-addressed write")
	}
	b, e := s.Get(ctx, key)
	if e != nil || string(b) != "hello" {
		t.Fatal(string(b), e)
	}
	if e = s.Ping(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, "../secret"); e == nil {
		t.Fatal("unsafe object key accepted")
	}
}
