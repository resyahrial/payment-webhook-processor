package main

import (
	"context"
	"errors"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"payment-webhook-processor/internal/config"
	"payment-webhook-processor/internal/db"
	apphttp "payment-webhook-processor/internal/http"
	"payment-webhook-processor/internal/logging"
	"payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/service"
)

const shutdownTimeout = 10 * time.Second
const startupTimeout = 10 * time.Second

func main() {
	cfg := config.Load()
	logger := logging.NewJSONLogger(os.Stdout)
	appMetrics := metrics.New()
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	poolConfig := db.PoolConfig{
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
	}
	database, err := db.Open(startupCtx, cfg.DatabaseURL, poolConfig)
	if err != nil {
		logger.Error().Err(err).Msg("database initialization failed")
		os.Exit(1)
	}
	defer func() {
		if err := appMetrics.Shutdown(context.Background()); err != nil {
			logger.Error().Err(err).Msg("metrics shutdown failed")
		}
		if err := database.Close(); err != nil {
			logger.Error().Err(err).Msg("database close failed")
		}
	}()
	if err := appMetrics.RegisterDatabaseStatsCollector(database); err != nil {
		logger.Error().Err(err).Msg("database metrics registration failed")
		os.Exit(1)
	}

	if err := db.RunMigrations(startupCtx, database); err != nil {
		logger.Error().Err(err).Msg("database migration failed")
		os.Exit(1)
	}

	webhookEventRepository := repository.NewWebhookEventRepository(database, appMetrics)
	paymentRepository := repository.NewPaymentRepository(database, appMetrics)
	anomalyRepository := repository.NewAnomalyRepository(database, appMetrics)
	paymentProcessor := service.NewPaymentProcessor(paymentRepository, anomalyRepository, appMetrics)
	webhookProcessor := service.NewIdempotencyService(webhookEventRepository, paymentProcessor.Update, appMetrics)
	webhookHandler := apphttp.NewWebhookHandler(cfg.WebhookSigningSecret, webhookProcessor, logger, appMetrics)

	server := &stdhttp.Server{
		Addr:    ":" + cfg.Port,
		Handler: apphttp.NewRouter(webhookHandler, appMetrics.Handler()),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info().Str("addr", server.Addr).Str("environment", cfg.Environment).Msg("starting server")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			serverErrors <- err
		}
		close(serverErrors)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error().Err(err).Msg("graceful shutdown failed")
			os.Exit(1)
		}
	case err, ok := <-serverErrors:
		if ok && err != nil {
			logger.Error().Err(err).Msg("server failed")
			os.Exit(1)
		}
	}
}
