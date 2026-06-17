package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestValidateSignatureAcceptsCorrectHMAC(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_123"}`)
	secret := "top-secret"
	signature := signPayload(body, secret)

	err := ValidateSignature(body, signature, secret)
	if err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestValidateSignatureRejectsMissingSignature(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_123"}`)

	err := ValidateSignature(body, "", "top-secret")
	if !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("expected ErrMissingSignature, got %v", err)
	}
}

func TestValidateSignatureRejectsIncorrectHMAC(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_123"}`)

	err := ValidateSignature(body, "not-a-valid-signature", "top-secret")
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestValidateSignatureRejectsSignatureGeneratedWithDifferentSecret(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_123"}`)
	signature := signPayload(body, "different-secret")

	err := ValidateSignature(body, signature, "top-secret")
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func TestValidateSignatureRejectsBodyChangedAfterSigning(t *testing.T) {
	t.Parallel()

	originalBody := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_123"}`)
	modifiedBody := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_456"}`)
	secret := "top-secret"
	signature := signPayload(originalBody, secret)

	err := ValidateSignature(modifiedBody, signature, secret)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
}

func signPayload(body []byte, secret string) string {
	hasher := hmac.New(sha256.New, []byte(secret))
	_, _ = hasher.Write(body)
	return hex.EncodeToString(hasher.Sum(nil))
}
