package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunMigrationsAppliesSchemaAndIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newTestDatabase(t, ctx)
	defer cleanup()

	if err := RunMigrations(ctx, testDB); err != nil {
		t.Fatalf("first migration run failed: %v", err)
	}

	if err := RunMigrations(ctx, testDB); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	assertTableExists(t, ctx, testDB, "payments")
	assertTableExists(t, ctx, testDB, "webhook_events")
	assertTableExists(t, ctx, testDB, "anomalies")
	assertTableExists(t, ctx, testDB, schemaMigrationsTable)

	var applied int
	if err := testDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", schemaMigrationsTable)).Scan(&applied); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}

	if applied != 1 {
		t.Fatalf("expected 1 applied migration, got %d", applied)
	}
}

func TestRunMigrationsRejectsDuplicateProviderEventID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newTestDatabase(t, ctx)
	defer cleanup()

	if err := RunMigrations(ctx, testDB); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	paymentTimestamp := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	if _, err := testDB.ExecContext(ctx, `
		INSERT INTO payments (payment_id, status, status_timestamp)
		VALUES ($1, $2, $3)
	`, "payment-1", "pending", paymentTimestamp); err != nil {
		t.Fatalf("insert payment: %v", err)
	}

	if _, err := testDB.ExecContext(ctx, `
		INSERT INTO webhook_events (provider_event_id, payment_id, event_type, event_timestamp, raw_payload, processing_status)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, "evt-1", "payment-1", "payment.updated", paymentTimestamp, `{\"status\":\"pending\"}`, "pending"); err != nil {
		t.Fatalf("insert first webhook event: %v", err)
	}

	_, err := testDB.ExecContext(ctx, `
		INSERT INTO webhook_events (provider_event_id, payment_id, event_type, event_timestamp, raw_payload, processing_status)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, "evt-1", "payment-1", "payment.updated", paymentTimestamp, `{\"status\":\"pending\"}`, "pending")
	if err == nil {
		t.Fatal("expected duplicate provider_event_id insert to fail")
	}
}

func TestRunMigrationsSupportsPaymentUpsertByPaymentID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testDB, cleanup := newTestDatabase(t, ctx)
	defer cleanup()

	if err := RunMigrations(ctx, testDB); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	initialTimestamp := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	updatedTimestamp := initialTimestamp.Add(5 * time.Minute)

	if _, err := testDB.ExecContext(ctx, `
		INSERT INTO payments (payment_id, status, status_timestamp)
		VALUES ($1, $2, $3)
		ON CONFLICT (payment_id) DO UPDATE
		SET status = EXCLUDED.status,
		    status_timestamp = EXCLUDED.status_timestamp,
		    updated_at = NOW()
	`, "payment-1", "pending", initialTimestamp); err != nil {
		t.Fatalf("insert initial payment state: %v", err)
	}

	if _, err := testDB.ExecContext(ctx, `
		INSERT INTO payments (payment_id, status, status_timestamp)
		VALUES ($1, $2, $3)
		ON CONFLICT (payment_id) DO UPDATE
		SET status = EXCLUDED.status,
		    status_timestamp = EXCLUDED.status_timestamp,
		    updated_at = NOW()
	`, "payment-1", "captured", updatedTimestamp); err != nil {
		t.Fatalf("upsert payment state: %v", err)
	}

	var status string
	var statusTimestamp time.Time
	if err := testDB.QueryRowContext(ctx, `
		SELECT status, status_timestamp
		FROM payments
		WHERE payment_id = $1
	`, "payment-1").Scan(&status, &statusTimestamp); err != nil {
		t.Fatalf("query payment state: %v", err)
	}

	if status != "captured" {
		t.Fatalf("expected upserted status captured, got %q", status)
	}

	if !statusTimestamp.Equal(updatedTimestamp) {
		t.Fatalf("expected upserted status timestamp %s, got %s", updatedTimestamp, statusTimestamp)
	}
}

func newTestDatabase(t *testing.T, ctx context.Context) (*sql.DB, func()) {
	t.Helper()

	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	adminDB, err := Open(ctx, baseURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}

	schemaName := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA "%s"`, schemaName)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create schema: %v", err)
	}

	testDBURL, err := withSearchPath(baseURL, schemaName)
	if err != nil {
		_ = adminDB.Close()
		t.Fatalf("build schema-scoped database url: %v", err)
	}

	testDB, err := Open(ctx, testDBURL)
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName))
		_ = adminDB.Close()
		t.Fatalf("open test database: %v", err)
	}

	cleanup := func() {
		if err := testDB.Close(); err != nil {
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

func withSearchPath(databaseURL, schemaName string) (string, error) {
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

func assertTableExists(t *testing.T, ctx context.Context, db *sql.DB, tableName string) {
	t.Helper()

	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = $1
		)
	`, tableName).Scan(&exists); err != nil {
		t.Fatalf("check table %s existence: %v", tableName, err)
	}

	if !exists {
		t.Fatalf("expected table %s to exist", tableName)
	}
}
