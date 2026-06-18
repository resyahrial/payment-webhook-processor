package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestOpenRejectsEmptyDatabaseURL(t *testing.T) {
	t.Parallel()

	_, err := Open(context.Background(), "", PoolConfig{})
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Fatalf("expected ErrMissingDatabaseURL, got %v", err)
	}
}

func TestApplyPoolConfigSetsDatabaseLimits(t *testing.T) {
	t.Parallel()

	database := &sql.DB{}
	ApplyPoolConfig(database, PoolConfig{
		MaxOpenConns:    12,
		MaxIdleConns:    4,
		ConnMaxLifetime: 15 * time.Minute,
	})

	stats := database.Stats()
	if stats.MaxOpenConnections != 12 {
		t.Fatalf("expected max open connections 12, got %d", stats.MaxOpenConnections)
	}
}
