# SmartHome Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a lightweight Go microservice that ingests sensor metrics via `POST /metric/{category}/{metric_name}`, stores them in PostgreSQL, packages the service into a multi-arch Docker image pushed to Docker Hub via GitHub Actions on release tags, and provides a Helm chart for Kubernetes deployment.

**Architecture:** A standard Go service using Go 1.22+ `net/http` router pattern matching and `jackc/pgx/v5` connection pooling. The service executes idempotent schema migrations on startup, exposes health endpoints for Kubernetes probes, runs containerized as an unprivileged user, and deploys via a parameterized Helm chart.

**Tech Stack:** Go 1.24+, PostgreSQL (`jackc/pgx/v5`), Docker (multi-stage Alpine), GitHub Actions (Buildx, QEMU, `action-gh-release`), Helm v3/v4.

**Spec:** [docs/superpowers/specs/2026-09-27-smarthome-metrics-design.md](file:///Users/bartlomiej.klimczak/Projects/smarthome-metrics/docs/superpowers/specs/2026-09-27-smarthome-metrics-design.md)

## Global Constraints
- Target Docker Hub repository: `bartlomiejklimczak/smarthome-metrics`
- Supported endpoint: `POST /metric/{category}/{metric_name}` with body as raw float string
- Health check endpoint: `GET /healthz` (checks database connection)
- Database: PostgreSQL with table `metrics` (id, timestamp, category, metric_name, value)
- External PostgreSQL support in Helm chart via direct values or `existingSecret`
- Zero placeholders in implementation code

---

### Task 1: Project Scaffolding & Configuration Module

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.dockerignore`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: Standard library `os`, `strconv`, `time`, `net/url`, `fmt`
- Produces: `config.Config` struct with fields `Port`, `DatabaseURL`, `DBHost`, `DBPort`, `DBUser`, `DBPassword`, `DBName`, `DBSSLMode`, `ShutdownTimeout`, and method `(c *Config) GetDatabaseURL() string`

- [ ] **Step 1: Initialize go module and ignore files**

Create `.gitignore`:
```
bin/
dist/
*.exe
*.test
*.out
.DS_Store
.env
```

Create `.dockerignore`:
```
.git
.github
charts
docs
bin
dist
*.md
```

Initialize `go.mod`:
```bash
go mod init github.com/bartlomiejklimczak/smarthome-metrics
```

- [ ] **Step 2: Write the failing configuration test**

Create `internal/config/config_test.go`:
```go
package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear relevant env vars
	envKeys := []string{"PORT", "DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE", "SHUTDOWN_TIMEOUT"}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	cfg := Load()

	if cfg.Port != "8088" {
		t.Errorf("expected Port 8088, got %s", cfg.Port)
	}
	if cfg.DBHost != "localhost" {
		t.Errorf("expected DBHost localhost, got %s", cfg.DBHost)
	}
	if cfg.DBPort != "5432" {
		t.Errorf("expected DBPort 5432, got %s", cfg.DBPort)
	}
	if cfg.DBUser != "postgres" {
		t.Errorf("expected DBUser postgres, got %s", cfg.DBUser)
	}
	if cfg.DBName != "metrics" {
		t.Errorf("expected DBName metrics, got %s", cfg.DBName)
	}
	if cfg.DBSSLMode != "disable" {
		t.Errorf("expected DBSSLMode disable, got %s", cfg.DBSSLMode)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("expected ShutdownTimeout 5s, got %v", cfg.ShutdownTimeout)
	}

	expectedURL := "postgres://postgres:@localhost:5432/metrics?sslmode=disable"
	if cfg.GetDatabaseURL() != expectedURL {
		t.Errorf("expected GetDatabaseURL %s, got %s", expectedURL, cfg.GetDatabaseURL())
	}
}

func TestLoadConfig_CustomEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DATABASE_URL", "postgres://custom:pass@remote:5433/customdb?sslmode=require")
	os.Setenv("SHUTDOWN_TIMEOUT", "10s")
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("SHUTDOWN_TIMEOUT")
	}()

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("expected Port 9090, got %s", cfg.Port)
	}
	if cfg.GetDatabaseURL() != "postgres://custom:pass@remote:5433/customdb?sslmode=require" {
		t.Errorf("expected custom DATABASE_URL, got %s", cfg.GetDatabaseURL())
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config`
Expected: FAIL with undefined `Load`

- [ ] **Step 4: Implement configuration loader**

Create `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"net/url"
	"os"
	"time"
)

type Config struct {
	Port            string
	DatabaseURL     string
	DBHost          string
	DBPort          string
	DBUser          string
	DBPassword      string
	DBName          string
	DBSSLMode       string
	ShutdownTimeout time.Duration
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultValue
}

func Load() *Config {
	shutdownTimeoutStr := getEnv("SHUTDOWN_TIMEOUT", "5s")
	shutdownTimeout, err := time.ParseDuration(shutdownTimeoutStr)
	if err != nil {
		shutdownTimeout = 5 * time.Second
	}

	return &Config{
		Port:            getEnv("PORT", "8088"),
		DatabaseURL:     getEnv("DATABASE_URL", ""),
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBPort:          getEnv("DB_PORT", "5432"),
		DBUser:          getEnv("DB_USER", "postgres"),
		DBPassword:      getEnv("DB_PASSWORD", ""),
		DBName:          getEnv("DB_NAME", "metrics"),
		DBSSLMode:       getEnv("DB_SSLMODE", "disable"),
		ShutdownTimeout: shutdownTimeout,
	}
}

func (c *Config) GetDatabaseURL() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}

	var userPass *url.Userinfo
	if c.DBPassword != "" {
		userPass = url.UserPassword(c.DBUser, c.DBPassword)
	} else {
		userPass = url.User(c.DBUser)
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     userPass,
		Host:     fmt.Sprintf("%s:%s", c.DBHost, c.DBPort),
		Path:     c.DBName,
		RawQuery: fmt.Sprintf("sslmode=%s", url.QueryEscape(c.DBSSLMode)),
	}

	return u.String()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/config`
Expected: PASS

- [ ] **Step 6: Commit Task 1**

```bash
git add go.mod .gitignore .dockerignore internal/config/
git commit -m "feat: add project scaffolding and configuration module"
```

---

### Task 2: Database Layer & Metric Store

**Files:**
- Create: `internal/db/store.go`
- Create: `internal/db/db.go`
- Modify: `go.mod` (add `github.com/jackc/pgx/v5`)

**Interfaces:**
- Consumes: `github.com/jackc/pgx/v5/pgxpool`, `context.Context`
- Produces:
  ```go
  type MetricStore interface {
      InsertMetric(ctx context.Context, category, metricName string, value float64) error
      Ping(ctx context.Context) error
      Close()
  }
  ```
  and `NewPgxStore(ctx context.Context, connString string) (*PgxStore, error)`

- [ ] **Step 1: Install pgx dependency**

Run: `go get github.com/jackc/pgx/v5`
Run: `go mod tidy`

- [ ] **Step 2: Define MetricStore interface and mock in `internal/db/store.go`**

Create `internal/db/store.go`:
```go
package db

import (
	"context"
)

// MetricStore defines persistence operations for metrics.
type MetricStore interface {
	InsertMetric(ctx context.Context, category, metricName string, value float64) error
	Ping(ctx context.Context) error
	Close()
}

// MockStore is an in-memory mock useful for testing HTTP handlers.
type MockStore struct {
	InsertFunc func(ctx context.Context, category, metricName string, value float64) error
	PingFunc   func(ctx context.Context) error
	CloseFunc  func()
}

func (m *MockStore) InsertMetric(ctx context.Context, category, metricName string, value float64) error {
	if m.InsertFunc != nil {
		return m.InsertFunc(ctx, category, metricName, value)
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
```

- [ ] **Step 3: Implement PgxStore and migration logic in `internal/db/db.go`**

Create `internal/db/db.go`:
```go
package db

import (
	"context"
	"fmt"
	"time"

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
```

- [ ] **Step 4: Verify package compiles and runs mock tests**

Create `internal/db/store_test.go`:
```go
package db

import (
	"context"
	"errors"
	"testing"
)

func TestMockStore(t *testing.T) {
	ctx := context.Background()
	var insertedCat, insertedMetric string
	var insertedVal float64

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
	}

	err := mock.InsertMetric(ctx, "power", "watts", 1500.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if insertedCat != "power" || insertedMetric != "watts" || insertedVal != 1500.5 {
		t.Errorf("mock did not capture correct values: %s %s %f", insertedCat, insertedMetric, insertedVal)
	}

	if err := mock.Ping(ctx); err == nil {
		t.Errorf("expected ping error, got nil")
	}
}
```

Run: `go test -v ./internal/db`
Expected: PASS

- [ ] **Step 5: Commit Task 2**

```bash
git add go.mod go.sum internal/db/
git commit -m "feat: implement database store interface, pgx driver, and schema migration"
```

---

### Task 3: HTTP Server & Metric Ingestion API Handlers

**Files:**
- Create: `internal/server/handlers.go`
- Create: `internal/server/routes.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `internal/db.MetricStore`, standard library `net/http`
- Produces: `server.NewServer(store db.MetricStore) *http.ServeMux`

- [ ] **Step 1: Write comprehensive failing HTTP handler tests**

Create `internal/server/server_test.go`:
```go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

func TestMetricHandler_Success(t *testing.T) {
	var savedCat, savedMetric string
	var savedVal float64

	store := &db.MockStore{
		InsertFunc: func(ctx context.Context, category, metricName string, value float64) error {
			savedCat = category
			savedMetric = metricName
			savedVal = value
			return nil
		},
	}

	handler := NewRouter(store)

	body := bytes.NewBufferString(" 230.45 \n")
	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}

	if savedCat != "main_meter" || savedMetric != "delivery_current" || savedVal != 230.45 {
		t.Errorf("expected ('main_meter', 'delivery_current', 230.45), got (%s, %s, %f)",
			savedCat, savedMetric, savedVal)
	}
}

func TestMetricHandler_InvalidFloat(t *testing.T) {
	store := &db.MockStore{}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", bytes.NewBufferString("not_a_float"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestMetricHandler_EmptyBody(t *testing.T) {
	store := &db.MockStore{}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", bytes.NewBufferString("   "))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestMetricHandler_InvalidIdentifier(t *testing.T) {
	store := &db.MockStore{}
	handler := NewRouter(store)

	// Slash or invalid chars in name
	req := httptest.NewRequest(http.MethodPost, "/metric/main@meter/delivery!current", bytes.NewBufferString("100"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid slug, got %d", rec.Code)
	}
}

func TestMetricHandler_DatabaseError(t *testing.T) {
	store := &db.MockStore{
		InsertFunc: func(ctx context.Context, category, metricName string, value float64) error {
			return errors.New("db disk full")
		},
	}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", bytes.NewBufferString("100.5"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestMetricHandler_PayloadTooLarge(t *testing.T) {
	store := &db.MockStore{}
	handler := NewRouter(store)

	hugeBody := strings.Repeat("9", 70000)
	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", bytes.NewBufferString(hugeBody))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 400 or 413 for oversized body, got %d", rec.Code)
	}
}

func TestHealthz_Success(t *testing.T) {
	store := &db.MockStore{
		PingFunc: func(ctx context.Context) error {
			return nil
		},
	}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHealthz_DatabaseDown(t *testing.T) {
	store := &db.MockStore{
		PingFunc: func(ctx context.Context) error {
			return errors.New("connection refused")
		},
	}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server`
Expected: FAIL with undefined `NewRouter`

- [ ] **Step 3: Implement handlers and router**

Create `internal/server/handlers.go`:
```go
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

var validSlugRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]{1,128}$`)

type Handler struct {
	store db.MetricStore
}

func NewHandler(store db.MetricStore) *Handler {
	return &Handler{store: store}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) HandlePostMetric(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	metricName := r.PathValue("metric_name")

	if !validSlugRegex.MatchString(category) || !validSlugRegex.MatchString(metricName) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid category or metric_name; must match ^[a-zA-Z0-9_\\-\\.]{1,128}$",
		})
		return
	}

	// Limit body reading to 64KB
	bodyReader := http.MaxBytesReader(w, r.Body, 64*1024)
	bodyBytes, err := io.ReadAll(bodyReader)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "payload too large or unreadable",
		})
		return
	}

	trimmedBody := strings.TrimSpace(string(bodyBytes))
	if trimmedBody == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "body cannot be empty; expected float value",
		})
		return
	}

	val, err := strconv.ParseFloat(trimmedBody, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid float value in body",
		})
		return
	}

	if err := h.store.InsertMetric(r.Context(), category, metricName, val); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to persist metric",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "healthy",
		"database": "connected",
	})
}
```

Create `internal/server/routes.go`:
```go
package server

import (
	"net/http"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

func NewRouter(store db.MetricStore) http.Handler {
	mux := http.NewServeMux()
	h := NewHandler(store)

	mux.HandleFunc("POST /metric/{category}/{metric_name}", h.HandlePostMetric)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)

	return mux
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/server`
Expected: PASS

- [ ] **Step 5: Commit Task 3**

```bash
git add internal/server/
git commit -m "feat: implement HTTP router and handlers for metric ingestion and health probes"
```

---

### Task 4: Main Application Entrypoint & Graceful Shutdown

**Files:**
- Create: `cmd/server/main.go`

**Interfaces:**
- Consumes: `internal/config`, `internal/db`, `internal/server`
- Produces: Executable binary `bin/smarthome-metrics`

- [ ] **Step 1: Write main.go with graceful termination**

Create `cmd/server/main.go`:
```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/config"
	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
	"github.com/bartlomiejklimczak/smarthome-metrics/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	logger.Info("starting smarthome-metrics server", "port", cfg.Port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := cfg.GetDatabaseURL()
	store, err := db.NewPgxStore(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	logger.Info("connected to PostgreSQL and initialized schema")

	router := server.NewRouter(store)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Port),
		Handler: router,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening for HTTP requests", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		logger.Error("server encountered error", "error", err)
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server shut down successfully")
}
```

- [ ] **Step 2: Build the binary locally to verify compilation**

Run: `go build -o bin/smarthome-metrics ./cmd/server`
Expected: Success with no errors.

- [ ] **Step 3: Commit Task 4**

```bash
git add cmd/server/main.go
git commit -m "feat: add application entrypoint with signal handling and graceful shutdown"
```

---

### Task 5: Multi-Stage Dockerfile & Local Container Build

**Files:**
- Create: `Dockerfile`

**Interfaces:**
- Produces: Minimal container running non-root on port 8088

- [ ] **Step 1: Write Dockerfile**

Create `Dockerfile`:
```dockerfile
# Stage 1: Build binary
FROM golang:1.24-alpine AS builder

WORKDIR /build

RUN apk add --no-cache ca-certificates tzdata git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /app/smarthome-metrics \
    ./cmd/server

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 appgroup \
    && adduser -u 10001 -G appgroup -s /sbin/nologin -D appuser

WORKDIR /app

COPY --from=builder /app/smarthome-metrics /app/smarthome-metrics

USER 10001:10001

EXPOSE 8088

ENTRYPOINT ["/app/smarthome-metrics"]
```

- [ ] **Step 2: Build Docker image locally**

Run: `docker build -t bartlomiejklimczak/smarthome-metrics:local .`
Expected: Image builds successfully.

- [ ] **Step 3: Commit Task 5**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile for unprivileged container execution"
```

---

### Task 6: GitHub Actions Workflows (CI & Tag-Driven Release)

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: GitHub Secrets `DOCKERHUB_USERNAME`, `DOCKERHUB_TOKEN`
- Produces: Multi-arch Docker images at `bartlomiejklimczak/smarthome-metrics` and GitHub Releases on `v*` tags

- [ ] **Step 1: Write CI workflow**

Create `.github/workflows/ci.yml`:
```yaml
name: CI

on:
  push:
    branches: [ "main" ]
  pull_request:
    branches: [ "main" ]

jobs:
  test:
    name: Run Tests
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true

      - name: Run unit tests
        run: go test -v -race ./...

      - name: Verify build
        run: go build -v ./cmd/server

  docker-lint:
    name: Verify Dockerfile Build
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build local container
        uses: docker/build-push-action@v6
        with:
          context: .
          push: false
          tags: bartlomiejklimczak/smarthome-metrics:test
```

- [ ] **Step 2: Write Release workflow**

Create `.github/workflows/release.yml`:
```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  release:
    name: Build, Push Image & Create GitHub Release
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true

      - name: Run unit tests
        run: go test -v -race ./...

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Extract Docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: bartlomiejklimczak/smarthome-metrics
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=raw,value=latest

      - name: Build and push multi-arch Docker image
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
          tag_name: ${{ github.ref_name }}
          name: Release ${{ github.ref_name }}
          draft: false
          prerelease: false
```

- [ ] **Step 3: Commit Task 6**

```bash
git add .github/workflows/
git commit -m "ci: add GitHub Actions workflows for continuous integration and automated releases"
```

---

### Task 7: Helm Chart for Kubernetes Deployment

**Files:**
- Create: `charts/smarthome-metrics/Chart.yaml`
- Create: `charts/smarthome-metrics/values.yaml`
- Create: `charts/smarthome-metrics/.helmignore`
- Create: `charts/smarthome-metrics/templates/_helpers.tpl`
- Create: `charts/smarthome-metrics/templates/deployment.yaml`
- Create: `charts/smarthome-metrics/templates/service.yaml`
- Create: `charts/smarthome-metrics/templates/configmap.yaml`
- Create: `charts/smarthome-metrics/templates/secret.yaml`
- Create: `charts/smarthome-metrics/templates/serviceaccount.yaml`
- Create: `charts/smarthome-metrics/templates/ingress.yaml`
- Create: `charts/smarthome-metrics/templates/NOTES.txt`

**Interfaces:**
- Produces: Helm Chart deployable via `helm install smarthome-metrics ./charts/smarthome-metrics`

- [ ] **Step 1: Create Chart metadata and .helmignore**

Create `charts/smarthome-metrics/.helmignore`:
```
.DS_Store
*.tgz
.git/
.gitignore
```

Create `charts/smarthome-metrics/Chart.yaml`:
```yaml
apiVersion: v2
name: smarthome-metrics
description: A lightweight Go service to ingest and persist smart home metrics into PostgreSQL
type: application
version: 0.1.0
appVersion: "1.0.0"
maintainers:
  - name: bartlomiejklimczak
```

- [ ] **Step 2: Create `values.yaml`**

Create `charts/smarthome-metrics/values.yaml`:
```yaml
replicaCount: 1

image:
  repository: bartlomiejklimczak/smarthome-metrics
  pullPolicy: IfNotPresent
  tag: "" # Defaults to .Chart.AppVersion

imagePullSecrets: []
nameOverride: ""
fullnameOverride: ""

serviceAccount:
  create: true
  automount: true
  annotations: {}
  name: ""

podAnnotations: {}
podLabels: {}

podSecurityContext:
  runAsNonRoot: true
  runAsUser: 10001
  runAsGroup: 10001
  fsGroup: 10001

securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 10001
  capabilities:
    drop:
      - ALL

service:
  type: ClusterIP
  port: 8088

database:
  # Hostname or cluster service for external PostgreSQL
  host: "postgres.default.svc.cluster.local"
  port: 5432
  name: "metrics"
  user: "postgres"
  password: "" # Plaintext password if not using existingSecret
  sslmode: "disable"

  # Name of an existing secret containing database credentials
  existingSecret: ""
  # Key inside existingSecret for password (used when database.existingSecret is set)
  existingSecretPasswordKey: "postgresql-password"
  # Optional key inside existingSecret that contains the complete DATABASE_URL
  urlSecretKey: ""

livenessProbe:
  httpGet:
    path: /healthz
    port: http
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /healthz
    port: http
  initialDelaySeconds: 3
  periodSeconds: 5
  timeoutSeconds: 2
  failureThreshold: 2

resources:
  limits:
    cpu: 200m
    memory: 128Mi
  requests:
    cpu: 20m
    memory: 32Mi

ingress:
  enabled: false
  className: ""
  annotations: {}
  hosts:
    - host: metrics.local
      paths:
        - path: /
          pathType: Prefix
  tls: []

nodeSelector: {}
tolerations: []
affinity: {}
```

- [ ] **Step 3: Create Helm templates**

Create `charts/smarthome-metrics/templates/_helpers.tpl`:
```tpl
{{/*
Expand the name of the chart.
*/}}
{{- define "smarthome-metrics.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "smarthome-metrics.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "smarthome-metrics.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "smarthome-metrics.labels" -}}
helm.sh/chart: {{ include "smarthome-metrics.chart" . }}
{{ include "smarthome-metrics.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "smarthome-metrics.selectorLabels" -}}
app.kubernetes.io/name: {{ include "smarthome-metrics.name" . }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "smarthome-metrics.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "smarthome-metrics.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Database secret name
*/}}
{{- define "smarthome-metrics.secretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- printf "%s-db" (include "smarthome-metrics.fullname" .) }}
{{- end }}
{{- end }}
```

Create `charts/smarthome-metrics/templates/serviceaccount.yaml`:
```yaml
{{- if .Values.serviceAccount.create -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "smarthome-metrics.serviceAccountName" . }}
  labels:
    {{- include "smarthome-metrics.labels" . | nindent 4 }}
  {{- with .Values.serviceAccount.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
automountServiceAccountToken: {{ .Values.serviceAccount.automount }}
{{- end }}
```

Create `charts/smarthome-metrics/templates/secret.yaml`:
```yaml
{{- if and (not .Values.database.existingSecret) .Values.database.password -}}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "smarthome-metrics.secretName" . }}
  labels:
    {{- include "smarthome-metrics.labels" . | nindent 4 }}
type: Opaque
stringData:
  DB_PASSWORD: {{ .Values.database.password | quote }}
{{- end }}
```

Create `charts/smarthome-metrics/templates/configmap.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "smarthome-metrics.fullname" . }}-config
  labels:
    {{- include "smarthome-metrics.labels" . | nindent 4 }}
data:
  PORT: {{ .Values.service.port | quote }}
  DB_HOST: {{ .Values.database.host | quote }}
  DB_PORT: {{ .Values.database.port | quote }}
  DB_NAME: {{ .Values.database.name | quote }}
  DB_USER: {{ .Values.database.user | quote }}
  DB_SSLMODE: {{ .Values.database.sslmode | quote }}
```

Create `charts/smarthome-metrics/templates/service.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "smarthome-metrics.fullname" . }}
  labels:
    {{- include "smarthome-metrics.labels" . | nindent 4 }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
      name: http
  selector:
    {{- include "smarthome-metrics.selectorLabels" . | nindent 4 }}
```

Create `charts/smarthome-metrics/templates/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "smarthome-metrics.fullname" . }}
  labels:
    {{- include "smarthome-metrics.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "smarthome-metrics.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      {{- with .Values.podAnnotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      labels:
        {{- include "smarthome-metrics.labels" . | nindent 8 }}
        {{- with .Values.podLabels }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
    spec:
      {{- with .Values.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      serviceAccountName: {{ include "smarthome-metrics.serviceAccountName" . }}
      securityContext:
        {{- toYaml .Values.podSecurityContext | nindent 8 }}
      containers:
        - name: {{ .Chart.Name }}
          securityContext:
            {{- toYaml .Values.securityContext | nindent 12 }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: {{ .Values.service.port }}
              protocol: TCP
          envFrom:
            - configMapRef:
                name: {{ include "smarthome-metrics.fullname" . }}-config
          env:
            {{- if .Values.database.urlSecretKey }}
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: {{ include "smarthome-metrics.secretName" . }}
                  key: {{ .Values.database.urlSecretKey }}
            {{- else if .Values.database.existingSecret }}
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ include "smarthome-metrics.secretName" . }}
                  key: {{ .Values.database.existingSecretPasswordKey }}
            {{- else if .Values.database.password }}
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ include "smarthome-metrics.secretName" . }}
                  key: DB_PASSWORD
            {{- end }}
          livenessProbe:
            {{- toYaml .Values.livenessProbe | nindent 12 }}
          readinessProbe:
            {{- toYaml .Values.readinessProbe | nindent 12 }}
          resources:
            {{- toYaml .Values.resources | nindent 12 }}
      {{- with .Values.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
```

Create `charts/smarthome-metrics/templates/ingress.yaml`:
```yaml
{{- if .Values.ingress.enabled -}}
{{- $fullName := include "smarthome-metrics.fullname" . -}}
{{- $svcPort := .Values.service.port -}}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ $fullName }}
  labels:
    {{- include "smarthome-metrics.labels" . | nindent 4 }}
  {{- with .Values.ingress.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- if .Values.ingress.className }}
  ingressClassName: {{ .Values.ingress.className }}
  {{- end }}
  {{- if .Values.ingress.tls }}
  tls:
    {{- range .Values.ingress.tls }}
    - hosts:
        {{- range .hosts }}
        - {{ . | quote }}
        {{- end }}
      secretName: {{ .secretName }}
    {{- end }}
  {{- end }}
  rules:
    {{- range .Values.ingress.hosts }}
    - host: {{ .host | quote }}
      http:
        paths:
          {{- range .paths }}
          - path: {{ .path }}
            pathType: {{ .pathType }}
            backend:
              service:
                name: {{ $fullName }}
                port:
                  number: {{ $svcPort }}
          {{- end }}
    {{- end }}
{{- end }}
```

Create `charts/smarthome-metrics/templates/NOTES.txt`:
```
SmartHome Metrics has been deployed!

1. Get the application URL:
{{- if .Values.ingress.enabled }}
{{- range $host := .Values.ingress.hosts }}
  http://{{ $host.host }}
{{- end }}
{{- else if contains "NodePort" .Values.service.type }}
  export NODE_PORT=$(kubectl get --namespace {{ .Release.Namespace }} -o jsonpath="{.spec.ports[0].nodePort}" services {{ include "smarthome-metrics.fullname" . }})
  export NODE_IP=$(kubectl get nodes --namespace {{ .Release.Namespace }} -o jsonpath="{.items[0].status.addresses[0].address}")
  echo "http://$NODE_IP:$NODE_PORT"
{{- else if contains "LoadBalancer" .Values.service.type }}
     NOTE: It may take a few minutes for the LoadBalancer IP to be available.
           You can watch the status of by running 'kubectl get --namespace {{ .Release.Namespace }} svc -w {{ include "smarthome-metrics.fullname" . }}'
  export SERVICE_IP=$(kubectl get svc --namespace {{ .Release.Namespace }} {{ include "smarthome-metrics.fullname" . }} --template "{{"{{ range (index .status.loadBalancer.ingress 0) }}{{.}}{{ end }}"}}")
  echo "http://$SERVICE_IP:{{ .Values.service.port }}"
{{- else if contains "ClusterIP" .Values.service.type }}
  export POD_NAME=$(kubectl get pods --namespace {{ .Release.Namespace }} -l "{{ include "smarthome-metrics.selectorLabels" . }}" -o jsonpath="{.items[0].metadata.name}")
  echo "Forwarding 8088 to local port 8088..."
  kubectl --namespace {{ .Release.Namespace }} port-forward $POD_NAME 8088:{{ .Values.service.port }}
{{- end }}

2. Test pushing a metric:
  curl -X POST http://127.0.0.1:8088/metric/main_meter/delivery_current \
       -H "Content-Type: text/plain" \
       -d "230.5"

3. Verify health status:
  curl http://127.0.0.1:8088/healthz
```

- [ ] **Step 4: Lint and template the Helm chart**

Run: `helm lint charts/smarthome-metrics`
Expected: 1 chart(s) linted, 0 chart(s) failed

Run: `helm template test charts/smarthome-metrics`
Expected: Renders valid Kubernetes YAML.

- [ ] **Step 5: Commit Task 7**

```bash
git add charts/
git commit -m "feat: add Helm chart for Kubernetes deployment"
```

---

### Task 8: End-to-End Verification, Documentation & Initial Tag

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write README.md with usage, architecture, and deployment instructions**

Create `README.md` covering:
- Project overview
- Endpoints specification (`POST /metric/{category}/{metric_name}` and `GET /healthz`)
- Local development & testing instructions (`go test -v ./...`)
- Docker building and running
- Helm installation commands
- GitHub Actions CI/CD explanation and how git tags trigger releases

- [ ] **Step 2: Run all unit and integration tests**

Run: `go test -v -race ./...`
Expected: All tests pass.

- [ ] **Step 3: Verify Helm templates with external secret values**

Run: `helm template test charts/smarthome-metrics --set database.existingSecret=my-db-secret`
Expected: Renders without error, includes secret reference in deployment.

- [ ] **Step 4: Commit README and verify git status**

```bash
git add README.md
git commit -m "docs: add comprehensive README with setup and deployment instructions"
```
