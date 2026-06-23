package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

const SignatureHeader = "X-Webhook-Signature"

var (
	ErrMissingSignature = errors.New("missing webhook signature")
	ErrInvalidSignature = errors.New("invalid webhook signature")
)

func ValidateSignature(rawBody []byte, signature string, secret string) error {
	providedSignature := strings.TrimSpace(signature)
	if providedSignature == "" {
		return ErrMissingSignature
	}

	expectedSignature := computeSignature(rawBody, secret)
	providedSignatureBytes, err := hex.DecodeString(providedSignature)
	if err != nil {
		return ErrInvalidSignature
	}

	if subtle.ConstantTimeCompare(expectedSignature, providedSignatureBytes) != 1 {
		return ErrInvalidSignature
	}

	return nil
}

func computeSignature(rawBody []byte, secret string) []byte {
	hasher := hmac.New(sha256.New, []byte(secret))
	_, _ = hasher.Write(rawBody)
	return hasher.Sum(nil)
}
