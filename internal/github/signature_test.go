package github

import "testing"

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	const signature = "sha256=d42142b53efbc7cf5cd20b6e074eb33707e0de3b368f698e6d6f6c824ffb8d37"
	if err := VerifySignature("secret", body, signature); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	if err := VerifySignature("wrong", body, signature); err == nil {
		t.Fatal("VerifySignature() accepted a wrong secret")
	}
}
