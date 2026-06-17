package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"payment-webhook-processor/internal/service"
	"payment-webhook-processor/internal/webhook"
)

func TestWebhookHandlerServesValidSignedWebhook(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_901","payment_id":"pay_901","event_type":"payment.paid","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{result: service.Result{Status: service.ResultStatusProcessed}}
	handler := NewWebhookHandler(secret, processor)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if processor.calls != 1 {
		t.Fatalf("expected 1 processor call, got %d", processor.calls)
	}

	expectedTime := time.Date(2026, time.June, 18, 10, 30, 0, 0, time.UTC)
	if !processor.events[0].EventTimestamp.Equal(expectedTime) {
		t.Fatalf("expected parsed timestamp %s, got %s", expectedTime, processor.events[0].EventTimestamp)
	}

	if string(processor.events[0].RawPayload) != body {
		t.Fatalf("expected raw payload %q, got %q", body, string(processor.events[0].RawPayload))
	}

	assertWebhookResponseStatus(t, recorder.Body.Bytes(), string(service.ResultStatusProcessed))
}

func TestWebhookHandlerRejectsMissingSignature(t *testing.T) {
	t.Parallel()

	processor := &stubWebhookProcessor{}
	handler := NewWebhookHandler("top-secret", processor)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(`{"provider_event_id":"evt_902","payment_id":"pay_902","event_type":"payment.pending","event_timestamp":"2026-06-18T10:30:00Z"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", recorder.Code)
	}

	if processor.calls != 0 {
		t.Fatalf("expected processor not to be called, got %d calls", processor.calls)
	}
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "unauthorized")
}

func TestWebhookHandlerRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	processor := &stubWebhookProcessor{}
	handler := NewWebhookHandler("top-secret", processor)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(`{"provider_event_id":"evt_903","payment_id":"pay_903","event_type":"payment.pending","event_timestamp":"2026-06-18T10:30:00Z"}`))
	req.Header.Set(webhook.SignatureHeader, "not-a-valid-signature")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", recorder.Code)
	}

	if processor.calls != 0 {
		t.Fatalf("expected processor not to be called, got %d calls", processor.calls)
	}
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "unauthorized")
}

func TestWebhookHandlerRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	processor := &stubWebhookProcessor{}
	handler := NewWebhookHandler(secret, processor)
	body := `{"provider_event_id":"evt_904"`

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if processor.calls != 0 {
		t.Fatalf("expected processor not to be called, got %d calls", processor.calls)
	}
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "invalid_payload")
}

func TestWebhookHandlerRejectsUnknownEventType(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	processor := &stubWebhookProcessor{}
	handler := NewWebhookHandler(secret, processor)
	body := `{"provider_event_id":"evt_905","payment_id":"pay_905","event_type":"payment.refunded","event_timestamp":"2026-06-18T10:30:00Z"}`

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	if processor.calls != 0 {
		t.Fatalf("expected processor not to be called, got %d calls", processor.calls)
	}
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "invalid_payload")
}

func TestWebhookHandlerReturnsSuccessForDuplicateEvent(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_906","payment_id":"pay_906","event_type":"payment.failed","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{result: service.Result{Status: service.ResultStatusDuplicate}}
	handler := NewWebhookHandler(secret, processor)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if processor.calls != 1 {
		t.Fatalf("expected 1 processor call, got %d", processor.calls)
	}
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), string(service.ResultStatusDuplicate))
}

func TestWebhookHandlerReturnsInternalServerErrorOnProcessorFailure(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_907","payment_id":"pay_907","event_type":"payment.pending","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{err: errors.New("database unavailable")}
	handler := NewWebhookHandler(secret, processor)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "internal_error")
}

func TestWebhookHandlerRejectsNonPostRequests(t *testing.T) {
	t.Parallel()

	handler := NewWebhookHandler("top-secret", &stubWebhookProcessor{})
	req := httptest.NewRequest(stdhttp.MethodGet, "/webhooks/payment", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", recorder.Code)
	}
}

type stubWebhookProcessor struct {
	result service.Result
	err    error
	calls  int
	events []webhook.PaymentEvent
}

func (s *stubWebhookProcessor) Process(ctx context.Context, event webhook.PaymentEvent) (service.Result, error) {
	s.calls++
	s.events = append(s.events, event)
	return s.result, s.err
}

func signWebhookPayload(body []byte, secret string) string {
	hasher := hmac.New(sha256.New, []byte(secret))
	_, _ = hasher.Write(body)
	return hex.EncodeToString(hasher.Sum(nil))
}

func assertWebhookResponseStatus(t *testing.T, body []byte, expectedStatus string) {
	t.Helper()

	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("expected valid json response, got error %v", err)
	}

	if payload["status"] != expectedStatus {
		t.Fatalf("expected response status %q, got %q", expectedStatus, payload["status"])
	}
}
