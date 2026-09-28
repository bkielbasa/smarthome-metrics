package pse_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bartlomiejklimczak/smarthome-metrics/internal/pse"
)

func TestClient_FetchPrices_Success(t *testing.T) {
	fixture := `{
		"value": [
			{
				"dtime": "2024-09-01 00:15:00",
				"period": "00:00 - 00:15",
				"rce_pln": 548.73,
				"dtime_utc": "2024-08-31 22:15:00",
				"period_utc": "22:00 - 22:15",
				"business_date": "2024-09-01"
			},
			{
				"dtime": "2024-09-01 00:30:00",
				"period": "00:15 - 00:30",
				"rce_pln": 600.00,
				"dtime_utc": "2024-08-31 22:30:00",
				"period_utc": "22:15 - 22:30",
				"business_date": "2024-09-01"
			}
		]
	}`

	var requestedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fixture))
	}))
	defer server.Close()

	client := pse.NewClient(server.URL, server.Client())
	records, err := client.FetchPrices(context.Background(), "2024-09-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if !strings.Contains(requestedURL, "business_date") {
		t.Errorf("expected URL to contain business_date, got %s", requestedURL)
	}

	expectedTS := time.Date(2024, 8, 31, 22, 15, 0, 0, time.UTC)
	if !records[0].Timestamp.Equal(expectedTS) {
		t.Errorf("expected timestamp %v, got %v", expectedTS, records[0].Timestamp)
	}
	if records[0].RCEMWh != 548.73 {
		t.Errorf("expected RCEMWh 548.73, got %f", records[0].RCEMWh)
	}
	if records[0].RCEKWh != 0.54873 {
		t.Errorf("expected RCEKWh 0.54873, got %f", records[0].RCEKWh)
	}

	if records[1].RCEMWh != 600.00 || records[1].RCEKWh != 0.60000 {
		t.Errorf("expected 600.00 / 0.60, got %f / %f", records[1].RCEMWh, records[1].RCEKWh)
	}
}

func TestClient_FetchPrices_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := pse.NewClient(server.URL, server.Client())
	_, err := client.FetchPrices(context.Background(), "2024-09-01")
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
}

func TestClient_FetchPrices_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := pse.NewClient(server.URL, server.Client())
	_, err := client.FetchPrices(context.Background(), "2024-09-01")
	if err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}
}
