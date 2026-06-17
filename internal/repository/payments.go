package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/webhook"
)

var ErrPaymentNotFound = errors.New("payment not found")

type Payment struct {
	PaymentID       string
	Status          webhook.PaymentStatus
	StatusTimestamp time.Time
}

type PaymentRepository struct {
	db      *sql.DB
	metrics *metrics.Metrics
}

func NewPaymentRepository(db *sql.DB, repositoryMetrics *metrics.Metrics) *PaymentRepository {
	return &PaymentRepository{db: db, metrics: repositoryMetrics}
}

func (r *PaymentRepository) GetByPaymentID(ctx context.Context, paymentID string) (Payment, error) {
	var payment Payment

	err := r.db.QueryRowContext(ctx, `
		SELECT payment_id, status, status_timestamp
		FROM payments
		WHERE payment_id = $1
	`, paymentID).Scan(&payment.PaymentID, &payment.Status, &payment.StatusTimestamp)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Payment{}, ErrPaymentNotFound
		}

		return Payment{}, fmt.Errorf("get payment by id: %w", err)
	}

	return payment, nil
}

func (r *PaymentRepository) Upsert(ctx context.Context, payment Payment) error {
	startedAt := time.Now()
	defer func() {
		r.metrics.ObserveDatabaseWriteDuration("upsert_payment", time.Since(startedAt))
	}()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO payments (payment_id, status, status_timestamp)
		VALUES ($1, $2, $3)
		ON CONFLICT (payment_id) DO UPDATE
		SET status = EXCLUDED.status,
		    status_timestamp = EXCLUDED.status_timestamp,
		    updated_at = NOW()
	`, payment.PaymentID, payment.Status, payment.StatusTimestamp)
	if err != nil {
		return fmt.Errorf("upsert payment: %w", err)
	}

	return nil
}
