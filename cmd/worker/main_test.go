package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/QihuiPan/ci-preview-platform/internal/domain"
)

func TestFailedInfrastructureDoesNotReclaimSameLease(t *testing.T) {
	w := &worker{identity: domain.Worker{Capacity: 1}, active: map[string]bool{}, seen: map[string]bool{}}
	first := domain.AttemptLease{Attempt: domain.Attempt{ID: "first"}}
	second := domain.AttemptLease{Attempt: domain.Attempt{ID: "second"}}
	if len(w.claim([]domain.AttemptLease{first, second})) != 1 {
		t.Fatal("capacity limit was not respected")
	}
	delete(w.active, "first")
	if len(w.claim([]domain.AttemptLease{first})) != 0 {
		t.Fatal("an abandoned lease was renewed instead of expiring")
	}
	if len(w.claim([]domain.AttemptLease{second})) != 1 || w.seen["first"] {
		t.Fatal("new attempts must remain eligible and expired leases must be forgotten")
	}
}

func TestArchiveBoundaries(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "result.txt"), []byte("result"), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := archive(root, &out); e != nil {
		t.Fatal(e)
	}
	entry, e := tar.NewReader(&out).Next()
	if e != nil || entry.Name != "result.txt" {
		t.Fatal(entry, e)
	}
	if runtime.GOOS != "windows" {
		if e = os.Symlink("/etc/passwd", filepath.Join(root, "escape")); e != nil {
			t.Fatal(e)
		}
		if e = archive(root, &bytes.Buffer{}); e == nil {
			t.Fatal("artifact symlink accepted")
		}
	}
}
