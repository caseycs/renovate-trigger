package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidateSignatureValid(t *testing.T) {
	body := []byte(`{"test": true}`)
	secret := "my-secret"
	sig := sign(body, secret)

	if !ValidateSignature(body, secret, sig) {
		t.Error("expected valid signature")
	}
}

func TestValidateSignatureInvalid(t *testing.T) {
	body := []byte(`{"test": true}`)
	if ValidateSignature(body, "secret", "sha256=invalid") {
		t.Error("expected invalid signature")
	}
}

func TestValidateSignatureEmptyHeader(t *testing.T) {
	if ValidateSignature([]byte("body"), "secret", "") {
		t.Error("expected false for empty header")
	}
}

func TestValidateSignatureEmptySecret(t *testing.T) {
	if ValidateSignature([]byte("body"), "", "sha256=abc") {
		t.Error("expected false for empty secret")
	}
}

func TestValidateSignatureWrongPrefix(t *testing.T) {
	if ValidateSignature([]byte("body"), "secret", "sha1=abc") {
		t.Error("expected false for wrong prefix")
	}
}

func TestValidateSignatureWrongBody(t *testing.T) {
	body := []byte(`original`)
	secret := "secret"
	sig := sign(body, secret)

	if ValidateSignature([]byte("tampered"), secret, sig) {
		t.Error("expected false for tampered body")
	}
}
