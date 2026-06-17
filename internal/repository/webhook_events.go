package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"payment-webhook-processor/internal/webhook"
)

const ProcessingStatusPending = "pending"

var ErrDuplicateProviderEventID = errors.New("duplicate provider event id")

type WebhookEventRepository struct {
	db *sql.DB
}

func NewWebhookEventRepository(db *sql.DB) *WebhookEventRepository {
	return &WebhookEventRepository{db: db}
}

func (r *WebhookEventRepository) Insert(ctx context.Context, event webhook.PaymentEvent) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO webhook_events (
			provider_event_id,
			payment_id,
			event_type,
			event_timestamp,
			raw_payload,
			processing_status
		) VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`,
		event.ProviderEventID,
		event.PaymentID,
		string(event.EventType),
		event.EventTimestamp,
		string(event.RawPayload),
		ProcessingStatusPending,
	)
	if err != nil {
		if isDuplicateProviderEventIDError(err) {
			return ErrDuplicateProviderEventID
		}

		return fmt.Errorf("insert webhook event: %w", err)
	}

	return nil
}

func isDuplicateProviderEventIDError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == "webhook_events_provider_event_id_key"
}
