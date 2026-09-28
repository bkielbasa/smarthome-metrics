# Energy Storage Simulation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a background energy storage simulation subsystem and dedicated Grafana dashboard calculating real-time and historical theoretical battery earnings (daily, weekly, monthly, and yearly).

**Architecture:** A battery physics and economic simulation engine (`internal/simulation`) runs on a 1-minute ticker, pulling latest PV generation, home load, and PSE dynamic prices from PostgreSQL, advancing simulated battery state-of-charge with a Smart Hybrid arbitrage strategy, and persisting simulation metrics to `metrics` under `category = 'simulation'`. A new Grafana dashboard aggregates and visualizes theoretical financial returns and battery performance.

**Tech Stack:** Go 1.26, PostgreSQL (pgx/v5), Grafana JSON Dashboard v11.

**Spec:** `docs/superpowers/specs/2026-09-28-energy-storage-simulation-design.md`

## Global Constraints
- Target language: Go 1.26 standard library and existing dependencies (`github.com/jackc/pgx/v5`).
- All simulated telemetry is stored in the `metrics` table with `category = 'simulation'`.
- All database queries must use explicit PostgreSQL type casting for extended query protocol compatibility.
- Test-driven development: every component must have comprehensive unit tests (`go test -v -race ./...`).

---

### Task 1: Simulation Configuration

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `Config.SimulationEnabled` (`bool`)
  - `Config.SimulationInterval` (`time.Duration`)
  - `Config.SimulationBatteryCapacityKWh` (`float64`)
  - `Config.SimulationBatteryPowerKW` (`float64`)
  - `Config.SimulationDistributionFee` (`float64`)

- [ ] **Step 1: Write the failing tests in `internal/config/config_test.go`**

```go
func TestLoadConfig_SimulationDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SimulationEnabled {
		t.Errorf("expected SimulationEnabled to be true by default")
	}
	if cfg.SimulationInterval != time.Minute {
		t.Errorf("expected SimulationInterval to be 1m, got %v", cfg.SimulationInterval)
	}
	if cfg.SimulationBatteryCapacityKWh != 10.0 {
		t.Errorf("expected SimulationBatteryCapacityKWh to be 10.0, got %v", cfg.SimulationBatteryCapacityKWh)
	}
	if cfg.SimulationBatteryPowerKW != 5.0 {
		t.Errorf("expected SimulationBatteryPowerKW to be 5.0, got %v", cfg.SimulationBatteryPowerKW)
	}
	if cfg.SimulationDistributionFee != 0.40 {
		t.Errorf("expected SimulationDistributionFee to be 0.40, got %v", cfg.SimulationDistributionFee)
	}
}

func TestLoadConfig_SimulationCustomEnv(t *testing.T) {
	t.Setenv("SIMULATION_ENABLED", "false")
	t.Setenv("SIMULATION_INTERVAL", "30s")
	t.Setenv("SIMULATION_BATTERY_CAPACITY_KWH", "15.5")
	t.Setenv("SIMULATION_BATTERY_POWER_KW", "7.5")
	t.Setenv("SIMULATION_DISTRIBUTION_FEE", "0.45")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SimulationEnabled {
		t.Errorf("expected SimulationEnabled to be false")
	}
	if cfg.SimulationInterval != 30*time.Second {
		t.Errorf("expected SimulationInterval to be 30s, got %v", cfg.SimulationInterval)
	}
	if cfg.SimulationBatteryCapacityKWh != 15.5 {
		t.Errorf("expected SimulationBatteryCapacityKWh to be 15.5, got %v", cfg.SimulationBatteryCapacityKWh)
	}
	if cfg.SimulationBatteryPowerKW != 7.5 {
		t.Errorf("expected SimulationBatteryPowerKW to be 7.5, got %v", cfg.SimulationBatteryPowerKW)
	}
	if cfg.SimulationDistributionFee != 0.45 {
		t.Errorf("expected SimulationDistributionFee to be 0.45, got %v", cfg.SimulationDistributionFee)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestLoadConfig_Simulation ./internal/config`
Expected: Compilation failure (fields not defined on `Config`).

- [ ] **Step 3: Add simulation configuration fields and parsing in `internal/config/config.go`**

Add fields to `Config` struct:
```go
	SimulationEnabled            bool
	SimulationInterval           time.Duration
	SimulationBatteryCapacityKWh float64
	SimulationBatteryPowerKW     float64
	SimulationDistributionFee    float64
```

In `Load()` function:
```go
	simEnabled := true
	if val := os.Getenv("SIMULATION_ENABLED"); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			simEnabled = parsed
		}
	}

	simInterval := time.Minute
	if val := os.Getenv("SIMULATION_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			simInterval = d
		}
	}

	simCapacity := 10.0
	if val := os.Getenv("SIMULATION_BATTERY_CAPACITY_KWH"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
			simCapacity = f
		}
	}

	simPower := 5.0
	if val := os.Getenv("SIMULATION_BATTERY_POWER_KW"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
			simPower = f
		}
	}

	simFee := 0.40
	if val := os.Getenv("SIMULATION_DISTRIBUTION_FEE"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f >= 0 {
			simFee = f
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/config`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add energy storage simulation configuration options"
```

---

### Task 2: Database Store Helper for Fetching Latest Metrics

**Files:**
- Modify: `internal/db/store.go`
- Modify: `internal/db/db.go`
- Modify: `internal/db/store_test.go`

**Interfaces:**
- Produces:
  - `MetricStore.GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error)`

- [ ] **Step 1: Write failing test in `internal/db/store_test.go`**

```go
func TestMockStore_GetLatestMetric(t *testing.T) {
	mock := &MockStore{}
	now := time.Now().UTC()
	err := mock.InsertMetricWithTimestamp(context.Background(), now, "simulation", "sim_battery_soc_kwh", 7.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, ts, err := mock.GetLatestMetric(context.Background(), "simulation", "sim_battery_soc_kwh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 7.5 {
		t.Errorf("expected value 7.5, got %v", val)
	}
	if !ts.Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, ts)
	}

	// Missing metric
	_, _, err = mock.GetLatestMetric(context.Background(), "unknown", "unknown")
	if err != ErrMetricNotFound {
		t.Errorf("expected ErrMetricNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestMockStore_GetLatestMetric ./internal/db`
Expected: Compilation failure (`GetLatestMetric` not declared on `MetricStore`).

- [ ] **Step 3: Update `MetricStore` interface, `ErrMetricNotFound`, `MockStore`, and `PgxStore`**

In `internal/db/store.go`:
```go
var ErrMetricNotFound = errors.New("metric not found")

type MetricStore interface {
	InsertMetric(ctx context.Context, category, metricName string, value float64) error
	InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error
	GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error)
	Ping(ctx context.Context) error
	Close()
}
```

In `internal/db/db.go`:
```go
func (s *PgxStore) GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error) {
	query := `SELECT value, timestamp FROM metrics 
WHERE category = $1::text AND metric_name = $2::text 
ORDER BY timestamp DESC LIMIT 1`
	var val float64
	var ts time.Time
	err := s.pool.QueryRow(ctx, query, category, metricName).Scan(&val, &ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, ErrMetricNotFound
	}
	return val, ts, err
}
```

In `MockStore` (`internal/db/store.go`):
Implement `GetLatestMetric` finding the most recent record matching category and metricName.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/db`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/store.go internal/db/db.go internal/db/store_test.go
git commit -m "feat(db): add GetLatestMetric to MetricStore"
```

---

### Task 3: Battery Physics & Economic Model

**Files:**
- Create: `internal/simulation/model.go`
- Create: `internal/simulation/model_test.go`

**Interfaces:**
- Produces:
  - `type BatteryModel struct`
  - `NewBatteryModel(capacityKWh, maxPowerKW, efficiency, distributionFee float64) *BatteryModel`
  - `type StepInput struct { LoadPowerW, PVPowerW, RCEKWh, DurationHours float64 }`
  - `type StepOutput struct { BatteryEnergyKWh, BatteryPowerW, BatterySoCPct, SavingsIntervalPLN, NetGridPowerW float64 }`
  - `BatteryModel.Step(in StepInput) StepOutput`
  - `BatteryModel.SetEnergy(kwh float64)`

- [ ] **Step 1: Write unit tests in `internal/simulation/model_test.go`**

```go
package simulation

import (
	"testing"
)

func TestBatteryModel_SurplusCharging(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(0.0)

	// Solar = 6000W, Load = 1000W -> Surplus = 5000W
	// Duration = 1 hour (1.0h)
	out := model.Step(StepInput{
		LoadPowerW:    1000,
		PVPowerW:      6000,
		RCEKWh:        0.50,
		DurationHours: 1.0,
	})

	// Battery power should be clamped to MaxPowerKW = 5000W
	if out.BatteryPowerW != 5000 {
		t.Errorf("expected BatteryPowerW = 5000, got %v", out.BatteryPowerW)
	}
	// With 0.95 efficiency: 5 kWh * 0.95 = 4.75 kWh stored
	expectedEnergy := 5.0 * 0.95
	if out.BatteryEnergyKWh != expectedEnergy {
		t.Errorf("expected BatteryEnergyKWh = %v, got %v", expectedEnergy, out.BatteryEnergyKWh)
	}
}

func TestBatteryModel_DeficitDischarging(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(5.0)

	// Solar = 0W, Load = 2000W -> Deficit = 2000W
	// Duration = 1 hour (1.0h)
	out := model.Step(StepInput{
		LoadPowerW:    2000,
		PVPowerW:      0,
		RCEKWh:        0.60,
		DurationHours: 1.0,
	})

	// Battery discharges to supply 2000W to load
	if out.BatteryPowerW != -2000 {
		t.Errorf("expected BatteryPowerW = -2000, got %v", out.BatteryPowerW)
	}
	// Net grid power should be 0 (all supplied by battery)
	if out.NetGridPowerW != 0 {
		t.Errorf("expected NetGridPowerW = 0, got %v", out.NetGridPowerW)
	}
	// Savings: avoided buying 2 kWh at (0.60 + 0.40) = 2.00 PLN
	if out.SavingsIntervalPLN <= 0 {
		t.Errorf("expected positive savings, got %v", out.SavingsIntervalPLN)
	}
}

func TestBatteryModel_CapacityClamping(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 1.0, 0.40)
	model.SetEnergy(9.0)

	// Surplus 5000W for 1h would add 5 kWh -> capped at 10.0 kWh
	out := model.Step(StepInput{
		LoadPowerW:    0,
		PVPowerW:      5000,
		RCEKWh:        0.50,
		DurationHours: 1.0,
	})
	if out.BatteryEnergyKWh != 10.0 {
		t.Errorf("expected energy capped at 10.0, got %v", out.BatteryEnergyKWh)
	}
}

func TestBatteryModel_LowPriceArbitrage(t *testing.T) {
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(2.0)

	// Night time: Solar = 0, Load = 500W, RCE is very low = 0.05 PLN/kWh
	out := model.Step(StepInput{
		LoadPowerW:    500,
		PVPowerW:      0,
		RCEKWh:        0.05,
		DurationHours: 1.0,
	})

	// When price is ultra low (< 0.15), battery charges from grid rather than discharging
	if out.BatteryPowerW <= 0 {
		t.Errorf("expected battery to charge from cheap grid, got power %v", out.BatteryPowerW)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/simulation`
Expected: Compilation failure (package `simulation` not implemented).

- [ ] **Step 3: Implement `internal/simulation/model.go`**

Implement physics, inverter clamping, state of charge bounds, and economic cashflow calculation for baseline vs simulated scenarios.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/simulation`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/simulation/model.go internal/simulation/model_test.go
git commit -m "feat(simulation): add battery physics and financial simulation model"
```

---

### Task 4: Simulation Worker

**Files:**
- Create: `internal/simulation/worker.go`
- Create: `internal/simulation/worker_test.go`

**Interfaces:**
- Consumes:
  - `MetricStore.GetLatestMetric`
  - `MetricStore.InsertMetric`
  - `BatteryModel.Step`
- Produces:
  - `type Worker struct`
  - `NewWorker(store MetricStore, model *BatteryModel, interval time.Duration, logger *slog.Logger) *Worker`
  - `Worker.Start(ctx context.Context)`
  - `Worker.Step(ctx context.Context, deltaHours float64) error`

- [ ] **Step 1: Write worker unit tests in `internal/simulation/worker_test.go`**

Verify:
- Rehydration on boot loads existing `sim_battery_soc_kwh` from store.
- `Step()` queries PV, Load, and RCE, computes model step, and inserts:
  - `sim_battery_soc_kwh`
  - `sim_battery_soc_pct`
  - `sim_battery_power_w`
  - `sim_savings_interval_pln`
  - `sim_savings_pln`
- Graceful shutdown when context is cancelled.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestWorker ./internal/simulation`
Expected: Compilation failure (`Worker` not defined).

- [ ] **Step 3: Implement `internal/simulation/worker.go`**

Implement the worker with `Rehydrate()`, `Step()`, and `Start()` ticker loop.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/simulation`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/simulation/worker.go internal/simulation/worker_test.go
git commit -m "feat(simulation): add continuous simulation worker"
```

---

### Task 5: Server Wiring & Integration

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `charts/smarthome-metrics/values.yaml`

**Interfaces:**
- Consumes:
  - `config.Config.Simulation*`
  - `simulation.NewWorker`
  - `simulation.NewBatteryModel`

- [ ] **Step 1: Wire simulation worker into `cmd/server/main.go`**

In `cmd/server/main.go`, when `cfg.SimulationEnabled` is true:
```go
	if cfg.SimulationEnabled {
		simModel := simulation.NewBatteryModel(
			cfg.SimulationBatteryCapacityKWh,
			cfg.SimulationBatteryPowerKW,
			0.95,
			cfg.SimulationDistributionFee,
		)
		simWorker := simulation.NewWorker(store, simModel, cfg.SimulationInterval, logger)
		go simWorker.Start(serverCtx)
	}
```

- [ ] **Step 2: Update `charts/smarthome-metrics/values.yaml`**

Add `simulation:` configuration block.

- [ ] **Step 3: Verify build and test suite**

Run:
```bash
go test -count=1 -race ./...
go build ./cmd/server
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go charts/smarthome-metrics/values.yaml
git commit -m "feat: integrate battery simulation worker into server lifecycle"
```

---

### Task 6: Grafana Simulation Dashboard

**Files:**
- Create: `dashboards/smarthome-symulacja.json`
- Modify: `dashboards/README.md`

- [ ] **Step 1: Construct `dashboards/smarthome-symulacja.json`**

Include:
- 4 Stat Cards: Today's Savings, Last 7 Days, Last 30 Days, Projected Yearly.
- 2 Gauges: Simulated Battery SoC (%), Current Battery Power (W).
- 2 Time Series Panels: Battery SoC & Power Curve, Cumulative Savings Curve.
- Tags: `["smarthome", "simulation", "battery", "energy", "rce"]`.

- [ ] **Step 2: Validate JSON schema**

Run: `python3 -m json.tool dashboards/smarthome-symulacja.json > /dev/null`
Expected: Return code 0 (valid JSON).

- [ ] **Step 3: Document dashboard in `dashboards/README.md`**

Add section describing `smarthome-symulacja.json`.

- [ ] **Step 4: Commit**

```bash
git add dashboards/smarthome-symulacja.json dashboards/README.md
git commit -m "feat(dashboards): add energy storage simulation dashboard"
```

---

### Task 7: Full System Verification & Push

- [ ] **Step 1: Run comprehensive test suite**

Run: `go test -v -count=1 -race ./...`
Expected: All tests PASS.

- [ ] **Step 2: Verify git status and commit cleanliness**

Run: `git status`
Expected: Clean working tree.
