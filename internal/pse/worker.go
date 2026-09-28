package pse

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

type PriceFetcher interface {
	FetchPrices(ctx context.Context, fromDate string) ([]PriceRecord, error)
}

type Worker struct {
	fetcher  PriceFetcher
	store    db.MetricStore
	interval time.Duration
	logger   *slog.Logger
}

func NewWorker(fetcher PriceFetcher, store db.MetricStore, interval time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		fetcher:  fetcher,
		store:    store,
		interval: interval,
		logger:   logger,
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.logger.InfoContext(ctx, "starting pse price fetcher worker", "interval", w.interval)

	// Immediate sync on start
	if err := w.Sync(ctx); err != nil {
		w.logger.WarnContext(ctx, "initial pse price sync failed", "error", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "stopping pse price fetcher worker")
			return
		case <-ticker.C:
			if err := w.Sync(ctx); err != nil {
				w.logger.WarnContext(ctx, "pse periodic price sync failed", "error", err)
			}
		}
	}
}

func (w *Worker) Sync(ctx context.Context) error {
	loc, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		loc = time.FixedZone("CEST", 2*3600)
	}

	today := time.Now().In(loc).Format("2006-01-02")

	records, err := w.fetcher.FetchPrices(ctx, today)
	if err != nil {
		return fmt.Errorf("failed to fetch pse prices: %w", err)
	}

	var insertedCount int
	for _, rec := range records {
		if err := w.store.InsertMetricWithTimestamp(ctx, rec.Timestamp, "energy_market", "rce_mwh", rec.RCEMWh); err != nil {
			w.logger.WarnContext(ctx, "failed to insert rce_mwh metric", "timestamp", rec.Timestamp, "error", err)
		} else {
			insertedCount++
		}

		if err := w.store.InsertMetricWithTimestamp(ctx, rec.Timestamp, "energy_market", "rce_kwh", rec.RCEKWh); err != nil {
			w.logger.WarnContext(ctx, "failed to insert rce_kwh metric", "timestamp", rec.Timestamp, "error", err)
		} else {
			insertedCount++
		}
	}

	w.logger.InfoContext(ctx, "pse price sync completed", "records_fetched", len(records), "inserted_or_checked", insertedCount)
	return nil
}
