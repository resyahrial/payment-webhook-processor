package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrMissingDatabaseURL = errors.New("database url is required")

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func Open(ctx context.Context, databaseURL string, poolConfig PoolConfig) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, ErrMissingDatabaseURL
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	ApplyPoolConfig(db, poolConfig)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func ApplyPoolConfig(db *sql.DB, poolConfig PoolConfig) {
	if db == nil {
		return
	}

	if poolConfig.MaxOpenConns >= 0 {
		db.SetMaxOpenConns(poolConfig.MaxOpenConns)
	}
	if poolConfig.MaxIdleConns >= 0 {
		db.SetMaxIdleConns(poolConfig.MaxIdleConns)
	}
	if poolConfig.ConnMaxLifetime >= 0 {
		db.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)
	}
}
