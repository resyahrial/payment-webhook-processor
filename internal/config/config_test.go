package config

import (
	"testing"
	"time"
)

func TestLoadUsesDefaultsWhenEnvironmentVariablesAreUnset(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("WEBHOOK_SIGNING_SECRET", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("ENV", "")
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	t.Setenv("DB_MAX_IDLE_CONNS", "")
	t.Setenv("DB_CONN_MAX_LIFETIME", "")

	cfg := Load()

	if cfg.Port != "8080" {
		t.Fatalf("expected default port 8080, got %q", cfg.Port)
	}

	if cfg.DatabaseURL != "" {
		t.Fatalf("expected empty database url, got %q", cfg.DatabaseURL)
	}

	if cfg.WebhookSigningSecret != "" {
		t.Fatalf("expected empty webhook signing secret, got %q", cfg.WebhookSigningSecret)
	}

	if cfg.Environment != "development" {
		t.Fatalf("expected default environment development, got %q", cfg.Environment)
	}

	if cfg.DBMaxOpenConns != defaultDBMaxOpenConns {
		t.Fatalf("expected default DB max open conns %d, got %d", defaultDBMaxOpenConns, cfg.DBMaxOpenConns)
	}

	if cfg.DBMaxIdleConns != defaultDBMaxIdleConns {
		t.Fatalf("expected default DB max idle conns %d, got %d", defaultDBMaxIdleConns, cfg.DBMaxIdleConns)
	}

	if cfg.DBConnMaxLifetime != defaultDBConnMaxLifetime {
		t.Fatalf("expected default DB conn max lifetime %s, got %s", defaultDBConnMaxLifetime, cfg.DBConnMaxLifetime)
	}
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("WEBHOOK_SIGNING_SECRET", "top-secret")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("ENV", "production")
	t.Setenv("DB_MAX_OPEN_CONNS", "21")
	t.Setenv("DB_MAX_IDLE_CONNS", "7")
	t.Setenv("DB_CONN_MAX_LIFETIME", "45m")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Fatalf("expected port override 9090, got %q", cfg.Port)
	}

	if cfg.DatabaseURL != "postgres://user:pass@localhost/db" {
		t.Fatalf("expected database url override, got %q", cfg.DatabaseURL)
	}

	if cfg.WebhookSigningSecret != "top-secret" {
		t.Fatalf("expected signing secret override, got %q", cfg.WebhookSigningSecret)
	}

	if cfg.Environment != "staging" {
		t.Fatalf("expected APP_ENV to take precedence, got %q", cfg.Environment)
	}

	if cfg.DBMaxOpenConns != 21 {
		t.Fatalf("expected DB max open conns override 21, got %d", cfg.DBMaxOpenConns)
	}

	if cfg.DBMaxIdleConns != 7 {
		t.Fatalf("expected DB max idle conns override 7, got %d", cfg.DBMaxIdleConns)
	}

	if cfg.DBConnMaxLifetime != 45*time.Minute {
		t.Fatalf("expected DB conn max lifetime override 45m, got %s", cfg.DBConnMaxLifetime)
	}
}

func TestLoadFallsBackForInvalidDatabasePoolOverrides(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "-1")
	t.Setenv("DB_MAX_IDLE_CONNS", "bad")
	t.Setenv("DB_CONN_MAX_LIFETIME", "not-a-duration")

	cfg := Load()

	if cfg.DBMaxOpenConns != defaultDBMaxOpenConns {
		t.Fatalf("expected invalid DB max open conns to fall back to %d, got %d", defaultDBMaxOpenConns, cfg.DBMaxOpenConns)
	}

	if cfg.DBMaxIdleConns != defaultDBMaxIdleConns {
		t.Fatalf("expected invalid DB max idle conns to fall back to %d, got %d", defaultDBMaxIdleConns, cfg.DBMaxIdleConns)
	}

	if cfg.DBConnMaxLifetime != defaultDBConnMaxLifetime {
		t.Fatalf("expected invalid DB conn max lifetime to fall back to %s, got %s", defaultDBConnMaxLifetime, cfg.DBConnMaxLifetime)
	}
}
