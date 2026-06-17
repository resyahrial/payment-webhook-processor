package config

import "os"

const defaultPort = "8080"
const defaultEnvironment = "development"

type Config struct {
	Port                 string
	DatabaseURL          string
	WebhookSigningSecret string
	Environment          string
}

func Load() Config {
	return Config{
		Port:                 envOrDefault("PORT", defaultPort),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		WebhookSigningSecret: os.Getenv("WEBHOOK_SIGNING_SECRET"),
		Environment:          firstNonEmpty(os.Getenv("APP_ENV"), os.Getenv("ENV"), defaultEnvironment),
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
