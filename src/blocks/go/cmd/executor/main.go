package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hcm-next-executor/internal/blocks/legalname"
	"hcm-next-executor/internal/executor"
)

const defaultAddress = ":7001"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	registry := executor.NewRegistry()

	if err := legalname.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}

	address := os.Getenv("EXECUTOR_ADDR")
	if address == "" {
		address = defaultAddress
	}

	service := executor.NewServer(registry)
	httpServer := executor.NewHTTPServer(address, service.Routes())

	shutdownContext, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	go func() {
		logger.Info("starting executor service", "address", address)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("executor service failed", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdownContext.Done()

	gracefulContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(gracefulContext); err != nil {
		logger.Error("executor service shutdown failed", "error", err)
		os.Exit(1)
	}
}
