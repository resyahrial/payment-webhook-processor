package service

import (
	"context"
	"errors"
	"fmt"

	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

type PaymentStateRepository interface {
	GetByPaymentID(ctx context.Context, paymentID string) (repository.Payment, error)
	Upsert(ctx context.Context, payment repository.Payment) error
}

type PaymentProcessingStatus string

const (
	PaymentProcessingStatusCreated PaymentProcessingStatus = "created"
	PaymentProcessingStatusUpdated PaymentProcessingStatus = "updated"
	PaymentProcessingStatusIgnored PaymentProcessingStatus = "ignored"
	PaymentProcessingStatusFailed  PaymentProcessingStatus = "failed"
)

type PaymentProcessingResult struct {
	Status PaymentProcessingStatus
}

type PaymentProcessor struct {
	repository PaymentStateRepository
}

func NewPaymentProcessor(repository PaymentStateRepository) *PaymentProcessor {
	return &PaymentProcessor{repository: repository}
}

func (p *PaymentProcessor) Process(ctx context.Context, event webhook.PaymentEvent) (PaymentProcessingResult, error) {
	current, err := p.repository.GetByPaymentID(ctx, event.PaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			if err := p.repository.Upsert(ctx, paymentFromEvent(event)); err != nil {
				return PaymentProcessingResult{Status: PaymentProcessingStatusFailed}, fmt.Errorf("create payment state: %w", err)
			}

			return PaymentProcessingResult{Status: PaymentProcessingStatusCreated}, nil
		}

		return PaymentProcessingResult{Status: PaymentProcessingStatusFailed}, fmt.Errorf("get current payment state: %w", err)
	}

	if !event.EventTimestamp.After(current.StatusTimestamp) {
		return PaymentProcessingResult{Status: PaymentProcessingStatusIgnored}, nil
	}

	if err := p.repository.Upsert(ctx, paymentFromEvent(event)); err != nil {
		return PaymentProcessingResult{Status: PaymentProcessingStatusFailed}, fmt.Errorf("update payment state: %w", err)
	}

	return PaymentProcessingResult{Status: PaymentProcessingStatusUpdated}, nil
}

func (p *PaymentProcessor) Update(ctx context.Context, event webhook.PaymentEvent) error {
	_, err := p.Process(ctx, event)
	return err
}

func paymentFromEvent(event webhook.PaymentEvent) repository.Payment {
	return repository.Payment{
		PaymentID:       event.PaymentID,
		Status:          event.PaymentStatus,
		StatusTimestamp: event.EventTimestamp,
	}
}
