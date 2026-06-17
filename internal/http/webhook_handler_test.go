package http

import (
	"bytes"
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

	dto "github.com/prometheus/client_model/go"

	"payment-webhook-processor/internal/logging"
	appmetrics "payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/service"
	"payment-webhook-processor/internal/webhook"
)

func TestWebhookHandlerServesValidSignedWebhook(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_901","payment_id":"pay_901","event_type":"payment.paid","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{result: service.Result{Status: service.ResultStatusProcessed}}
	handlerMetrics := appmetrics.New()
	handler := NewWebhookHandler(secret, processor, nil, handlerMetrics)

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

	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_requests_total", nil, 1)
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_success_total", map[string]string{"result": "processed"}, 1)
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_provider_events_total", map[string]string{"event_type": "payment.paid", "status": "paid"}, 1)
	assertMetricHistogramCount(t, handlerMetrics, "payment_webhook_response_duration_seconds", map[string]string{"result": "processed"}, 1)

	assertWebhookResponseStatus(t, recorder.Body.Bytes(), string(service.ResultStatusProcessed))
}

func TestWebhookHandlerRejectsMissingSignature(t *testing.T) {
	t.Parallel()

	processor := &stubWebhookProcessor{}
	handlerMetrics := appmetrics.New()
	handler := NewWebhookHandler("top-secret", processor, nil, handlerMetrics)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(`{"provider_event_id":"evt_902","payment_id":"pay_902","event_type":"payment.pending","event_timestamp":"2026-06-18T10:30:00Z"}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", recorder.Code)
	}

	if processor.calls != 0 {
		t.Fatalf("expected processor not to be called, got %d calls", processor.calls)
	}
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_requests_total", nil, 1)
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_errors_total", map[string]string{"result": "unauthorized"}, 1)
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_signature_failures_total", nil, 1)
	assertMetricHistogramCount(t, handlerMetrics, "payment_webhook_response_duration_seconds", map[string]string{"result": "unauthorized"}, 1)
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "unauthorized")
}

func TestWebhookHandlerRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	processor := &stubWebhookProcessor{}
	handler := NewWebhookHandler("top-secret", processor, nil, nil)

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
	handler := NewWebhookHandler(secret, processor, nil, nil)
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
	handler := NewWebhookHandler(secret, processor, nil, nil)
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
	handlerMetrics := appmetrics.New()
	handler := NewWebhookHandler(secret, processor, nil, handlerMetrics)

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
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_success_total", map[string]string{"result": "duplicate"}, 1)
	assertMetricHistogramCount(t, handlerMetrics, "payment_webhook_response_duration_seconds", map[string]string{"result": "duplicate"}, 1)
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), string(service.ResultStatusDuplicate))
}

func TestWebhookHandlerReturnsInternalServerErrorOnProcessorFailure(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_907","payment_id":"pay_907","event_type":"payment.pending","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{err: errors.New("database unavailable")}
	handlerMetrics := appmetrics.New()
	handler := NewWebhookHandler(secret, processor, nil, handlerMetrics)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	assertMetricCounterValue(t, handlerMetrics, "payment_webhook_errors_total", map[string]string{"result": "internal_error"}, 1)
	assertMetricHistogramCount(t, handlerMetrics, "payment_webhook_response_duration_seconds", map[string]string{"result": "internal_error"}, 1)
	assertWebhookResponseStatus(t, recorder.Body.Bytes(), "internal_error")
}

func TestWebhookHandlerRejectsNonPostRequests(t *testing.T) {
	t.Parallel()

	handler := NewWebhookHandler("top-secret", &stubWebhookProcessor{}, nil, nil)
	req := httptest.NewRequest(stdhttp.MethodGet, "/webhooks/payment", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", recorder.Code)
	}
}

func TestWebhookHandlerLogsCompletionForValidWebhook(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_log_901","payment_id":"pay_log_901","event_type":"payment.paid","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{result: service.Result{Status: service.ResultStatusProcessed}}
	logs := &bytes.Buffer{}
	handler := NewRouter(NewWebhookHandler(secret, processor, logging.NewJSONLogger(logs), nil), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	req.Header.Set(requestIDHeader, "req_valid_901")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	entry := decodeSingleLogEntry(t, logs)
	assertLogField(t, entry, "request_id", "req_valid_901")
	assertLogField(t, entry, "provider_event_id", "evt_log_901")
	assertLogField(t, entry, "payment_id", "pay_log_901")
	assertLogField(t, entry, "event_type", "payment.paid")
	assertLogField(t, entry, "payment_status", "paid")
	assertLogField(t, entry, "processing_result", "processed")
	assertNumericLogField(t, entry, "latency_ms")
}

func TestWebhookHandlerLogsCompletionForDuplicateWebhook(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_log_902","payment_id":"pay_log_902","event_type":"payment.failed","event_timestamp":"2026-06-18T10:30:00Z"}`
	processor := &stubWebhookProcessor{result: service.Result{Status: service.ResultStatusDuplicate}}
	logs := &bytes.Buffer{}
	handler := NewRouter(NewWebhookHandler(secret, processor, logging.NewJSONLogger(logs), nil), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	entry := decodeSingleLogEntry(t, logs)
	assertLogField(t, entry, "processing_result", "duplicate")
	assertStringLogFieldPresent(t, entry, "request_id")
}

func TestWebhookHandlerLogsInvalidSignatureSafely(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	signature := "not-a-valid-signature"
	body := `{"provider_event_id":"evt_log_903","payment_id":"pay_log_903","event_type":"payment.pending","event_timestamp":"2026-06-18T10:30:00Z"}`
	logs := &bytes.Buffer{}
	handler := NewRouter(NewWebhookHandler(secret, &stubWebhookProcessor{}, logging.NewJSONLogger(logs), nil), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signature)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	entry := decodeSingleLogEntry(t, logs)
	assertLogField(t, entry, "provider_event_id", "evt_log_903")
	assertLogField(t, entry, "payment_id", "pay_log_903")
	assertLogField(t, entry, "event_type", "payment.pending")
	assertLogField(t, entry, "error_type", "invalid_signature")
	assertLogDoesNotContain(t, logs.String(), secret)
	assertLogDoesNotContain(t, logs.String(), signature)
}

func TestWebhookHandlerLogsInvalidPayloadSafely(t *testing.T) {
	t.Parallel()

	secret := "top-secret"
	body := `{"provider_event_id":"evt_log_904"`
	logs := &bytes.Buffer{}
	handler := NewRouter(NewWebhookHandler(secret, &stubWebhookProcessor{}, logging.NewJSONLogger(logs), nil), nil)

	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", strings.NewReader(body))
	req.Header.Set(webhook.SignatureHeader, signWebhookPayload([]byte(body), secret))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	entry := decodeSingleLogEntry(t, logs)
	assertLogField(t, entry, "error_type", "invalid_payload")
	assertLogDoesNotContain(t, logs.String(), secret)
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

func decodeSingleLogEntry(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(logs)
	var entry map[string]any
	if err := decoder.Decode(&entry); err != nil {
		t.Fatalf("expected a single json log entry, got error %v with content %q", err, logs.String())
	}

	var extra map[string]any
	if err := decoder.Decode(&extra); err == nil {
		t.Fatalf("expected exactly one log entry, got additional entry %v", extra)
	}

	return entry
}

func assertLogField(t *testing.T, entry map[string]any, key, expected string) {
	t.Helper()

	actual, ok := entry[key].(string)
	if !ok {
		t.Fatalf("expected %s to be a string, got %T (%v)", key, entry[key], entry[key])
	}

	if actual != expected {
		t.Fatalf("expected %s %q, got %q", key, expected, actual)
	}
}

func assertStringLogFieldPresent(t *testing.T, entry map[string]any, key string) {
	t.Helper()

	actual, ok := entry[key].(string)
	if !ok || actual == "" {
		t.Fatalf("expected non-empty string field %s, got %T (%v)", key, entry[key], entry[key])
	}
}

func assertNumericLogField(t *testing.T, entry map[string]any, key string) {
	t.Helper()

	actual, ok := entry[key].(float64)
	if !ok {
		t.Fatalf("expected %s to be numeric, got %T (%v)", key, entry[key], entry[key])
	}

	if actual < 0 {
		t.Fatalf("expected %s to be non-negative, got %v", key, actual)
	}
}

func assertLogDoesNotContain(t *testing.T, content, unexpected string) {
	t.Helper()

	if strings.Contains(content, unexpected) {
		t.Fatalf("expected log output not to contain %q, got %q", unexpected, content)
	}
}

func assertMetricCounterValue(t *testing.T, handlerMetrics *appmetrics.Metrics, metricName string, expectedLabels map[string]string, expectedValue float64) {
	t.Helper()

	metric := findMetric(t, handlerMetrics, metricName, expectedLabels)
	if got := metric.GetCounter().GetValue(); got != expectedValue {
		t.Fatalf("expected counter %s with labels %v to be %v, got %v", metricName, expectedLabels, expectedValue, got)
	}
}

func assertMetricHistogramCount(t *testing.T, handlerMetrics *appmetrics.Metrics, metricName string, expectedLabels map[string]string, expectedCount uint64) {
	t.Helper()

	metric := findMetric(t, handlerMetrics, metricName, expectedLabels)
	if got := metric.GetHistogram().GetSampleCount(); got != expectedCount {
		t.Fatalf("expected histogram %s with labels %v count %d, got %d", metricName, expectedLabels, expectedCount, got)
	}
}

func findMetric(t *testing.T, handlerMetrics *appmetrics.Metrics, metricName string, expectedLabels map[string]string) *dto.Metric {
	t.Helper()

	metricFamilies, err := handlerMetrics.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != metricName {
			continue
		}

		for _, metric := range metricFamily.GetMetric() {
			if labelsMatch(metric.GetLabel(), expectedLabels) {
				return metric
			}
		}
	}

	t.Fatalf("metric %s with labels %v not found", metricName, expectedLabels)
	return nil
}

func labelsMatch(metricLabels []*dto.LabelPair, expectedLabels map[string]string) bool {
	if len(expectedLabels) == 0 {
		return len(metricLabels) == 0
	}

	if len(metricLabels) != len(expectedLabels) {
		return false
	}

	for _, label := range metricLabels {
		if expectedLabels[label.GetName()] != label.GetValue() {
			return false
		}
	}

	return true
}
