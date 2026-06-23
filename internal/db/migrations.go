package db

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaMigrationsTable = "schema_migrations"

type migration struct {
	version string
	sql     string
}

var migrations = []migration{
	{
		version: "001_init",
		sql: initialMigrationSQL,
	},
}

func RunMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, schemaMigrationsTable)); err != nil {
		return fmt.Errorf("ensure schema migrations table: %w", err)
	}

	for _, migration := range migrations {
		applied, err := migrationApplied(ctx, db, migration.version)
		if err != nil {
			return err
		}

		if applied {
			continue
		}

		if err := applyMigration(ctx, db, migration); err != nil {
			return err
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1 FROM %s WHERE version = $1
		)
	`, schemaMigrationsTable), version).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}

	return exists, nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.version, err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, migration.sql); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.version, err)
	}

	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (version) VALUES ($1)
	`, schemaMigrationsTable), migration.version); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.version, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.version, err)
	}

	return nil
}

const initialMigrationSQL = `
CREATE TABLE IF NOT EXISTS payments (
	payment_id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	status_timestamp TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_events (
	id BIGSERIAL PRIMARY KEY,
	provider_event_id TEXT NOT NULL UNIQUE,
	payment_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_timestamp TIMESTAMPTZ NOT NULL,
	raw_payload JSONB NOT NULL,
	processing_status TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS anomalies (
	id BIGSERIAL PRIMARY KEY,
	webhook_event_id BIGINT REFERENCES webhook_events(id) ON DELETE SET NULL,
	payment_id TEXT REFERENCES payments(payment_id) ON DELETE SET NULL,
	anomaly_type TEXT NOT NULL,
	details JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
