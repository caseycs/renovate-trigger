package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func ValidateSignature(body []byte, secret string, signatureHeader string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}

	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}

	signature := strings.TrimPrefix(signatureHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}
