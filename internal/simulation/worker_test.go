package simulation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

type mockMetricRecord struct {
	category   string
	metricName string
	value      float64
	timestamp  time.Time
}

type testStore struct {
	mu           sync.Mutex
	latestValues map[string]float64
	inserts      []mockMetricRecord
	getError     error
	insertError  error
}

func newTestStore() *testStore {
	return &testStore{
		latestValues: make(map[string]float64),
	}
}

func (m *testStore) key(category, metricName string) string {
	return category + "/" + metricName
}

func (m *testStore) setLatest(category, metricName string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latestValues[m.key(category, metricName)] = value
}

func (m *testStore) GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getError != nil {
		return 0, time.Time{}, m.getError
	}
	val, ok := m.latestValues[m.key(category, metricName)]
	if !ok {
		return 0, time.Time{}, db.ErrMetricNotFound
	}
	return val, time.Now().UTC(), nil
}

func (m *testStore) InsertMetric(ctx context.Context, category, metricName string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertError != nil {
		return m.insertError
	}
	m.inserts = append(m.inserts, mockMetricRecord{
		category:   category,
		metricName: metricName,
		value:      value,
		timestamp:  time.Now().UTC(),
	})
	m.latestValues[m.key(category, metricName)] = value
	return nil
}

func (m *testStore) getInserts() []mockMetricRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockMetricRecord, len(m.inserts))
	copy(cp, m.inserts)
	return cp
}

func TestWorker_Rehydrate_LoadsExistingState(t *testing.T) {
	store := newTestStore()
	store.setLatest("simulation", "sim_battery_soc_kwh", 7.5)
	store.setLatest("simulation", "sim_savings_pln", 34.20)

	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	worker := NewSingleWorker(store, model, time.Minute, nil)

	ctx := context.Background()
	if err := worker.Rehydrate(ctx); err != nil {
		t.Fatalf("unexpected error rehydrating: %v", err)
	}

	if energy := model.Energy(); energy != 7.5 {
		t.Errorf("expected battery energy to be 7.5 kWh, got %f", energy)
	}
	if savings := worker.CumulativeSavings(); savings != 34.20 {
		t.Errorf("expected cumulative savings to be 34.20 PLN, got %f", savings)
	}
}

func TestWorker_Rehydrate_NotFoundDefaults(t *testing.T) {
	store := newTestStore()
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	// NewBatteryModel initializes energy to 50% capacity (5.0 kWh)
	worker := NewSingleWorker(store, model, time.Minute, nil)

	ctx := context.Background()
	if err := worker.Rehydrate(ctx); err != nil {
		t.Fatalf("expected nil error on not found, got %v", err)
	}

	if energy := model.Energy(); energy != 5.0 {
		t.Errorf("expected battery energy to remain 5.0 kWh, got %f", energy)
	}
	if savings := worker.CumulativeSavings(); savings != 0.0 {
		t.Errorf("expected cumulative savings to be 0.0 PLN, got %f", savings)
	}
}

func TestWorker_Rehydrate_DatabaseError(t *testing.T) {
	store := newTestStore()
	store.getError = errors.New("db connection refused")

	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	worker := NewSingleWorker(store, model, time.Minute, nil)

	ctx := context.Background()
	if err := worker.Rehydrate(ctx); err == nil {
		t.Fatal("expected error on db failure, got nil")
	}
}

func TestWorker_Step_CalculatesAndInsertsMetrics(t *testing.T) {
	store := newTestStore()
	store.setLatest("photovoltaics", "current", 3000.0)
	store.setLatest("main_meter", "current_usage", 1000.0)
	store.setLatest("energy_market", "rce_kwh", 0.50)

	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	model.SetEnergy(5.0)

	worker := NewSingleWorker(store, model, time.Minute, nil)

	ctx := context.Background()
	deltaHours := 1.0 / 60.0 // 1 minute

	if err := worker.Step(ctx, deltaHours); err != nil {
		t.Fatalf("unexpected error on Step(): %v", err)
	}

	inserts := store.getInserts()
	if len(inserts) != 5 {
		t.Fatalf("expected 5 metrics inserted, got %d", len(inserts))
	}

	insertedMap := make(map[string]float64)
	for _, ins := range inserts {
		if ins.category != "simulation" {
			t.Errorf("expected category 'simulation', got %q", ins.category)
		}
		insertedMap[ins.metricName] = ins.value
	}

	expectedMetrics := []string{
		"sim_battery_soc_kwh",
		"sim_battery_soc_pct",
		"sim_battery_power_w",
		"sim_savings_interval_pln",
		"sim_savings_pln",
	}

	for _, name := range expectedMetrics {
		if _, ok := insertedMap[name]; !ok {
			t.Errorf("missing metric %q in inserts", name)
		}
	}

	// 2000W surplus for 1 minute:
	// power: 2000W
	if p := insertedMap["sim_battery_power_w"]; p != 2000.0 {
		t.Errorf("expected sim_battery_power_w=2000, got %f", p)
	}
	// energy should increase above 5.0
	if soc := insertedMap["sim_battery_soc_kwh"]; soc <= 5.0 {
		t.Errorf("expected sim_battery_soc_kwh > 5.0, got %f", soc)
	}
	// SoC pct should increase above 50%
	if pct := insertedMap["sim_battery_soc_pct"]; pct <= 50.0 {
		t.Errorf("expected sim_battery_soc_pct > 50, got %f", pct)
	}

	firstIntervalSavings := insertedMap["sim_savings_interval_pln"]
	if insertedMap["sim_savings_pln"] != firstIntervalSavings {
		t.Errorf("expected sim_savings_pln (%f) == sim_savings_interval_pln (%f)",
			insertedMap["sim_savings_pln"], firstIntervalSavings)
	}

	// Step a second time to verify cumulative savings accumulation
	if err := worker.Step(ctx, deltaHours); err != nil {
		t.Fatalf("unexpected error on second Step(): %v", err)
	}

	inserts2 := store.getInserts()
	if len(inserts2) != 10 {
		t.Fatalf("expected 10 total metrics inserted after 2 steps, got %d", len(inserts2))
	}

	secondIntervalSavings := inserts2[8].value // index 8 is sim_savings_interval_pln of step 2
	secondCumSavings := inserts2[9].value      // index 9 is sim_savings_pln of step 2

	expectedCum := firstIntervalSavings + secondIntervalSavings
	if diff := secondCumSavings - expectedCum; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("expected cumulative savings %f, got %f", expectedCum, secondCumSavings)
	}
}

func TestWorker_Step_MissingTelemetry(t *testing.T) {
	testCases := []struct {
		name    string
		init    func(s *testStore)
		missing string
	}{
		{
			name: "missing PV",
			init: func(s *testStore) {
				s.setLatest("main_meter", "current_usage", 1000.0)
				s.setLatest("energy_market", "rce_kwh", 0.50)
			},
			missing: "photovoltaics/current",
		},
		{
			name: "missing Load",
			init: func(s *testStore) {
				s.setLatest("photovoltaics", "current", 3000.0)
				s.setLatest("energy_market", "rce_kwh", 0.50)
			},
			missing: "main_meter/current_usage",
		},
		{
			name: "missing RCE",
			init: func(s *testStore) {
				s.setLatest("photovoltaics", "current", 3000.0)
				s.setLatest("main_meter", "current_usage", 1000.0)
			},
			missing: "energy_market/rce_kwh",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore()
			tc.init(store)

			model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
			initialEnergy := model.Energy()
			worker := NewSingleWorker(store, model, time.Minute, nil)

			err := worker.Step(context.Background(), 1.0/60.0)
			if err == nil {
				t.Fatalf("expected error when %s is missing, got nil", tc.missing)
			}

			// Verify energy is unchanged
			if model.Energy() != initialEnergy {
				t.Errorf("expected energy to remain %f, got %f", initialEnergy, model.Energy())
			}

			// Verify no metrics were inserted
			if len(store.getInserts()) != 0 {
				t.Errorf("expected 0 inserts on missing telemetry, got %d", len(store.getInserts()))
			}
		})
	}
}

func TestWorker_Step_InsertError(t *testing.T) {
	store := newTestStore()
	store.setLatest("photovoltaics", "current", 3000.0)
	store.setLatest("main_meter", "current_usage", 1000.0)
	store.setLatest("energy_market", "rce_kwh", 0.50)
	store.insertError = errors.New("db insert failed")

	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	worker := NewSingleWorker(store, model, time.Minute, nil)

	err := worker.Step(context.Background(), 1.0/60.0)
	if err == nil {
		t.Fatal("expected error on insert failure, got nil")
	}
}

func TestWorker_Start_GracefulShutdown(t *testing.T) {
	store := newTestStore()
	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	worker := NewSingleWorker(store, model, 10*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		worker.Start(ctx)
		close(done)
	}()

	// Allow worker to start
	time.Sleep(25 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Succeeded
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker did not exit cleanly within timeout after context cancellation")
	}
}

func TestWorker_Start_TickerExecutesStep(t *testing.T) {
	store := newTestStore()
	store.setLatest("photovoltaics", "current", 4000.0)
	store.setLatest("main_meter", "current_usage", 1500.0)
	store.setLatest("energy_market", "rce_kwh", 0.60)

	model := NewBatteryModel(10.0, 5.0, 0.95, 0.40)
	worker := NewSingleWorker(store, model, 15*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		worker.Start(ctx)
		close(done)
	}()

	// Wait until at least 1 step has executed and inserted metrics
	deadline := time.Now().Add(500 * time.Millisecond)
	var count int
	for time.Now().Before(deadline) {
		count = len(store.getInserts())
		if count >= 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	<-done

	if count < 5 {
		t.Fatalf("expected at least 5 metrics inserted by ticker, got %d", count)
	}
}

func TestWorker_DefaultVariants_StepAndRehydrate(t *testing.T) {
	store := newTestStore()
	store.setLatest("photovoltaics", "current", 6000.0)
	store.setLatest("main_meter", "current_usage", 1000.0)
	store.setLatest("energy_market", "rce_kwh", 0.50)

	variants := DefaultVariants(0.95, 0.40)
	if len(variants) != 4 {
		t.Fatalf("expected 4 default variants, got %d", len(variants))
	}

	worker := NewWorker(store, variants, time.Minute, nil)

	ctx := context.Background()
	deltaHours := 1.0 / 60.0

	if err := worker.Step(ctx, deltaHours); err != nil {
		t.Fatalf("unexpected error on Step(): %v", err)
	}

	inserts := store.getInserts()
	// 3 non-default variants * 5 + 1 default variant * (5 + 5) = 25 inserts
	if len(inserts) != 25 {
		t.Fatalf("expected 25 metrics inserted across 4 variants (with default alias), got %d", len(inserts))
	}

	categories := make(map[string]int)
	for _, ins := range inserts {
		categories[ins.category]++
	}

	expectedCategories := []string{
		"simulation_5kwh_5kw",
		"simulation_5kwh_10kw",
		"simulation_10kwh_5kw",
		"simulation_10kwh_10kw",
		"simulation",
	}

	for _, cat := range expectedCategories {
		if count := categories[cat]; count != 5 {
			t.Errorf("expected 5 inserts for category %q, got %d", cat, count)
		}
	}

	// Verify CumulativeSavings query
	defaultSavings := worker.CumulativeSavings()
	if defaultSavings == 0 {
		t.Errorf("expected non-zero default savings, got 0")
	}
	var5kwSavings := worker.CumulativeSavings("5kwh_5kw")
	if var5kwSavings == 0 {
		t.Errorf("expected non-zero 5kwh_5kw savings, got 0")
	}

	// Create new worker to verify Rehydrate loads from store
	newVariants := DefaultVariants(0.95, 0.40)
	newWorker := NewWorker(store, newVariants, time.Minute, nil)
	if err := newWorker.Rehydrate(ctx); err != nil {
		t.Fatalf("unexpected error rehydrating multi-variants: %v", err)
	}

	if newWorker.CumulativeSavings() != defaultSavings {
		t.Errorf("expected rehydrated default savings %f, got %f", defaultSavings, newWorker.CumulativeSavings())
	}
	if newWorker.CumulativeSavings("5kwh_5kw") != var5kwSavings {
		t.Errorf("expected rehydrated 5kwh_5kw savings %f, got %f", var5kwSavings, newWorker.CumulativeSavings("5kwh_5kw"))
	}
}
