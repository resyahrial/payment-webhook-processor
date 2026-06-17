package config

import "testing"

func TestLoadUsesDefaultsWhenEnvironmentVariablesAreUnset(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("WEBHOOK_SIGNING_SECRET", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("ENV", "")

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
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/db")
	t.Setenv("WEBHOOK_SIGNING_SECRET", "top-secret")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("ENV", "production")

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
}
