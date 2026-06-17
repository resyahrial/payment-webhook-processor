package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/webhook"
)

const ProcessingStatusPending = "pending"

var ErrDuplicateProviderEventID = errors.New("duplicate provider event id")

type WebhookEventRepository struct {
	db      *sql.DB
	metrics *metrics.Metrics
}

func NewWebhookEventRepository(db *sql.DB, repositoryMetrics *metrics.Metrics) *WebhookEventRepository {
	return &WebhookEventRepository{db: db, metrics: repositoryMetrics}
}

func (r *WebhookEventRepository) Insert(ctx context.Context, event webhook.PaymentEvent) (int64, error) {
	startedAt := time.Now()
	defer func() {
		r.metrics.ObserveDatabaseWriteDuration("insert_webhook_event", time.Since(startedAt))
	}()

	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO webhook_events (
			provider_event_id,
			payment_id,
			event_type,
			event_timestamp,
			raw_payload,
			processing_status
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		RETURNING id
	`,
		event.ProviderEventID,
		event.PaymentID,
		string(event.EventType),
		event.EventTimestamp,
		string(event.RawPayload),
		ProcessingStatusPending,
	).Scan(&id)
	if err != nil {
		if isDuplicateProviderEventIDError(err) {
			return 0, ErrDuplicateProviderEventID
		}

		return 0, fmt.Errorf("insert webhook event: %w", err)
	}

	return id, nil
}

func isDuplicateProviderEventIDError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == "webhook_events_provider_event_id_key"
}
