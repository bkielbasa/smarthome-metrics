package db

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrMetricNotFound = errors.New("metric not found")

// MetricStore defines persistence operations for metrics.
type MetricStore interface {
	InsertMetric(ctx context.Context, category, metricName string, value float64) error
	InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error
	GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error)
	Ping(ctx context.Context) error
	Close()
}

type mockRecord struct {
	timestamp  time.Time
	category   string
	metricName string
	value      float64
}

// MockStore is an in-memory mock useful for testing HTTP handlers.
type MockStore struct {
	InsertFunc              func(ctx context.Context, category, metricName string, value float64) error
	InsertWithTimestampFunc func(ctx context.Context, ts time.Time, category, metricName string, value float64) error
	GetLatestMetricFunc     func(ctx context.Context, category, metricName string) (float64, time.Time, error)
	PingFunc                func(ctx context.Context) error
	CloseFunc               func()

	mu      sync.Mutex
	records []mockRecord
}

func (m *MockStore) InsertMetric(ctx context.Context, category, metricName string, value float64) error {
	m.mu.Lock()
	m.records = append(m.records, mockRecord{
		timestamp:  time.Now().UTC(),
		category:   category,
		metricName: metricName,
		value:      value,
	})
	m.mu.Unlock()
	if m.InsertFunc != nil {
		return m.InsertFunc(ctx, category, metricName, value)
	}
	return nil
}

func (m *MockStore) InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error {
	m.mu.Lock()
	m.records = append(m.records, mockRecord{
		timestamp:  ts,
		category:   category,
		metricName: metricName,
		value:      value,
	})
	m.mu.Unlock()
	if m.InsertWithTimestampFunc != nil {
		return m.InsertWithTimestampFunc(ctx, ts, category, metricName, value)
	}
	return nil
}

func (m *MockStore) GetLatestMetric(ctx context.Context, category, metricName string) (float64, time.Time, error) {
	if m.GetLatestMetricFunc != nil {
		return m.GetLatestMetricFunc(ctx, category, metricName)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var (
		found     bool
		latestVal float64
		latestTS  time.Time
	)
	for _, r := range m.records {
		if r.category == category && r.metricName == metricName {
			if !found || r.timestamp.After(latestTS) {
				found = true
				latestVal = r.value
				latestTS = r.timestamp
			}
		}
	}
	if !found {
		return 0, time.Time{}, ErrMetricNotFound
	}
	return latestVal, latestTS, nil
}

func (m *MockStore) Ping(ctx context.Context) error {
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return nil
}

func (m *MockStore) Close() {
	if m.CloseFunc != nil {
		m.CloseFunc()
	}
}

