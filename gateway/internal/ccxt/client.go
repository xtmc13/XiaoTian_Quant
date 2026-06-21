// Package ccxt provides a Go client for the Python CCXT bridge service.
package ccxt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client calls the CCXT bridge REST API.
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates a CCXT bridge client.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = os.Getenv("CCXT_BRIDGE_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8002"
	}
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// OHLCV represents a single candlestick returned by CCXT.
type OHLCV struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// FetchOHLCV fetches historical candlesticks via CCXT.
func (c *Client) FetchOHLCV(exchange, symbol, timeframe string, limit int, since int64) ([]OHLCV, error) {
	body := map[string]any{
		"exchange":  exchange,
		"symbol":    symbol,
		"timeframe": timeframe,
		"limit":     limit,
	}
	if since > 0 {
		body["since"] = since
	}
	resp, err := c.post("/ohlcv", body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Bars []OHLCV `json:"bars"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	return result.Bars, nil
}

// FetchTickers fetches tickers for given symbols via CCXT.
func (c *Client) FetchTickers(exchange string, symbols []string) (map[string]map[string]any, error) {
	resp, err := c.post("/ticker", map[string]any{
		"exchange": exchange,
		"symbols":  symbols,
	})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tickers map[string]map[string]any `json:"tickers"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	return result.Tickers, nil
}

// Health checks if the CCXT bridge is reachable.
func (c *Client) Health() (map[string]any, error) {
	resp, err := c.client.Get(c.baseURL + "/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ccxt bridge health check failed: %s", string(body))
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// IsAvailable returns true if the CCXT bridge responds to health checks.
func (c *Client) IsAvailable() bool {
	_, err := c.Health()
	return err == nil
}

func (c *Client) post(path string, body map[string]any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Post(c.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ccxt bridge error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}
