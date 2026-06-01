package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// CCXTAdapter wraps the CCXT Bridge Python sidecar, providing unified
// access to 100+ exchanges via a single REST API.
type CCXTAdapter struct {
	exchangeName string
	bridgeURL    string
	httpClient   *http.Client
}

func NewCCXTAdapter(exchangeName string) *CCXTAdapter {
	url := os.Getenv("CCXT_BRIDGE_URL")
	if url == "" {
		url = "http://localhost:8002"
	}
	return &CCXTAdapter{
		exchangeName: strings.ToUpper(exchangeName),
		bridgeURL:    url,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *CCXTAdapter) Name() string     { return "ccxt_" + strings.ToLower(c.exchangeName) }
func (c *CCXTAdapter) Start() error     { return nil }
func (c *CCXTAdapter) Stop() error      { return nil }
func (c *CCXTAdapter) IsConnected() bool { return true }

// ── Bridge call ────────────────────────────────────────────────

func (c *CCXTAdapter) post(endpoint string, body map[string]any) (map[string]any, error) {
	body["exchange"] = c.exchangeName
	data, _ := json.Marshal(body)

	resp, err := c.httpClient.Post(c.bridgeURL+endpoint, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ccxt %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("ccxt parse: %w (body: %.200s)", err, string(respBody))
	}

	if detail, ok := result["detail"].(string); ok && resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ccxt error: %s", detail)
	}

	return result, nil
}

// ── Market Data ────────────────────────────────────────────────

func (c *CCXTAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	result, err := c.post("/ohlcv", map[string]any{
		"symbol":    symbol,
		"timeframe": interval,
		"limit":     limit,
	})
	if err != nil {
		return nil, err
	}

	bars, _ := result["bars"].([]any)
	klines := make([][]any, 0, len(bars))
	for _, b := range bars {
		bar, _ := b.(map[string]any)
		klines = append(klines, []any{
			int64(bar["time"].(float64)),
			bar["open"], bar["high"], bar["low"], bar["close"], bar["volume"],
		})
	}
	return klines, nil
}

func (c *CCXTAdapter) GetTicker(symbol string) (map[string]any, error) {
	result, err := c.post("/ticker", map[string]any{
		"symbols": []string{symbol},
	})
	if err != nil {
		return nil, err
	}

	tickers, _ := result["tickers"].(map[string]any)
	if t, ok := tickers[symbol].(map[string]any); ok {
		return t, nil
	}
	return nil, fmt.Errorf("ccxt: ticker not found for %s", symbol)
}

// ── Account ────────────────────────────────────────────────────

func (c *CCXTAdapter) GetBalance() ([]map[string]any, error) {
	result, err := c.post("/balance", map[string]any{
		"api_key":    os.Getenv(strings.ToUpper(c.exchangeName) + "_API_KEY"),
		"api_secret": os.Getenv(strings.ToUpper(c.exchangeName) + "_API_SECRET"),
	})
	if err != nil {
		return nil, err
	}

	total, _ := result["total"].(map[string]any)
	free, _ := result["free"].(map[string]any)
	balances := make([]map[string]any, 0, len(total))
	for asset, v := range total {
		balances = append(balances, map[string]any{
			"asset":  asset,
			"free":   free[asset],
			"locked": v.(float64) - free[asset].(float64),
		})
	}
	return balances, nil
}

// ── Orders ─────────────────────────────────────────────────────

func (c *CCXTAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	result, err := c.post("/order", map[string]any{
		"symbol":     symbol,
		"side":       side,
		"order_type": orderType,
		"price":      price,
		"quantity":   quantity,
		"api_key":    os.Getenv(strings.ToUpper(c.exchangeName) + "_API_KEY"),
		"api_secret": os.Getenv(strings.ToUpper(c.exchangeName) + "_API_SECRET"),
	})
	if err != nil {
		return nil, err
	}
	order, _ := result["order"].(map[string]any)
	return order, nil
}

func (c *CCXTAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return c.post("/cancel", map[string]any{
		"symbol":   symbol,
		"order_id": orderID,
		"api_key":    os.Getenv(strings.ToUpper(c.exchangeName) + "_API_KEY"),
		"api_secret": os.Getenv(strings.ToUpper(c.exchangeName) + "_API_SECRET"),
	})
}

func (c *CCXTAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	result, err := c.post("/orders", map[string]any{
		"symbol": symbol,
		"api_key":    os.Getenv(strings.ToUpper(c.exchangeName) + "_API_KEY"),
		"api_secret": os.Getenv(strings.ToUpper(c.exchangeName) + "_API_SECRET"),
	})
	if err != nil {
		return nil, err
	}
	orders, _ := result["orders"].([]any)
	out := make([]map[string]any, 0, len(orders))
	for _, o := range orders {
		out = append(out, o.(map[string]any))
	}
	return out, nil
}

func (c *CCXTAdapter) GetPositions() ([]map[string]any, error) {
	result, err := c.post("/positions", map[string]any{
		"api_key":    os.Getenv(strings.ToUpper(c.exchangeName) + "_API_KEY"),
		"api_secret": os.Getenv(strings.ToUpper(c.exchangeName) + "_API_SECRET"),
	})
	if err != nil {
		return nil, err
	}
	positions, _ := result["positions"].([]any)
	out := make([]map[string]any, 0, len(positions))
	for _, p := range positions {
		out = append(out, p.(map[string]any))
	}
	return out, nil
}

func (c *CCXTAdapter) StartMarketStream(symbols []string) error { return nil }
func (c *CCXTAdapter) StartUserStream() error                    { return nil }
