package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveArtifactPreservesBytesAndExistingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.tar")
	want := []byte{0, 255, 13, 10, 128, 0}
	if e := saveArtifact(path, bytes.NewReader(want)); e != nil {
		t.Fatal(e)
	}
	if e := saveArtifact(path, bytes.NewReader([]byte("replacement"))); e == nil {
		t.Fatal("existing download was overwritten")
	}
	got, e := os.ReadFile(path)
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("binary download changed", e, got)
	}
}
