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

func TestMetricHandler_NaN(t *testing.T) {
	store := &db.MockStore{}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", bytes.NewBufferString("NaN"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for NaN, got %d", rec.Code)
	}
}

func TestMetricHandler_Infinity(t *testing.T) {
	store := &db.MockStore{}
	handler := NewRouter(store)

	req := httptest.NewRequest(http.MethodPost, "/metric/main_meter/delivery_current", bytes.NewBufferString("+Inf"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for +Inf, got %d", rec.Code)
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
