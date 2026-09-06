package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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
