package service

import (
	"context"
	"errors"
	"fmt"

	"payment-webhook-processor/internal/metrics"
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
	Reason string
}

const (
	IgnoreReasonOlderTimestamp    = "older_timestamp"
	IgnoreReasonEqualTimestamp    = "equal_timestamp"
	IgnoreReasonInvalidTransition = "invalid_terminal_transition"
)

type PaymentProcessor struct {
	repository      PaymentStateRepository
	anomalyRecorder AnomalyRecorder
	metrics         *metrics.Metrics
}

func NewPaymentProcessor(repository PaymentStateRepository, anomalyRecorder AnomalyRecorder, processorMetrics *metrics.Metrics) *PaymentProcessor {
	return &PaymentProcessor{repository: repository, anomalyRecorder: anomalyRecorder, metrics: processorMetrics}
}

func (p *PaymentProcessor) Process(ctx context.Context, update PaymentUpdate) (PaymentProcessingResult, error) {
	resultStatus := string(PaymentProcessingStatusFailed)
	timer := p.metrics.StartPaymentProcessingTimer(&resultStatus)
	defer func() {
		p.metrics.IncPaymentProcessing(resultStatus)
		timer.Observe()
	}()

	current, err := p.repository.GetByPaymentID(ctx, update.Event.PaymentID)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			if err := p.repository.Upsert(ctx, paymentFromEvent(update.Event)); err != nil {
				return PaymentProcessingResult{Status: PaymentProcessingStatusFailed}, fmt.Errorf("create payment state: %w", err)
			}

			resultStatus = string(PaymentProcessingStatusCreated)
			return PaymentProcessingResult{Status: PaymentProcessingStatusCreated}, nil
		}

		return PaymentProcessingResult{Status: PaymentProcessingStatusFailed}, fmt.Errorf("get current payment state: %w", err)
	}

	p.recordAnomalies(ctx, DetectAnomalies(current, update.Event, update.WebhookEventID))

	if !update.Event.EventTimestamp.After(current.StatusTimestamp) {
		ignoreReason := IgnoreReasonOlderTimestamp
		if update.Event.EventTimestamp.Equal(current.StatusTimestamp) {
			ignoreReason = IgnoreReasonEqualTimestamp
		}
		p.metrics.IncPaymentIgnored(ignoreReason)
		resultStatus = string(PaymentProcessingStatusIgnored)
		return PaymentProcessingResult{Status: PaymentProcessingStatusIgnored, Reason: ignoreReason}, nil
	}

	if !isTransitionAllowed(current.Status, update.Event.PaymentStatus) {
		p.metrics.IncPaymentIgnored(IgnoreReasonInvalidTransition)
		resultStatus = string(PaymentProcessingStatusIgnored)
		return PaymentProcessingResult{Status: PaymentProcessingStatusIgnored, Reason: IgnoreReasonInvalidTransition}, nil
	}

	if err := p.repository.Upsert(ctx, paymentFromEvent(update.Event)); err != nil {
		return PaymentProcessingResult{Status: PaymentProcessingStatusFailed}, fmt.Errorf("update payment state: %w", err)
	}

	resultStatus = string(PaymentProcessingStatusUpdated)
	return PaymentProcessingResult{Status: PaymentProcessingStatusUpdated}, nil
}

func (p *PaymentProcessor) Update(ctx context.Context, update PaymentUpdate) error {
	_, err := p.Process(ctx, update)
	return err
}

func (p *PaymentProcessor) recordAnomalies(ctx context.Context, anomalies []repository.Anomaly) {
	if p.anomalyRecorder == nil {
		return
	}

	for _, anomaly := range anomalies {
		p.metrics.IncAnomaly(string(anomaly.AnomalyType))
		_ = p.anomalyRecorder.Record(ctx, anomaly)
	}
}

func paymentFromEvent(event webhook.PaymentEvent) repository.Payment {
	return repository.Payment{
		PaymentID:       event.PaymentID,
		Status:          event.PaymentStatus,
		StatusTimestamp: event.EventTimestamp,
	}
}
