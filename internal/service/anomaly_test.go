package service

import (
	"testing"
	"time"

	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

func TestDetectAnomaliesRecordsStaleEvent(t *testing.T) {
	t.Parallel()

	current := repository.Payment{
		PaymentID:       "pay_501",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 18, 10, 0, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_501", current.PaymentID, webhook.EventTypePaymentPending, current.StatusTimestamp.Add(-5*time.Minute))

	anomalies := DetectAnomalies(current, event, 41)
	if len(anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
	}

	assertAnomalyRecord(t, anomalies[0], repository.Anomaly{
		WebhookEventID: 41,
		PaymentID:      current.PaymentID,
		AnomalyType:    repository.AnomalyTypeStaleEvent,
		Details: repository.AnomalyDetails{
			ProviderEventID:   event.ProviderEventID,
			CurrentStatus:     current.Status,
			CurrentTimestamp:  current.StatusTimestamp,
			IncomingStatus:    event.PaymentStatus,
			IncomingTimestamp: event.EventTimestamp,
		},
	})
}

func TestDetectAnomaliesRecordsInvalidTransition(t *testing.T) {
	t.Parallel()

	current := repository.Payment{
		PaymentID:       "pay_502",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 18, 11, 0, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_502", current.PaymentID, webhook.EventTypePaymentFailed, current.StatusTimestamp.Add(5*time.Minute))

	anomalies := DetectAnomalies(current, event, 42)
	if len(anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
	}

	assertAnomalyRecord(t, anomalies[0], repository.Anomaly{
		WebhookEventID: 42,
		PaymentID:      current.PaymentID,
		AnomalyType:    repository.AnomalyTypeInvalidTerminalTransition,
		Details: repository.AnomalyDetails{
			ProviderEventID:   event.ProviderEventID,
			CurrentStatus:     current.Status,
			CurrentTimestamp:  current.StatusTimestamp,
			IncomingStatus:    event.PaymentStatus,
			IncomingTimestamp: event.EventTimestamp,
		},
	})
}

func TestDetectAnomaliesDoesNotStackTransitionAnomalyOnStaleEvent(t *testing.T) {
	t.Parallel()

	current := repository.Payment{
		PaymentID:       "pay_503",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 18, 12, 0, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_503", current.PaymentID, webhook.EventTypePaymentFailed, current.StatusTimestamp.Add(-5*time.Minute))

	anomalies := DetectAnomalies(current, event, 43)
	if len(anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
	}

	assertAnomalyRecord(t, anomalies[0], repository.Anomaly{
		WebhookEventID: 43,
		PaymentID:      current.PaymentID,
		AnomalyType:    repository.AnomalyTypeStaleEvent,
		Details: repository.AnomalyDetails{
			ProviderEventID:   event.ProviderEventID,
			CurrentStatus:     current.Status,
			CurrentTimestamp:  current.StatusTimestamp,
			IncomingStatus:    event.PaymentStatus,
			IncomingTimestamp: event.EventTimestamp,
		},
	})
}

func TestDetectAnomaliesRecordsInvalidTerminalTransition(t *testing.T) {
	t.Parallel()

	current := repository.Payment{
		PaymentID:       "pay_503_b",
		Status:          webhook.PaymentStatusChargeback,
		StatusTimestamp: time.Date(2026, time.June, 18, 12, 30, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_503_b", current.PaymentID, webhook.EventTypePaymentRefunded, current.StatusTimestamp.Add(5*time.Minute))

	anomalies := DetectAnomalies(current, event, 46)
	if len(anomalies) != 1 {
		t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
	}

	assertAnomalyRecord(t, anomalies[0], repository.Anomaly{
		WebhookEventID: 46,
		PaymentID:      current.PaymentID,
		AnomalyType:    repository.AnomalyTypeInvalidTerminalTransition,
		Details: repository.AnomalyDetails{
			ProviderEventID:   event.ProviderEventID,
			CurrentStatus:     current.Status,
			CurrentTimestamp:  current.StatusTimestamp,
			IncomingStatus:    event.PaymentStatus,
			IncomingTimestamp: event.EventTimestamp,
		},
	})
}

func TestDetectAnomaliesSkipsNormalTransitions(t *testing.T) {
	t.Parallel()

	current := repository.Payment{
		PaymentID:       "pay_504",
		Status:          webhook.PaymentStatusPending,
		StatusTimestamp: time.Date(2026, time.June, 18, 13, 0, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_504", current.PaymentID, webhook.EventTypePaymentPaid, current.StatusTimestamp.Add(5*time.Minute))

	anomalies := DetectAnomalies(current, event, 44)
	if len(anomalies) != 0 {
		t.Fatalf("expected no anomalies, got %d", len(anomalies))
	}
}

func TestDetectAnomaliesSkipsEqualTimestampEvents(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, time.June, 18, 14, 0, 0, 0, time.UTC)
	current := repository.Payment{
		PaymentID:       "pay_505",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: timestamp,
	}
	event := paymentProcessorEvent("evt_505", current.PaymentID, webhook.EventTypePaymentPaid, timestamp)

	anomalies := DetectAnomalies(current, event, 45)
	if len(anomalies) != 0 {
		t.Fatalf("expected no anomalies, got %d", len(anomalies))
	}
}

func assertAnomalyRecord(t *testing.T, actual, expected repository.Anomaly) {
	t.Helper()

	if actual.WebhookEventID != expected.WebhookEventID {
		t.Fatalf("expected webhook event id %d, got %d", expected.WebhookEventID, actual.WebhookEventID)
	}

	if actual.PaymentID != expected.PaymentID {
		t.Fatalf("expected payment id %q, got %q", expected.PaymentID, actual.PaymentID)
	}

	if actual.AnomalyType != expected.AnomalyType {
		t.Fatalf("expected anomaly type %q, got %q", expected.AnomalyType, actual.AnomalyType)
	}

	if actual.Details.ProviderEventID != expected.Details.ProviderEventID {
		t.Fatalf("expected provider event id %q, got %q", expected.Details.ProviderEventID, actual.Details.ProviderEventID)
	}

	if actual.Details.CurrentStatus != expected.Details.CurrentStatus {
		t.Fatalf("expected current status %q, got %q", expected.Details.CurrentStatus, actual.Details.CurrentStatus)
	}

	if !actual.Details.CurrentTimestamp.Equal(expected.Details.CurrentTimestamp) {
		t.Fatalf("expected current timestamp %s, got %s", expected.Details.CurrentTimestamp, actual.Details.CurrentTimestamp)
	}

	if actual.Details.IncomingStatus != expected.Details.IncomingStatus {
		t.Fatalf("expected incoming status %q, got %q", expected.Details.IncomingStatus, actual.Details.IncomingStatus)
	}

	if !actual.Details.IncomingTimestamp.Equal(expected.Details.IncomingTimestamp) {
		t.Fatalf("expected incoming timestamp %s, got %s", expected.Details.IncomingTimestamp, actual.Details.IncomingTimestamp)
	}
}
