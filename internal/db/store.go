package db

import (
	"context"
	"time"
)

// MetricStore defines persistence operations for metrics.
type MetricStore interface {
	InsertMetric(ctx context.Context, category, metricName string, value float64) error
	InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error
	Ping(ctx context.Context) error
	Close()
}

// MockStore is an in-memory mock useful for testing HTTP handlers.
type MockStore struct {
	InsertFunc              func(ctx context.Context, category, metricName string, value float64) error
	InsertWithTimestampFunc func(ctx context.Context, ts time.Time, category, metricName string, value float64) error
	PingFunc                func(ctx context.Context) error
	CloseFunc               func()
}

func (m *MockStore) InsertMetric(ctx context.Context, category, metricName string, value float64) error {
	if m.InsertFunc != nil {
		return m.InsertFunc(ctx, category, metricName, value)
	}
	return nil
}

func (m *MockStore) InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error {
	if m.InsertWithTimestampFunc != nil {
		return m.InsertWithTimestampFunc(ctx, ts, category, metricName, value)
	}
	return nil
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
