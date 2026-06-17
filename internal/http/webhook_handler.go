package http

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"

	"github.com/rs/zerolog"

	"payment-webhook-processor/internal/logging"
	"payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/service"
	"payment-webhook-processor/internal/webhook"
)

type webhookProcessor interface {
	Process(ctx context.Context, event webhook.PaymentEvent) (service.Result, error)
}

type WebhookHandler struct {
	signingSecret string
	processor     webhookProcessor
	logger        *zerolog.Logger
	metrics       *metrics.Metrics
}

type webhookResponse struct {
	Status string `json:"status"`
}

func NewWebhookHandler(signingSecret string, processor webhookProcessor, logger *zerolog.Logger, handlerMetrics *metrics.Metrics) stdhttp.Handler {
	if logger == nil {
		logger = logging.NewJSONLogger(io.Discard)
	}

	return &WebhookHandler{signingSecret: signingSecret, processor: processor, logger: logger, metrics: handlerMetrics}
}

func (h *WebhookHandler) ServeHTTP(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	logFields := webhookLogFields{RequestID: RequestIDFromContext(r.Context())}
	resultLabel := "method_not_allowed"
	timer := h.metrics.StartWebhookResponseTimer(&resultLabel)
	h.metrics.IncWebhookRequest()
	defer func() {
		duration := timer.Observe()
		event := logging.WithTraceContext(r.Context(), h.logger.Info()).Int64("latency_ms", duration.Milliseconds())
		logFields.apply(event)
		event.Msg("webhook request completed")
	}()

	if r.Method != stdhttp.MethodPost {
		logFields.ErrorType = "method_not_allowed"
		h.metrics.IncWebhookError(resultLabel)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		logFields.ErrorType = "body_read_error"
		resultLabel = "invalid_payload"
		h.metrics.IncWebhookError(resultLabel)
		writeJSON(w, stdhttp.StatusBadRequest, webhookResponse{Status: "invalid_payload"})
		return
	}

	populateLogFieldsFromPayload(&logFields, rawBody)

	if err := webhook.ValidateSignature(rawBody, r.Header.Get(webhook.SignatureHeader), h.signingSecret); err != nil {
		logFields.ErrorType = "invalid_signature"
		resultLabel = "unauthorized"
		h.metrics.IncSignatureFailure()
		h.metrics.IncWebhookError(resultLabel)
		writeJSON(w, stdhttp.StatusUnauthorized, webhookResponse{Status: "unauthorized"})
		return
	}

	event, err := webhook.ParseEvent(rawBody)
	if err != nil {
		logFields.ErrorType = "invalid_payload"
		resultLabel = "invalid_payload"
		h.metrics.IncWebhookError(resultLabel)
		writeJSON(w, stdhttp.StatusBadRequest, webhookResponse{Status: "invalid_payload"})
		return
	}
	h.metrics.IncProviderEvent(string(event.EventType), string(event.PaymentStatus))

	logFields.ProviderEventID = event.ProviderEventID
	logFields.PaymentID = event.PaymentID
	logFields.EventType = string(event.EventType)
	logFields.PaymentStatus = string(event.PaymentStatus)

	result, err := h.processor.Process(r.Context(), event)
	if err != nil {
		logFields.ErrorType = "processing_error"
		resultLabel = "internal_error"
		h.metrics.IncWebhookError(resultLabel)
		writeJSON(w, stdhttp.StatusInternalServerError, webhookResponse{Status: "internal_error"})
		return
	}

	status := string(result.Status)
	if status == "" {
		status = string(service.ResultStatusProcessed)
	}
	logFields.ProcessingResult = status
	resultLabel = status
	h.metrics.IncWebhookSuccess(resultLabel)

	writeJSON(w, stdhttp.StatusOK, webhookResponse{Status: status})
}

type webhookLogFields struct {
	RequestID        string
	ProviderEventID  string
	PaymentID        string
	EventType        string
	PaymentStatus    string
	ProcessingResult string
	ErrorType        string
}

func (f webhookLogFields) apply(event *zerolog.Event) {
	if f.RequestID != "" {
		event.Str("request_id", f.RequestID)
	}
	if f.ProviderEventID != "" {
		event.Str("provider_event_id", f.ProviderEventID)
	}
	if f.PaymentID != "" {
		event.Str("payment_id", f.PaymentID)
	}
	if f.EventType != "" {
		event.Str("event_type", f.EventType)
	}
	if f.PaymentStatus != "" {
		event.Str("payment_status", f.PaymentStatus)
	}
	if f.ProcessingResult != "" {
		event.Str("processing_result", f.ProcessingResult)
	}
	if f.ErrorType != "" {
		event.Str("error_type", f.ErrorType)
	}
}

type safePayloadFields struct {
	ProviderEventID string `json:"provider_event_id"`
	PaymentID       string `json:"payment_id"`
	EventType       string `json:"event_type"`
}

func populateLogFieldsFromPayload(logFields *webhookLogFields, rawBody []byte) {
	var fields safePayloadFields
	if err := json.Unmarshal(rawBody, &fields); err != nil {
		return
	}

	if logFields.ProviderEventID == "" {
		logFields.ProviderEventID = fields.ProviderEventID
	}
	if logFields.PaymentID == "" {
		logFields.PaymentID = fields.PaymentID
	}
	if logFields.EventType == "" {
		logFields.EventType = fields.EventType
	}
	if logFields.PaymentStatus == "" {
		if status, ok := paymentStatusFromPayload(fields.EventType); ok {
			logFields.PaymentStatus = status
		}
	}
}

func paymentStatusFromPayload(eventType string) (string, bool) {
	switch eventType {
	case string(webhook.EventTypePaymentPending):
		return string(webhook.PaymentStatusPending), true
	case string(webhook.EventTypePaymentPaid):
		return string(webhook.PaymentStatusPaid), true
	case string(webhook.EventTypePaymentFailed):
		return string(webhook.PaymentStatusFailed), true
	case string(webhook.EventTypePaymentExpired):
		return string(webhook.PaymentStatusExpired), true
	default:
		return "", false
	}
}

func writeJSON(w stdhttp.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
