package main

import (
	"context"
	"errors"
	"log"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"payment-webhook-processor/internal/config"
	"payment-webhook-processor/internal/db"
	apphttp "payment-webhook-processor/internal/http"
	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/service"
)

const shutdownTimeout = 10 * time.Second
const startupTimeout = 10 * time.Second

func main() {
	cfg := config.Load()
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	database, err := db.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("database initialization failed: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("database close failed: %v", err)
		}
	}()

	if err := db.RunMigrations(startupCtx, database); err != nil {
		log.Printf("database migration failed: %v", err)
		os.Exit(1)
	}

	webhookEventRepository := repository.NewWebhookEventRepository(database)
	paymentRepository := repository.NewPaymentRepository(database)
	anomalyRepository := repository.NewAnomalyRepository(database)
	paymentProcessor := service.NewPaymentProcessor(paymentRepository, anomalyRepository)
	webhookProcessor := service.NewIdempotencyService(webhookEventRepository, paymentProcessor.Update)
	webhookHandler := apphttp.NewWebhookHandler(cfg.WebhookSigningSecret, webhookProcessor)

	server := &stdhttp.Server{
		Addr:    ":" + cfg.Port,
		Handler: apphttp.NewRouter(webhookHandler),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("starting server on %s in %s environment", server.Addr, cfg.Environment)
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
			log.Printf("graceful shutdown failed: %v", err)
			os.Exit(1)
		}
	case err, ok := <-serverErrors:
		if ok && err != nil {
			log.Printf("server failed: %v", err)
			os.Exit(1)
		}
	}
}
