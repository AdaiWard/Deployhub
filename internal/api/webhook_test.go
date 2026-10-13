package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidGitHubSignature(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	secret := "webhook-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !validGitHubSignature(body, signature, secret) {
		t.Fatal("expected signature to be valid")
	}
	if validGitHubSignature([]byte("tampered"), signature, secret) {
		t.Fatal("expected tampered body to be rejected")
	}
}
