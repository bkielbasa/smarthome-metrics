package pse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type rawPSEEntry struct {
	DTime          string  `json:"dtime"`
	Period         string  `json:"period"`
	RCEPLN         float64 `json:"rce_pln"`
	DTimeUTC       string  `json:"dtime_utc"`
	PeriodUTC      string  `json:"period_utc"`
	BusinessDate   string  `json:"business_date"`
	PublicationTS  string  `json:"publication_ts"`
}

type rawPSEResponse struct {
	Value []rawPSEEntry `json:"value"`
}

type PriceRecord struct {
	Timestamp time.Time
	RCEMWh    float64
	RCEKWh    float64
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *Client) FetchPrices(ctx context.Context, fromDate string) ([]PriceRecord, error) {
	// Construct filter: $filter=business_date ge 'fromDate'
	filterQuery := fmt.Sprintf("business_date ge '%s'", fromDate)
	reqURL := fmt.Sprintf("%s?$filter=%s", c.baseURL, url.QueryEscape(filterQuery))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pse request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pse api returned status: %d", resp.StatusCode)
	}

	var raw rawPSEResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode pse response: %w", err)
	}

	records := make([]PriceRecord, 0, len(raw.Value))
	for _, entry := range raw.Value {
		// Parse UTC timestamp: format "2006-01-02 15:04:05"
		ts, err := time.ParseInLocation("2006-01-02 15:04:05", entry.DTimeUTC, time.UTC)
		if err != nil {
			// Fallback: try parsing local dtime if dtime_utc is unavailable
			ts, err = time.ParseInLocation("2006-01-02 15:04:05", entry.DTime, time.UTC)
			if err != nil {
				continue
			}
		}

		records = append(records, PriceRecord{
			Timestamp: ts,
			RCEMWh:    entry.RCEPLN,
			RCEKWh:    entry.RCEPLN / 1000.0,
		})
	}

	return records, nil
}
