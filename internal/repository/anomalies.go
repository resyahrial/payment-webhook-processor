package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/webhook"
)

type AnomalyType string

const (
	AnomalyTypePaidAfterFailed     AnomalyType = "paid_after_failed"
	AnomalyTypeFailedAfterPaid     AnomalyType = "failed_after_paid"
	AnomalyTypeOlderEventTimestamp AnomalyType = "older_event_timestamp"
)

type AnomalyDetails struct {
	ProviderEventID   string                `json:"provider_event_id"`
	CurrentStatus     webhook.PaymentStatus `json:"current_status"`
	CurrentTimestamp  time.Time             `json:"current_timestamp"`
	IncomingStatus    webhook.PaymentStatus `json:"incoming_status"`
	IncomingTimestamp time.Time             `json:"incoming_timestamp"`
}

type Anomaly struct {
	WebhookEventID int64
	PaymentID      string
	AnomalyType    AnomalyType
	Details        AnomalyDetails
}

type AnomalyRepository struct {
	db      *sql.DB
	metrics *metrics.Metrics
}

func NewAnomalyRepository(db *sql.DB, repositoryMetrics *metrics.Metrics) *AnomalyRepository {
	return &AnomalyRepository{db: db, metrics: repositoryMetrics}
}

func (r *AnomalyRepository) Record(ctx context.Context, anomaly Anomaly) error {
	startedAt := time.Now()
	defer func() {
		r.metrics.ObserveDatabaseWriteDuration("insert_anomaly", time.Since(startedAt))
	}()

	detailsJSON, err := json.Marshal(anomaly.Details)
	if err != nil {
		return fmt.Errorf("marshal anomaly details: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO anomalies (webhook_event_id, payment_id, anomaly_type, details)
		VALUES ($1, $2, $3, $4::jsonb)
	`, nullableWebhookEventID(anomaly.WebhookEventID), anomaly.PaymentID, anomaly.AnomalyType, string(detailsJSON))
	if err != nil {
		return fmt.Errorf("insert anomaly: %w", err)
	}

	return nil
}

func nullableWebhookEventID(id int64) any {
	if id == 0 {
		return nil
	}

	return id
}
