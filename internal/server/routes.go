package server

import (
	"net/http"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/db"
)

// NewServer registers routes and returns the configured *http.ServeMux.
func NewServer(store db.MetricStore) *http.ServeMux {
	mux := http.NewServeMux()
	h := NewHandler(store)

	mux.HandleFunc("POST /metric/{category}/{metric_name}", h.HandlePostMetric)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)

	return mux
}

// NewRouter registers routes and returns the HTTP handler.
func NewRouter(store db.MetricStore) http.Handler {
	return NewServer(store)
}
