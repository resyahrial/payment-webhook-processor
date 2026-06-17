package http

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"

	"payment-webhook-processor/internal/service"
	"payment-webhook-processor/internal/webhook"
)

type webhookProcessor interface {
	Process(ctx context.Context, event webhook.PaymentEvent) (service.Result, error)
}

type WebhookHandler struct {
	signingSecret string
	processor     webhookProcessor
}

type webhookResponse struct {
	Status string `json:"status"`
}

func NewWebhookHandler(signingSecret string, processor webhookProcessor) stdhttp.Handler {
	return &WebhookHandler{signingSecret: signingSecret, processor: processor}
}

func (h *WebhookHandler) ServeHTTP(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadRequest, webhookResponse{Status: "invalid_payload"})
		return
	}

	if err := webhook.ValidateSignature(rawBody, r.Header.Get(webhook.SignatureHeader), h.signingSecret); err != nil {
		writeJSON(w, stdhttp.StatusUnauthorized, webhookResponse{Status: "unauthorized"})
		return
	}

	event, err := webhook.ParseEvent(rawBody)
	if err != nil {
		writeJSON(w, stdhttp.StatusBadRequest, webhookResponse{Status: "invalid_payload"})
		return
	}

	result, err := h.processor.Process(r.Context(), event)
	if err != nil {
		writeJSON(w, stdhttp.StatusInternalServerError, webhookResponse{Status: "internal_error"})
		return
	}

	status := string(result.Status)
	if status == "" {
		status = string(service.ResultStatusProcessed)
	}

	writeJSON(w, stdhttp.StatusOK, webhookResponse{Status: status})
}

func writeJSON(w stdhttp.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
