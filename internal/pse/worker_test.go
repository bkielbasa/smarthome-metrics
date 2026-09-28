package pse_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/pse"
)

type mockFetcher struct {
	records []pse.PriceRecord
	err     error
}

func (m *mockFetcher) FetchPrices(ctx context.Context, fromDate string) ([]pse.PriceRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.records, nil
}

type capturedInsert struct {
	ts         time.Time
	category   string
	metricName string
	value      float64
}

type mockStoreWithTimestamp struct {
	mu      sync.Mutex
	inserts []capturedInsert
}

func (m *mockStoreWithTimestamp) InsertMetric(ctx context.Context, category, metricName string, value float64) error {
	return nil
}

func (m *mockStoreWithTimestamp) InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inserts = append(m.inserts, capturedInsert{
		ts:         ts,
		category:   category,
		metricName: metricName,
		value:      value,
	})
	return nil
}

func (m *mockStoreWithTimestamp) Ping(ctx context.Context) error {
	return nil
}

func (m *mockStoreWithTimestamp) GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error) {
	return 0, time.Time{}, nil
}

func (m *mockStoreWithTimestamp) Close() {}

func TestWorker_Sync_Success(t *testing.T) {
	testTS := time.Date(2024, 9, 1, 12, 0, 0, 0, time.UTC)
	fetcher := &mockFetcher{
		records: []pse.PriceRecord{
			{
				Timestamp: testTS,
				RCEMWh:    450.0,
				RCEKWh:    0.45,
			},
		},
	}
	store := &mockStoreWithTimestamp{}
	worker := pse.NewWorker(fetcher, store, 15*time.Minute, nil)

	if err := worker.Sync(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.inserts) != 2 {
		t.Fatalf("expected 2 inserts (mwh and kwh), got %d", len(store.inserts))
	}

	// Verify rce_mwh
	ins0 := store.inserts[0]
	if ins0.category != "energy_market" || ins0.metricName != "rce_mwh" || ins0.value != 450.0 || !ins0.ts.Equal(testTS) {
		t.Errorf("unexpected mwh insert: %+v", ins0)
	}

	// Verify rce_kwh
	ins1 := store.inserts[1]
	if ins1.category != "energy_market" || ins1.metricName != "rce_kwh" || ins1.value != 0.45 || !ins1.ts.Equal(testTS) {
		t.Errorf("unexpected kwh insert: %+v", ins1)
	}
}

func TestWorker_Sync_FetchError(t *testing.T) {
	fetcher := &mockFetcher{
		err: errors.New("network down"),
	}
	store := &mockStoreWithTimestamp{}
	worker := pse.NewWorker(fetcher, store, 15*time.Minute, nil)

	err := worker.Sync(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWorker_Start_GracefulShutdown(t *testing.T) {
	fetcher := &mockFetcher{}
	store := &mockStoreWithTimestamp{}
	worker := pse.NewWorker(fetcher, store, 5*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		worker.Start(ctx)
		close(done)
	}()

	// Wait briefly, then cancel
	time.Sleep(15 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Succeeded
	case <-time.After(500 * time.Millisecond):
		t.Fatal("worker did not exit cleanly within timeout")
	}
}
