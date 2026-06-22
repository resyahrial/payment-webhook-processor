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

	if current.Status == webhook.PaymentStatusFailed && event.PaymentStatus == webhook.PaymentStatusPaid && event.EventTimestamp.After(current.StatusTimestamp) {
		appendAnomaly(repository.AnomalyTypePaidAfterFailed)
	}

	if current.Status == webhook.PaymentStatusPaid && event.PaymentStatus == webhook.PaymentStatusFailed {
		appendAnomaly(repository.AnomalyTypeFailedAfterPaid)
	}

	if current.Status == webhook.PaymentStatusPaid && event.PaymentStatus == webhook.PaymentStatusPending && event.EventTimestamp.After(current.StatusTimestamp) {
		appendAnomaly(repository.AnomalyTypePendingAfterPaid)
	}

	if event.EventTimestamp.Before(current.StatusTimestamp) {
		appendAnomaly(repository.AnomalyTypeOlderEventTimestamp)
	}

	return anomalies
}
