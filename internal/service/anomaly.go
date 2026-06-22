package service

import (
	"context"

	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

type AnomalyRecorder interface {
	Record(ctx context.Context, anomaly repository.Anomaly) error
}

func DetectAnomalies(current repository.Payment, event webhook.PaymentEvent, webhookEventID int64) []repository.Anomaly {
	details := repository.AnomalyDetails{
		ProviderEventID:   event.ProviderEventID,
		CurrentStatus:     current.Status,
		CurrentTimestamp:  current.StatusTimestamp,
		IncomingStatus:    event.PaymentStatus,
		IncomingTimestamp: event.EventTimestamp,
	}

	anomalies := make([]repository.Anomaly, 0, 2)
	appendAnomaly := func(anomalyType repository.AnomalyType) {
		anomalies = append(anomalies, repository.Anomaly{
			WebhookEventID: webhookEventID,
			PaymentID:      event.PaymentID,
			AnomalyType:    anomalyType,
			Details:        details,
		})
	}

	if event.EventTimestamp.Before(current.StatusTimestamp) {
		appendAnomaly(repository.AnomalyTypeStaleEvent)
		return anomalies
	}

	if !event.EventTimestamp.After(current.StatusTimestamp) {
		return anomalies
	}

	_, anomalyType, hasAnomaly := classifyTransition(current.Status, event.PaymentStatus)
	if hasAnomaly {
		appendAnomaly(anomalyType)
	}

	return anomalies
}

func classifyTransition(current, next webhook.PaymentStatus) (allowed bool, anomalyType repository.AnomalyType, hasAnomaly bool) {
	if isTransitionAllowed(current, next) {
		return true, "", false
	}

	if isTerminalStatus(current) {
		return false, repository.AnomalyTypeInvalidTerminalTransition, true
	}

	return true, repository.AnomalyTypeUnexpectedTransition, true
}

func isTransitionAllowed(current, next webhook.PaymentStatus) bool {
	switch current {
	case webhook.PaymentStatusPending:
		return next == webhook.PaymentStatusAuthorized || next == webhook.PaymentStatusPaid || next == webhook.PaymentStatusFailed || next == webhook.PaymentStatusExpired || next == webhook.PaymentStatusCancelled
	case webhook.PaymentStatusAuthorized:
		return next == webhook.PaymentStatusPaid || next == webhook.PaymentStatusFailed || next == webhook.PaymentStatusExpired || next == webhook.PaymentStatusCancelled
	case webhook.PaymentStatusPaid:
		return next == webhook.PaymentStatusPartiallyRefunded || next == webhook.PaymentStatusRefunded || next == webhook.PaymentStatusDisputed
	case webhook.PaymentStatusPartiallyRefunded:
		return next == webhook.PaymentStatusPartiallyRefunded || next == webhook.PaymentStatusRefunded || next == webhook.PaymentStatusDisputed
	case webhook.PaymentStatusDisputed:
		return next == webhook.PaymentStatusPaid || next == webhook.PaymentStatusChargeback
	default:
		return false
	}
}

func isTerminalStatus(status webhook.PaymentStatus) bool {
	switch status {
	case webhook.PaymentStatusFailed, webhook.PaymentStatusExpired, webhook.PaymentStatusCancelled, webhook.PaymentStatusRefunded, webhook.PaymentStatusChargeback:
		return true
	default:
		return false
	}
}
