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
	apphttp "payment-webhook-processor/internal/http"
)

const shutdownTimeout = 10 * time.Second

func main() {
	cfg := config.Load()
	server := &stdhttp.Server{
		Addr:    ":" + cfg.Port,
		Handler: apphttp.NewRouter(),
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
