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

	"human-capital-management-suite-executor/internal/blocks/compensation"
	"human-capital-management-suite-executor/internal/blocks/contactinfo"
	"human-capital-management-suite-executor/internal/blocks/emergencycontact"
	"human-capital-management-suite-executor/internal/blocks/legalname"
	"human-capital-management-suite-executor/internal/blocks/orgtransfer"
	"human-capital-management-suite-executor/internal/blocks/termination"
	"human-capital-management-suite-executor/internal/executor"
)

const defaultAddress = ":7001"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	registry := executor.NewRegistry()

	if err := legalname.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}
	if err := emergencycontact.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}
	if err := contactinfo.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}
	if err := compensation.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}
	if err := orgtransfer.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}
	if err := termination.RegisterBlocks(registry); err != nil {
		logger.Error("failed to register executor blocks", "error", err)
		os.Exit(1)
	}

	address := os.Getenv("EXECUTOR_ADDR")
	if address == "" {
		address = defaultAddress
	}

	service := executor.NewServer(registry, logger.With("service", "executor"))
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
