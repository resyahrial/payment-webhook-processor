package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	appdb "payment-webhook-processor/internal/db"
	"payment-webhook-processor/internal/webhook"
)

func TestWebhookEventRepositoryInsertStoresValidEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewWebhookEventRepository(testDB)
	event := webhook.PaymentEvent{
		ProviderEventID: "evt_201",
		PaymentID:       "pay_201",
		EventType:       webhook.EventTypePaymentPending,
		EventTimestamp:  time.Date(2026, time.June, 17, 10, 30, 0, 0, time.UTC),
		RawPayload:      []byte(`{"provider_event_id":"evt_201","payment_id":"pay_201","event_type":"payment.pending","event_timestamp":"2026-06-17T10:30:00Z"}`),
	}

	if err := repo.Insert(ctx, event); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	assertStoredEvent(t, ctx, testDB, event)
	assertStoredProcessingStatus(t, ctx, testDB, event.ProviderEventID, ProcessingStatusPending)
}

func TestWebhookEventRepositoryInsertRejectsDuplicateProviderEventID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewWebhookEventRepository(testDB)
	event := webhook.PaymentEvent{
		ProviderEventID: "evt_202",
		PaymentID:       "pay_202",
		EventType:       webhook.EventTypePaymentPaid,
		EventTimestamp:  time.Date(2026, time.June, 17, 11, 30, 0, 0, time.UTC),
		RawPayload:      []byte(`{"provider_event_id":"evt_202","payment_id":"pay_202","event_type":"payment.paid","event_timestamp":"2026-06-17T11:30:00Z"}`),
	}

	if err := repo.Insert(ctx, event); err != nil {
		t.Fatalf("insert first event: %v", err)
	}

	err := repo.Insert(ctx, event)
	if !errors.Is(err, ErrDuplicateProviderEventID) {
		t.Fatalf("expected ErrDuplicateProviderEventID, got %v", err)
	}
}

func TestWebhookEventRepositoryInsertAllowsSamePaymentIDWithDifferentProviderEventIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewWebhookEventRepository(testDB)
	firstTimestamp := time.Date(2026, time.June, 17, 11, 30, 0, 0, time.UTC)
	firstEvent := webhook.PaymentEvent{
		ProviderEventID: "evt_202_a",
		PaymentID:       "pay_202",
		EventType:       webhook.EventTypePaymentPending,
		EventTimestamp:  firstTimestamp,
		RawPayload:      []byte(`{"provider_event_id":"evt_202_a","payment_id":"pay_202","event_type":"payment.pending","event_timestamp":"2026-06-17T11:30:00Z"}`),
	}
	secondEvent := webhook.PaymentEvent{
		ProviderEventID: "evt_202_b",
		PaymentID:       "pay_202",
		EventType:       webhook.EventTypePaymentPaid,
		EventTimestamp:  firstTimestamp.Add(5 * time.Minute),
		RawPayload:      []byte(`{"provider_event_id":"evt_202_b","payment_id":"pay_202","event_type":"payment.paid","event_timestamp":"2026-06-17T11:35:00Z"}`),
	}

	if err := repo.Insert(ctx, firstEvent); err != nil {
		t.Fatalf("insert first event: %v", err)
	}

	if err := repo.Insert(ctx, secondEvent); err != nil {
		t.Fatalf("insert second event: %v", err)
	}

	assertStoredEvent(t, ctx, testDB, firstEvent)
	assertStoredEvent(t, ctx, testDB, secondEvent)
}

func TestWebhookEventRepositoryInsertSupportsAllEventTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		eventType webhook.EventType
	}{
		{name: "pending", eventType: webhook.EventTypePaymentPending},
		{name: "paid", eventType: webhook.EventTypePaymentPaid},
		{name: "failed", eventType: webhook.EventTypePaymentFailed},
		{name: "expired", eventType: webhook.EventTypePaymentExpired},
	}

	for index, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			testDB, cleanup := newRepositoryTestDatabase(t, ctx)
			defer cleanup()

			repo := NewWebhookEventRepository(testDB)
			eventTimestamp := time.Date(2026, time.June, 17, 12+index, 30, 0, 0, time.UTC)
			event := webhook.PaymentEvent{
				ProviderEventID: fmt.Sprintf("evt_30%d", index),
				PaymentID:       fmt.Sprintf("pay_30%d", index),
				EventType:       testCase.eventType,
				EventTimestamp:  eventTimestamp,
				RawPayload: []byte(fmt.Sprintf(`{"provider_event_id":"evt_30%d","payment_id":"pay_30%d","event_type":"%s","event_timestamp":"%s"}`,
					index,
					index,
					testCase.eventType,
					eventTimestamp.Format(time.RFC3339),
				)),
			}

			if err := repo.Insert(ctx, event); err != nil {
				t.Fatalf("insert event: %v", err)
			}

			assertStoredEvent(t, ctx, testDB, event)
		})
	}
}

func TestWebhookEventRepositoryInsertReturnsDatabaseErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newRepositoryTestDatabase(t, ctx)
	defer cleanup()

	repo := NewWebhookEventRepository(testDB)
	event := webhook.PaymentEvent{
		ProviderEventID: "evt_203",
		PaymentID:       "pay_203",
		EventType:       webhook.EventTypePaymentFailed,
		EventTimestamp:  time.Date(2026, time.June, 17, 12, 30, 0, 0, time.UTC),
		RawPayload:      []byte(`{"provider_event_id":"evt_203","payment_id":"pay_203","event_type":"payment.failed","event_timestamp":"2026-06-17T12:30:00Z"}`),
	}

	if err := testDB.Close(); err != nil {
		t.Fatalf("close test database: %v", err)
	}

	err := repo.Insert(ctx, event)
	if err == nil {
		t.Fatal("expected database error, got nil")
	}

	if errors.Is(err, ErrDuplicateProviderEventID) {
		t.Fatalf("expected non-duplicate database error, got %v", err)
	}

	cleanup = func() {}
}

func newRepositoryTestDatabase(t *testing.T, ctx context.Context) (*sql.DB, func()) {
	t.Helper()

	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	adminDB, err := appdb.Open(ctx, baseURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}

	schemaName := fmt.Sprintf("test_repository_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create schema: %v", err)
	}

	testDBURL, err := withRepositorySearchPath(baseURL, schemaName)
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName))
		_ = adminDB.Close()
		t.Fatalf("build schema-scoped database url: %v", err)
	}

	testDB, err := appdb.Open(ctx, testDBURL)
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName))
		_ = adminDB.Close()
		t.Fatalf("open test database: %v", err)
	}

	if err := appdb.RunMigrations(ctx, testDB); err != nil {
		_ = testDB.Close()
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName))
		_ = adminDB.Close()
		t.Fatalf("run migrations: %v", err)
	}

	cleanup := func() {
		if err := testDB.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			t.Fatalf("close test database: %v", err)
		}

		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)); err != nil {
			_ = adminDB.Close()
			t.Fatalf("drop schema: %v", err)
		}

		if err := adminDB.Close(); err != nil {
			t.Fatalf("close admin database: %v", err)
		}
	}

	return testDB, cleanup
}

func withRepositorySearchPath(databaseURL, schemaName string) (string, error) {
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		return "", err
	}

	query := parsedURL.Query()
	searchPath := strings.TrimSpace(query.Get("search_path"))
	if searchPath == "" {
		query.Set("search_path", schemaName)
	} else {
		query.Set("search_path", searchPath+","+schemaName)
	}
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

func assertStoredEvent(t *testing.T, ctx context.Context, db *sql.DB, event webhook.PaymentEvent) {
	t.Helper()

	var providerEventID string
	var paymentID string
	var eventType string
	var eventTimestamp time.Time
	var payloadMatches bool

	if err := db.QueryRowContext(ctx, `
		SELECT provider_event_id,
		       payment_id,
		       event_type,
		       event_timestamp,
		       raw_payload = $2::jsonb
		FROM webhook_events
		WHERE provider_event_id = $1
	`, event.ProviderEventID, string(event.RawPayload)).Scan(
		&providerEventID,
		&paymentID,
		&eventType,
		&eventTimestamp,
		&payloadMatches,
	); err != nil {
		t.Fatalf("query stored event: %v", err)
	}

	if providerEventID != event.ProviderEventID {
		t.Fatalf("expected provider event id %q, got %q", event.ProviderEventID, providerEventID)
	}

	if paymentID != event.PaymentID {
		t.Fatalf("expected payment id %q, got %q", event.PaymentID, paymentID)
	}

	if eventType != string(event.EventType) {
		t.Fatalf("expected event type %q, got %q", event.EventType, eventType)
	}

	if !eventTimestamp.Equal(event.EventTimestamp) {
		t.Fatalf("expected event timestamp %s, got %s", event.EventTimestamp, eventTimestamp)
	}

	if !payloadMatches {
		storedCanonical := canonicalJSON(t, readStoredPayload(t, ctx, db, event.ProviderEventID))
		originalCanonical := canonicalJSON(t, event.RawPayload)
		t.Fatalf("expected stored payload to match original JSONB content, got stored=%s original=%s", storedCanonical, originalCanonical)
	}
}

func assertStoredProcessingStatus(t *testing.T, ctx context.Context, db *sql.DB, providerEventID, expectedStatus string) {
	t.Helper()

	var status string
	if err := db.QueryRowContext(ctx, `
		SELECT processing_status
		FROM webhook_events
		WHERE provider_event_id = $1
	`, providerEventID).Scan(&status); err != nil {
		t.Fatalf("query processing status: %v", err)
	}

	if status != expectedStatus {
		t.Fatalf("expected processing status %q, got %q", expectedStatus, status)
	}
}

func readStoredPayload(t *testing.T, ctx context.Context, db *sql.DB, providerEventID string) []byte {
	t.Helper()

	var stored string
	if err := db.QueryRowContext(ctx, `
		SELECT raw_payload::text
		FROM webhook_events
		WHERE provider_event_id = $1
	`, providerEventID).Scan(&stored); err != nil {
		t.Fatalf("query stored payload: %v", err)
	}

	return []byte(stored)
}

func canonicalJSON(t *testing.T, raw []byte) string {
	t.Helper()

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("unmarshal json fixture: %v", err)
	}

	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal canonical json: %v", err)
	}

	return string(canonical)
}
