package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/config"
	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
	"github.com/bartlomiejklimczak/smarthome-metrics/internal/pse"
	"github.com/bartlomiejklimczak/smarthome-metrics/internal/server"
	"github.com/bartlomiejklimczak/smarthome-metrics/internal/simulation"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("application fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	slog.Info("starting smarthome-metrics server", "port", cfg.Port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := cfg.GetDatabaseURL()
	store, err := db.NewPgxStore(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer store.Close()
	slog.Info("connected to PostgreSQL and initialized schema")

	router := server.NewRouter(store)

	if cfg.PSEEnabled {
		pseClient := pse.NewClient(cfg.PSEApiURL, nil)
		pseWorker := pse.NewWorker(pseClient, store, cfg.PSEFetchInterval, slog.Default())
		go pseWorker.Start(ctx)
	}

	if cfg.SimulationEnabled {
		simModel := simulation.NewBatteryModel(
			cfg.SimulationBatteryCapacityKWh,
			cfg.SimulationBatteryPowerKW,
			0.95,
			cfg.SimulationDistributionFee,
		)
		simWorker := simulation.NewWorker(store, simModel, cfg.SimulationInterval, slog.Default())
		go simWorker.Start(ctx)
	}

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening for HTTP requests", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server error: %w", err)
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	slog.Info("server shut down successfully")
	return nil
}
