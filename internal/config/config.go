package config

import (
	"os"
	"strconv"
	"time"
)

const defaultPort = "8080"
const defaultEnvironment = "development"
const defaultDBMaxOpenConns = 10
const defaultDBMaxIdleConns = 5
const defaultDBConnMaxLifetime = 30 * time.Minute

type Config struct {
	Port                 string
	DatabaseURL          string
	WebhookSigningSecret string
	Environment          string
	DBMaxOpenConns       int
	DBMaxIdleConns       int
	DBConnMaxLifetime    time.Duration
}

func Load() Config {
	return Config{
		Port:                 envOrDefault("PORT", defaultPort),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		WebhookSigningSecret: os.Getenv("WEBHOOK_SIGNING_SECRET"),
		Environment:          firstNonEmpty(os.Getenv("APP_ENV"), os.Getenv("ENV"), defaultEnvironment),
		DBMaxOpenConns:       intEnvOrDefault("DB_MAX_OPEN_CONNS", defaultDBMaxOpenConns),
		DBMaxIdleConns:       intEnvOrDefault("DB_MAX_IDLE_CONNS", defaultDBMaxIdleConns),
		DBConnMaxLifetime:    durationEnvOrDefault("DB_CONN_MAX_LIFETIME", defaultDBConnMaxLifetime),
	}
}

func envOrDefault(key, fallback string) string {
	return firstNonEmpty(os.Getenv(key), fallback)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func intEnvOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}

	return parsed
}

func durationEnvOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return fallback
	}

	return parsed
}
