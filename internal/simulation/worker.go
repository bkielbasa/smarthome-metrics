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

// Variant represents an individual simulated battery configuration.
type Variant struct {
	Name      string // e.g. "5kwh_5kw", "5kwh_10kw", "10kwh_5kw", "10kwh_10kw"
	Category  string // e.g. "simulation_5kwh_5kw"
	Model     *BatteryModel
	IsDefault bool // If true, also writes to legacy "simulation" category

	mu                   sync.Mutex
	cumulativeSavingsPLN float64
}

// NewVariant creates a new simulation Variant.
func NewVariant(name, category string, model *BatteryModel, isDefault bool) *Variant {
	return &Variant{
		Name:      name,
		Category:  category,
		Model:     model,
		IsDefault: isDefault,
	}
}

// CumulativeSavings returns the total running savings in PLN for this variant.
func (v *Variant) CumulativeSavings() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cumulativeSavingsPLN
}

// SetCumulativeSavings sets the total running savings in PLN for this variant.
func (v *Variant) SetCumulativeSavings(val float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cumulativeSavingsPLN = val
}

// AddCumulativeSavings adds a delta to the running savings in PLN and returns the updated total.
func (v *Variant) AddCumulativeSavings(delta float64) float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cumulativeSavingsPLN += delta
	return v.cumulativeSavingsPLN
}

// DefaultVariants returns the 4 standard simulation combinations of 5kW/10kW and 5kWh/10kWh:
// - 5kwh_5kw: 5 kWh capacity, 5 kW inverter
// - 5kwh_10kw: 5 kWh capacity, 10 kW inverter
// - 10kwh_5kw: 10 kWh capacity, 5 kW inverter (default)
// - 10kwh_10kw: 10 kWh capacity, 10 kW inverter
func DefaultVariants(efficiency, distributionFee float64) []*Variant {
	return []*Variant{
		NewVariant("5kwh_5kw", "simulation_5kwh_5kw", NewBatteryModel(5.0, 5.0, efficiency, distributionFee), false),
		NewVariant("5kwh_10kw", "simulation_5kwh_10kw", NewBatteryModel(5.0, 10.0, efficiency, distributionFee), false),
		NewVariant("10kwh_5kw", "simulation_10kwh_5kw", NewBatteryModel(10.0, 5.0, efficiency, distributionFee), true),
		NewVariant("10kwh_10kw", "simulation_10kwh_10kw", NewBatteryModel(10.0, 10.0, efficiency, distributionFee), false),
	}
}

// Worker periodically runs battery simulations across variants using live telemetry and persists simulated metrics.
type Worker struct {
	store    MetricStore
	variants []*Variant
	interval time.Duration
	logger   *slog.Logger
}

// NewWorker creates a new simulation Worker with the given variants.
// If variants is empty, DefaultVariants(0.95, 0.40) is used.
func NewWorker(store MetricStore, variants []*Variant, interval time.Duration, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	if len(variants) == 0 {
		variants = DefaultVariants(0.95, 0.40)
	}
	return &Worker{
		store:    store,
		variants: variants,
		interval: interval,
		logger:   logger,
	}
}

// NewSingleWorker creates a new simulation Worker for a single battery model.
func NewSingleWorker(store MetricStore, model *BatteryModel, interval time.Duration, logger *slog.Logger) *Worker {
	v := NewVariant("default", "simulation", model, true)
	return NewWorker(store, []*Variant{v}, interval, logger)
}

// Variants returns all configured variants for this worker.
func (w *Worker) Variants() []*Variant {
	return w.variants
}

// Rehydrate restores battery state of charge and cumulative savings from the metric store on boot for all variants.
func (w *Worker) Rehydrate(ctx context.Context) error {
	for _, v := range w.variants {
		soc, _, err := w.store.GetLatestMetric(ctx, v.Category, "sim_battery_soc_kwh")
		if err != nil && errors.Is(err, db.ErrMetricNotFound) && v.IsDefault {
			// Fallback to legacy "simulation" category for default variant
			soc, _, err = w.store.GetLatestMetric(ctx, "simulation", "sim_battery_soc_kwh")
		}
		if err != nil {
			if errors.Is(err, db.ErrMetricNotFound) {
				w.logger.InfoContext(ctx, "no prior battery soc found, using default 50% capacity", "variant", v.Name, "soc_kwh", v.Model.Energy())
			} else {
				return fmt.Errorf("failed to rehydrate battery soc for %s: %w", v.Name, err)
			}
		} else {
			v.Model.SetEnergy(soc)
			w.logger.InfoContext(ctx, "rehydrated battery soc from store", "variant", v.Name, "soc_kwh", soc)
		}

		savings, _, err := w.store.GetLatestMetric(ctx, v.Category, "sim_savings_pln")
		if err != nil && errors.Is(err, db.ErrMetricNotFound) && v.IsDefault {
			savings, _, err = w.store.GetLatestMetric(ctx, "simulation", "sim_savings_pln")
		}
		if err != nil {
			if !errors.Is(err, db.ErrMetricNotFound) {
				return fmt.Errorf("failed to rehydrate simulation savings for %s: %w", v.Name, err)
			}
		} else {
			v.SetCumulativeSavings(savings)
			w.logger.InfoContext(ctx, "rehydrated cumulative savings from store", "variant", v.Name, "savings_pln", savings)
		}
	}

	return nil
}

// CumulativeSavings returns the total running savings in PLN for the requested variant (or the default variant if omitted).
func (w *Worker) CumulativeSavings(variantName ...string) float64 {
	target := ""
	if len(variantName) > 0 {
		target = variantName[0]
	}
	for _, v := range w.variants {
		if target != "" && v.Name == target {
			return v.CumulativeSavings()
		}
		if target == "" && v.IsDefault {
			return v.CumulativeSavings()
		}
	}
	if len(w.variants) > 0 {
		return w.variants[0].CumulativeSavings()
	}
	return 0
}

// Step queries current PV, Load, and RCE telemetry, advances all variant models by deltaHours,
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

	for _, v := range w.variants {
		out := v.Model.Step(StepInput{
			LoadPowerW:    load,
			PVPowerW:      pv,
			RCEKWh:        rce,
			DurationHours: deltaHours,
		})

		cumSavings := v.AddCumulativeSavings(out.SavingsIntervalPLN)

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

		categories := []string{v.Category}
		if v.IsDefault && v.Category != "simulation" {
			categories = append(categories, "simulation")
		}

		// Note: in-memory model state and cumulative savings have already advanced.
		// If a transient DB insert error occurs, we return the error while preserving
		// state continuity so the subsequent tick will persist updated cumulative metrics.
		for _, cat := range categories {
			for _, rec := range records {
				if err := w.store.InsertMetric(ctx, cat, rec.name, rec.value); err != nil {
					return fmt.Errorf("failed to insert metric %s for %s (%s): %w", rec.name, v.Name, cat, err)
				}
			}
		}
	}

	return nil
}

// Start begins the simulation worker periodic loop. It first rehydrates the model states
// and then executes Step at every ticker interval until the context is cancelled.
func (w *Worker) Start(ctx context.Context) {
	w.logger.InfoContext(ctx, "starting simulation worker", "interval", w.interval, "variants", len(w.variants))

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
