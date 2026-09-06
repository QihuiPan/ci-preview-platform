package cache

import "testing"

func TestValidateArchiveEntry(t *testing.T) {
	for _, name := range []string{"../secret", "a/../../secret", `C:\\secret`, "/etc/passwd"} {
		if err := ValidateArchiveEntry(name, 1, 10); err == nil {
			t.Errorf("ValidateArchiveEntry(%q) accepted traversal", name)
		}
	}
	if err := ValidateArchiveEntry("go-build/cache.bin", 10, 10); err != nil {
		t.Fatalf("ValidateArchiveEntry() error = %v", err)
	}
	if err := ValidateArchiveEntry("large.bin", 11, 10); err == nil {
		t.Fatal("ValidateArchiveEntry() accepted oversized entry")
	}
}
