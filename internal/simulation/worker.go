package simulation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

// MetricStore defines the subset of persistence operations required by the simulation worker.
type MetricStore interface {
	GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error)
	InsertMetric(ctx context.Context, category, metricName string, value float64) error
}

// Worker periodically runs battery simulations using live telemetry and persists simulated metrics.
type Worker struct {
	store    MetricStore
	model    *BatteryModel
	interval time.Duration
	logger   *slog.Logger

	mu                   sync.Mutex
	cumulativeSavingsPLN float64
}

// NewWorker creates a new simulation Worker.
func NewWorker(store MetricStore, model *BatteryModel, interval time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	return &Worker{
		store:    store,
		model:    model,
		interval: interval,
		logger:   logger,
	}
}

// Rehydrate restores battery state of charge and cumulative savings from the metric store on boot.
func (w *Worker) Rehydrate(ctx context.Context) error {
	soc, _, err := w.store.GetLatestMetric(ctx, "simulation", "sim_battery_soc_kwh")
	if err != nil {
		if errors.Is(err, db.ErrMetricNotFound) {
			w.logger.InfoContext(ctx, "no prior battery soc found, using default 50% capacity", "soc_kwh", w.model.Energy())
		} else {
			return fmt.Errorf("failed to rehydrate battery soc: %w", err)
		}
	} else {
		w.model.SetEnergy(soc)
		w.logger.InfoContext(ctx, "rehydrated battery soc from store", "soc_kwh", soc)
	}

	savings, _, err := w.store.GetLatestMetric(ctx, "simulation", "sim_savings_pln")
	if err != nil {
		if !errors.Is(err, db.ErrMetricNotFound) {
			return fmt.Errorf("failed to rehydrate simulation savings: %w", err)
		}
	} else {
		w.mu.Lock()
		w.cumulativeSavingsPLN = savings
		w.mu.Unlock()
		w.logger.InfoContext(ctx, "rehydrated cumulative savings from store", "savings_pln", savings)
	}

	return nil
}

// CumulativeSavings returns the total running savings in PLN since simulation inception.
func (w *Worker) CumulativeSavings() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cumulativeSavingsPLN
}

// Step queries current PV, Load, and RCE telemetry, advances the battery model by deltaHours,
// and persists calculated simulation metrics to the store.
func (w *Worker) Step(ctx context.Context, deltaHours float64) error {
	pv, _, err := w.store.GetLatestMetric(ctx, "photovoltaics", "current")
	if err != nil {
		w.logger.WarnContext(ctx, "failed to get pv metric for simulation step", "error", err)
		return fmt.Errorf("failed to get pv metric: %w", err)
	}

	load, _, err := w.store.GetLatestMetric(ctx, "main_meter", "current_usage")
	if err != nil {
		w.logger.WarnContext(ctx, "failed to get load metric for simulation step", "error", err)
		return fmt.Errorf("failed to get load metric: %w", err)
	}

	rce, _, err := w.store.GetLatestMetric(ctx, "energy_market", "rce_kwh")
	if err != nil {
		w.logger.WarnContext(ctx, "failed to get rce metric for simulation step", "error", err)
		return fmt.Errorf("failed to get rce metric: %w", err)
	}

	out := w.model.Step(StepInput{
		LoadPowerW:    load,
		PVPowerW:      pv,
		RCEKWh:        rce,
		DurationHours: deltaHours,
	})

	w.mu.Lock()
	w.cumulativeSavingsPLN += out.SavingsIntervalPLN
	cumSavings := w.cumulativeSavingsPLN
	w.mu.Unlock()

	records := []struct {
		name  string
		value float64
	}{
		{"sim_battery_soc_kwh", out.BatteryEnergyKWh},
		{"sim_battery_soc_pct", out.BatterySoCPct},
		{"sim_battery_power_w", out.BatteryPowerW},
		{"sim_savings_interval_pln", out.SavingsIntervalPLN},
		{"sim_savings_pln", cumSavings},
	}

	for _, rec := range records {
		if err := w.store.InsertMetric(ctx, "simulation", rec.name, rec.value); err != nil {
			return fmt.Errorf("failed to insert metric %s: %w", rec.name, err)
		}
	}

	return nil
}

// Start begins the simulation worker periodic loop. It first rehydrates the model state
// and then executes Step at every ticker interval until the context is cancelled.
func (w *Worker) Start(ctx context.Context) {
	w.logger.InfoContext(ctx, "starting simulation worker", "interval", w.interval)

	if err := w.Rehydrate(ctx); err != nil {
		w.logger.WarnContext(ctx, "failed to rehydrate simulation state", "error", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	deltaHours := w.interval.Hours()

	for {
		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "stopping simulation worker")
			return
		case <-ticker.C:
			if err := w.Step(ctx, deltaHours); err != nil {
				w.logger.WarnContext(ctx, "simulation step failed", "error", err)
			}
		}
	}
}
