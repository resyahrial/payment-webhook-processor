package service

import (
	"context"
	"errors"
	"fmt"

	"payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

type WebhookEventRepository interface {
	Insert(ctx context.Context, event webhook.PaymentEvent) (int64, error)
}

type PaymentUpdate struct {
	Event          webhook.PaymentEvent
	WebhookEventID int64
}

type PaymentUpdater func(ctx context.Context, update PaymentUpdate) error

type ResultStatus string

const (
	ResultStatusProcessed ResultStatus = "processed"
	ResultStatusDuplicate ResultStatus = "duplicate"
)

type Result struct {
	Status ResultStatus
}

type IdempotencyService struct {
	repository    WebhookEventRepository
	updatePayment PaymentUpdater
	metrics       *metrics.Metrics
}

func NewIdempotencyService(repository WebhookEventRepository, updatePayment PaymentUpdater, serviceMetrics *metrics.Metrics) *IdempotencyService {
	return &IdempotencyService{
		repository:    repository,
		updatePayment: updatePayment,
		metrics:       serviceMetrics,
	}
}

func (s *IdempotencyService) Process(ctx context.Context, event webhook.PaymentEvent) (Result, error) {
	webhookEventID, err := s.repository.Insert(ctx, event)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateProviderEventID) {
			s.metrics.IncWebhookDuplicate(string(event.EventType))
			return Result{Status: ResultStatusDuplicate}, nil
		}

		return Result{}, fmt.Errorf("store webhook event: %w", err)
	}

	if err := s.updatePayment(ctx, PaymentUpdate{Event: event, WebhookEventID: webhookEventID}); err != nil {
		return Result{}, fmt.Errorf("update payment state: %w", err)
	}

	return Result{Status: ResultStatusProcessed}, nil
}
