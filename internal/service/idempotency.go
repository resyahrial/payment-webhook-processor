package service

import (
	"context"
	"errors"
	"fmt"

	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

type WebhookEventRepository interface {
	Insert(ctx context.Context, event webhook.PaymentEvent) error
}

type PaymentUpdater func(ctx context.Context, event webhook.PaymentEvent) error

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
}

func NewIdempotencyService(repository WebhookEventRepository, updatePayment PaymentUpdater) *IdempotencyService {
	return &IdempotencyService{
		repository:    repository,
		updatePayment: updatePayment,
	}
}

func (s *IdempotencyService) Process(ctx context.Context, event webhook.PaymentEvent) (Result, error) {
	if err := s.repository.Insert(ctx, event); err != nil {
		if errors.Is(err, repository.ErrDuplicateProviderEventID) {
			return Result{Status: ResultStatusDuplicate}, nil
		}

		return Result{}, fmt.Errorf("store webhook event: %w", err)
	}

	if err := s.updatePayment(ctx, event); err != nil {
		return Result{}, fmt.Errorf("update payment state: %w", err)
	}

	return Result{Status: ResultStatusProcessed}, nil
}
