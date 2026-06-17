package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"payment-webhook-processor/internal/webhook"
)

func TestPaymentRepositoryGetByPaymentIDReturnsStoredPayment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewPaymentRepository(testDB)
	expected := Payment{
		PaymentID:       "pay_401",
		Status:          webhook.PaymentStatusPending,
		StatusTimestamp: time.Date(2026, time.June, 17, 10, 30, 0, 0, time.UTC),
	}

	insertPaymentFixture(t, ctx, testDB, expected)

	payment, err := repo.GetByPaymentID(ctx, expected.PaymentID)
	if err != nil {
		t.Fatalf("get payment by id: %v", err)
	}

	assertPayment(t, payment, expected)
}

func TestPaymentRepositoryGetByPaymentIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewPaymentRepository(testDB)

	_, err := repo.GetByPaymentID(ctx, "pay_missing")
	if !errors.Is(err, ErrPaymentNotFound) {
		t.Fatalf("expected ErrPaymentNotFound, got %v", err)
	}
}

func TestPaymentRepositoryUpsertCreatesPaymentState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewPaymentRepository(testDB)
	expected := Payment{
		PaymentID:       "pay_402",
		Status:          webhook.PaymentStatusPending,
		StatusTimestamp: time.Date(2026, time.June, 17, 11, 30, 0, 0, time.UTC),
	}

	if err := repo.Upsert(ctx, expected); err != nil {
		t.Fatalf("upsert payment state: %v", err)
	}

	assertStoredPayment(t, ctx, testDB, expected)
}

func TestPaymentRepositoryUpsertUpdatesExistingPaymentState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewPaymentRepository(testDB)
	initial := Payment{
		PaymentID:       "pay_403",
		Status:          webhook.PaymentStatusPending,
		StatusTimestamp: time.Date(2026, time.June, 17, 12, 30, 0, 0, time.UTC),
	}
	updated := Payment{
		PaymentID:       initial.PaymentID,
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: initial.StatusTimestamp.Add(5 * time.Minute),
	}

	insertPaymentFixture(t, ctx, testDB, initial)

	if err := repo.Upsert(ctx, updated); err != nil {
		t.Fatalf("upsert payment state: %v", err)
	}

	assertStoredPayment(t, ctx, testDB, updated)
}

func insertPaymentFixture(t *testing.T, ctx context.Context, db *sql.DB, payment Payment) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO payments (payment_id, status, status_timestamp)
		VALUES ($1, $2, $3)
	`, payment.PaymentID, payment.Status, payment.StatusTimestamp); err != nil {
		t.Fatalf("insert payment fixture: %v", err)
	}
}

func assertStoredPayment(t *testing.T, ctx context.Context, db *sql.DB, expected Payment) {
	t.Helper()

	var paymentID string
	var status string
	var statusTimestamp time.Time

	if err := db.QueryRowContext(ctx, `
		SELECT payment_id, status, status_timestamp
		FROM payments
		WHERE payment_id = $1
	`, expected.PaymentID).Scan(&paymentID, &status, &statusTimestamp); err != nil {
		t.Fatalf("query stored payment: %v", err)
	}

	assertPayment(t, Payment{
		PaymentID:       paymentID,
		Status:          webhook.PaymentStatus(status),
		StatusTimestamp: statusTimestamp,
	}, expected)
}

func assertPayment(t *testing.T, actual, expected Payment) {
	t.Helper()

	if actual.PaymentID != expected.PaymentID {
		t.Fatalf("expected payment id %q, got %q", expected.PaymentID, actual.PaymentID)
	}

	if actual.Status != expected.Status {
		t.Fatalf("expected status %q, got %q", expected.Status, actual.Status)
	}

	if !actual.StatusTimestamp.Equal(expected.StatusTimestamp) {
		t.Fatalf("expected status timestamp %s, got %s", expected.StatusTimestamp, actual.StatusTimestamp)
	}
}
