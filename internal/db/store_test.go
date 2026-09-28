package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Ensure both MockStore and PgxStore implement the MetricStore interface.
var _ MetricStore = (*MockStore)(nil)
var _ MetricStore = (*PgxStore)(nil)

func TestMockStore(t *testing.T) {
	ctx := context.Background()
	var insertedCat, insertedMetric string
	var insertedVal float64
	closed := false

	mock := &MockStore{
		InsertFunc: func(ctx context.Context, category, metricName string, value float64) error {
			insertedCat = category
			insertedMetric = metricName
			insertedVal = value
			return nil
		},
		PingFunc: func(ctx context.Context) error {
			return errors.New("ping error")
		},
		CloseFunc: func() {
			closed = true
		},
	}

	err := mock.InsertMetric(ctx, "power", "watts", 1500.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if insertedCat != "power" || insertedMetric != "watts" || insertedVal != 1500.5 {
		t.Errorf("mock did not capture correct values: %s %s %f", insertedCat, insertedMetric, insertedVal)
	}

	testTime := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	var capturedTS time.Time
	mock.InsertWithTimestampFunc = func(ctx context.Context, ts time.Time, category, metricName string, value float64) error {
		capturedTS = ts
		insertedCat = category
		insertedMetric = metricName
		insertedVal = value
		return nil
	}
	if err := mock.InsertMetricWithTimestamp(ctx, testTime, "energy_market", "rce_kwh", 0.45); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturedTS.Equal(testTime) || insertedCat != "energy_market" || insertedMetric != "rce_kwh" || insertedVal != 0.45 {
		t.Errorf("mock did not capture correct timestamped values: %v %s %s %f", capturedTS, insertedCat, insertedMetric, insertedVal)
	}

	if err := mock.Ping(ctx); err == nil {
		t.Errorf("expected ping error, got nil")
	}

	mock.Close()
	if !closed {
		t.Errorf("expected Close to be called")
	}
}

func TestMockStore_Defaults(t *testing.T) {
	ctx := context.Background()
	mock := &MockStore{}

	if err := mock.InsertMetric(ctx, "temperature", "celsius", 21.5); err != nil {
		t.Errorf("expected nil error on default InsertMetric, got %v", err)
	}

	if err := mock.Ping(ctx); err != nil {
		t.Errorf("expected nil error on default Ping, got %v", err)
	}

	// Ensure Close does not panic when CloseFunc is nil
	mock.Close()
}

func TestNewPgxStore_InvalidConfig(t *testing.T) {
	ctx := context.Background()
	_, err := NewPgxStore(ctx, "://bad-connection-string")
	if err == nil {
		t.Fatal("expected error with invalid connection string, got nil")
	}
}

func TestPgxStore_CloseNilPool(t *testing.T) {
	store := &PgxStore{}
	store.Close() // Should not panic
}

