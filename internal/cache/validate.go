package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strings"
)

// Key creates a content-addressed cache key scoped to a repository and toolchain.
func Key(repository, toolchain, lockDigest, version string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{repository, toolchain, lockDigest, version}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// ValidateArchiveEntry rejects traversal, absolute paths, and oversized entries.
func ValidateArchiveEntry(name string, size, maxSize int64) error {
	if name == "" {
		return errors.New("archive entry name is empty")
	}
	normalized := strings.ReplaceAll(name, "\\", "/")
	cleaned := path.Clean(normalized)
	if strings.HasPrefix(normalized, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, ":") {
		return errors.New("archive entry escapes the extraction root")
	}
	if size < 0 || size > maxSize {
		return errors.New("archive entry exceeds the size limit")
	}
	return nil
}
