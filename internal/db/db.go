package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS metrics (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    category VARCHAR(128) NOT NULL,
    metric_name VARCHAR(128) NOT NULL,
    value DOUBLE PRECISION NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metrics_cat_metric_time 
ON metrics (category, metric_name, timestamp DESC);
`

type PgxStore struct {
	pool *pgxpool.Pool
}

func NewPgxStore(ctx context.Context, connString string) (*PgxStore, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	cfg.MaxConns = 25
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 1 * time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	store := &PgxStore{pool: pool}

	// Verify connection
	if err := store.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	// Run auto-migration
	if err := store.Migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	return store, nil
}

func (s *PgxStore) Migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schemaDDL)
	return err
}

func (s *PgxStore) InsertMetric(ctx context.Context, category, metricName string, value float64) error {
	query := `INSERT INTO metrics (timestamp, category, metric_name, value) VALUES (NOW(), $1, $2, $3)`
	_, err := s.pool.Exec(ctx, query, category, metricName, value)
	return err
}

func (s *PgxStore) InsertMetricWithTimestamp(ctx context.Context, ts time.Time, category, metricName string, value float64) error {
	query := `INSERT INTO metrics (timestamp, category, metric_name, value)
SELECT $1::timestamptz, $2::text, $3::text, $4::double precision
WHERE NOT EXISTS (
    SELECT 1 FROM metrics WHERE category = $2::text AND metric_name = $3::text AND timestamp = $1::timestamptz
)`
	_, err := s.pool.Exec(ctx, query, ts, category, metricName, value)
	return err
}

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

func (s *PgxStore) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.pool.Ping(pingCtx)
}

func (s *PgxStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
