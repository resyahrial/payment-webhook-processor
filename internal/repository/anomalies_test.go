package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"payment-webhook-processor/internal/webhook"
)

func TestAnomalyRepositoryRecordStoresAnomaly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	insertPaymentFixture(t, ctx, testDB, Payment{
		PaymentID:       "pay_601",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 18, 10, 0, 0, 0, time.UTC),
	})
	webhookEventID := insertWebhookEventFixture(t, ctx, testDB, webhook.PaymentEvent{
		ProviderEventID: "evt_601",
		PaymentID:       "pay_601",
		EventType:       webhook.EventTypePaymentFailed,
		EventTimestamp:  time.Date(2026, time.June, 18, 9, 55, 0, 0, time.UTC),
		RawPayload:      []byte(`{"provider_event_id":"evt_601","payment_id":"pay_601","event_type":"payment.failed","event_timestamp":"2026-06-18T09:55:00Z"}`),
	})

	repo := NewAnomalyRepository(testDB)
	expected := Anomaly{
		WebhookEventID: webhookEventID,
		PaymentID:      "pay_601",
		AnomalyType:    AnomalyTypeOlderEventTimestamp,
		Details: AnomalyDetails{
			ProviderEventID:   "evt_601",
			CurrentStatus:     webhook.PaymentStatusPaid,
			CurrentTimestamp:  time.Date(2026, time.June, 18, 10, 0, 0, 0, time.UTC),
			IncomingStatus:    webhook.PaymentStatusFailed,
			IncomingTimestamp: time.Date(2026, time.June, 18, 9, 55, 0, 0, time.UTC),
		},
	}

	if err := repo.Record(ctx, expected); err != nil {
		t.Fatalf("record anomaly: %v", err)
	}

	assertStoredAnomaly(t, ctx, testDB, expected)
}

func TestAnomalyRepositoryRecordAllowsMultipleAnomaliesForOneEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	insertPaymentFixture(t, ctx, testDB, Payment{
		PaymentID:       "pay_602",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 18, 11, 0, 0, 0, time.UTC),
	})
	webhookEventID := insertWebhookEventFixture(t, ctx, testDB, webhook.PaymentEvent{
		ProviderEventID: "evt_602",
		PaymentID:       "pay_602",
		EventType:       webhook.EventTypePaymentFailed,
		EventTimestamp:  time.Date(2026, time.June, 18, 10, 55, 0, 0, time.UTC),
		RawPayload:      []byte(`{"provider_event_id":"evt_602","payment_id":"pay_602","event_type":"payment.failed","event_timestamp":"2026-06-18T10:55:00Z"}`),
	})

	repo := NewAnomalyRepository(testDB)
	first := Anomaly{
		WebhookEventID: webhookEventID,
		PaymentID:      "pay_602",
		AnomalyType:    AnomalyTypeFailedAfterPaid,
		Details: AnomalyDetails{
			ProviderEventID:   "evt_602",
			CurrentStatus:     webhook.PaymentStatusPaid,
			CurrentTimestamp:  time.Date(2026, time.June, 18, 11, 0, 0, 0, time.UTC),
			IncomingStatus:    webhook.PaymentStatusFailed,
			IncomingTimestamp: time.Date(2026, time.June, 18, 10, 55, 0, 0, time.UTC),
		},
	}
	second := first
	second.AnomalyType = AnomalyTypeOlderEventTimestamp

	if err := repo.Record(ctx, first); err != nil {
		t.Fatalf("record first anomaly: %v", err)
	}

	if err := repo.Record(ctx, second); err != nil {
		t.Fatalf("record second anomaly: %v", err)
	}

	var count int
	if err := testDB.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM anomalies
		WHERE webhook_event_id = $1
	`, webhookEventID).Scan(&count); err != nil {
		t.Fatalf("count anomalies: %v", err)
	}

	if count != 2 {
		t.Fatalf("expected 2 anomalies for one event, got %d", count)
	}
}

func insertWebhookEventFixture(t *testing.T, ctx context.Context, testDB *sql.DB, event webhook.PaymentEvent) int64 {
	t.Helper()

	var id int64
	if err := testDB.QueryRowContext(ctx, `
		INSERT INTO webhook_events (provider_event_id, payment_id, event_type, event_timestamp, raw_payload, processing_status)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		RETURNING id
	`, event.ProviderEventID, event.PaymentID, event.EventType, event.EventTimestamp, string(event.RawPayload), ProcessingStatusPending).Scan(&id); err != nil {
		t.Fatalf("insert webhook event fixture: %v", err)
	}

	return id
}

func assertStoredAnomaly(t *testing.T, ctx context.Context, db *sql.DB, expected Anomaly) {
	t.Helper()

	var webhookEventID int64
	var paymentID string
	var anomalyType string
	var rawDetails []byte

	if err := db.QueryRowContext(ctx, `
		SELECT webhook_event_id, payment_id, anomaly_type, details::text
		FROM anomalies
		WHERE webhook_event_id = $1 AND anomaly_type = $2
	`, expected.WebhookEventID, expected.AnomalyType).Scan(&webhookEventID, &paymentID, &anomalyType, &rawDetails); err != nil {
		t.Fatalf("query anomaly: %v", err)
	}

	if webhookEventID != expected.WebhookEventID {
		t.Fatalf("expected webhook event id %d, got %d", expected.WebhookEventID, webhookEventID)
	}

	if paymentID != expected.PaymentID {
		t.Fatalf("expected payment id %q, got %q", expected.PaymentID, paymentID)
	}

	if anomalyType != string(expected.AnomalyType) {
		t.Fatalf("expected anomaly type %q, got %q", expected.AnomalyType, anomalyType)
	}

	var actualDetails AnomalyDetails
	if err := json.Unmarshal(rawDetails, &actualDetails); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}

	if actualDetails.ProviderEventID != expected.Details.ProviderEventID {
		t.Fatalf("expected provider event id %q, got %q", expected.Details.ProviderEventID, actualDetails.ProviderEventID)
	}

	if actualDetails.CurrentStatus != expected.Details.CurrentStatus {
		t.Fatalf("expected current status %q, got %q", expected.Details.CurrentStatus, actualDetails.CurrentStatus)
	}

	if !actualDetails.CurrentTimestamp.Equal(expected.Details.CurrentTimestamp) {
		t.Fatalf("expected current timestamp %s, got %s", expected.Details.CurrentTimestamp, actualDetails.CurrentTimestamp)
	}

	if actualDetails.IncomingStatus != expected.Details.IncomingStatus {
		t.Fatalf("expected incoming status %q, got %q", expected.Details.IncomingStatus, actualDetails.IncomingStatus)
	}

	if !actualDetails.IncomingTimestamp.Equal(expected.Details.IncomingTimestamp) {
		t.Fatalf("expected incoming timestamp %s, got %s", expected.Details.IncomingTimestamp, actualDetails.IncomingTimestamp)
	}
}
