package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// VerifySignature verifies the GitHub sha256 HMAC header without timing leaks.
func VerifySignature(secret string, body []byte, header string) error {
	if secret == "" {
		return errors.New("webhook secret is not configured")
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return errors.New("signature must use sha256")
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return errors.New("signature is not valid hexadecimal")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), provided) {
		return errors.New("signature mismatch")
	}
	return nil
}
