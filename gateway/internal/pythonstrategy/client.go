// Package pythonstrategy provides a Go client for the Python strategy engine.
package pythonstrategy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// Client calls the Python strategy engine REST API.
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates a Python strategy engine client.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = os.Getenv("PYTHON_STRATEGY_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8003"
	}
	return &Client{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Signal represents a strategy signal returned by the Python engine.
type Signal struct {
	Time      int64   `json:"time"`
	BarIndex  int     `json:"bar_index"`
	Action    string  `json:"action"` // buy / sell / close
	Price     float64 `json:"price"`
	Reason    string  `json:"reason"`
	Size      float64 `json:"size,omitempty"`
}

// RunIndicatorStrategy runs a vectorized DataFrame strategy.
func (c *Client) RunIndicatorStrategy(code string, bars []model.Bar, params map[string]any, symbol, interval string) ([]Signal, error) {
	return c.runStrategy("/indicator/run", code, bars, params, symbol, interval)
}

// RunScriptStrategy runs an event-driven script strategy.
func (c *Client) RunScriptStrategy(code string, bars []model.Bar, params map[string]any, symbol, interval string) ([]Signal, error) {
	return c.runStrategy("/script/run", code, bars, params, symbol, interval)
}

func (c *Client) runStrategy(path, code string, bars []model.Bar, params map[string]any, symbol, interval string) ([]Signal, error) {
	if params == nil {
		params = map[string]any{}
	}
	barJSON := make([]map[string]any, len(bars))
	for i, b := range bars {
		barJSON[i] = map[string]any{
			"time":   b.Time,
			"open":   b.Open,
			"high":   b.High,
			"low":    b.Low,
			"close":  b.Close,
			"volume": b.Volume,
		}
	}
	body := map[string]any{
		"code":     code,
		"bars":     barJSON,
		"params":   params,
		"symbol":   symbol,
		"interval": interval,
	}
	resp, err := c.post(path, body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Success bool     `json:"success"`
		Signals []Signal `json:"signals"`
		Count   int      `json:"count"`
		Detail  string   `json:"detail"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("python strategy failed: %s", result.Detail)
	}
	return result.Signals, nil
}

// Health checks if the Python strategy engine is reachable.
func (c *Client) Health() (map[string]any, error) {
	resp, err := c.client.Get(c.baseURL + "/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("python strategy engine health check failed: %s", string(body))
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// IsAvailable returns true if the Python strategy engine responds to health checks.
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
		return nil, fmt.Errorf("python strategy engine error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}
