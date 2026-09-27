package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"math"
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

	if math.IsNaN(val) || math.IsInf(val, 0) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "body must be a finite numeric float",
		})
		return
	}

	if err := h.store.InsertMetric(r.Context(), category, metricName, val); err != nil {
		slog.ErrorContext(r.Context(), "failed to persist metric",
			"category", category,
			"metric_name", metricName,
			"error", err,
		)
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
		slog.WarnContext(r.Context(), "health check ping failed", "error", err)
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
