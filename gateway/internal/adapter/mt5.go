package adapter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MT5Adapter connects to MetaTrader 5 via a Python ZeroMQ bridge.
// Requires the MT5 bridge service running (sandbox/mt5_bridge.py).
// MT5 supports: forex, CFDs, commodities, indices, crypto.
type MT5Adapter struct {
	bridgeURL  string // http://localhost:8003
	server     string
	login      int
	password   string
	terminalPath string
	httpClient *http.Client
	mu         sync.RWMutex
	connected  bool
}

func NewMT5Adapter(bridgeURL, server string, login int, password, terminalPath string) *MT5Adapter {
	if bridgeURL == "" {
		bridgeURL = "http://localhost:8003"
	}
	return &MT5Adapter{
		bridgeURL:    bridgeURL,
		server:       server,
		login:        login,
		password:     password,
		terminalPath: terminalPath,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *MT5Adapter) Name() string     { return "mt5" }
func (m *MT5Adapter) Start() error     { m.connected = true; return nil }
func (m *MT5Adapter) Stop() error      { m.connected = false; return nil }
func (m *MT5Adapter) IsConnected() bool { return m.connected }

func (m *MT5Adapter) call(endpoint string, body map[string]any) (map[string]any, error) {
	body["server"] = m.server
	body["login"] = m.login
	body["password"] = m.password
	body["terminal_path"] = m.terminalPath

	data, _ := json.Marshal(body)
	resp, err := m.httpClient.Post(m.bridgeURL+endpoint, "application/json", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("mt5 bridge: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

// ── Account ──
func (m *MT5Adapter) GetBalance() ([]map[string]any, error) {
	result, err := m.call("/account", nil)
	if err != nil { return nil, err }
	return []map[string]any{{"raw": result}}, nil
}

// ── Positions ──
func (m *MT5Adapter) GetPositions() ([]map[string]any, error) {
	result, err := m.call("/positions", nil)
	if err != nil { return nil, err }
	positions := make([]map[string]any, 0)
	if arr, ok := result["positions"].([]any); ok {
		for _, p := range arr { positions = append(positions, p.(map[string]any)) }
	}
	return positions, nil
}

// ── Orders ──
func (m *MT5Adapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	mt5Type := 0 // ORDER_TYPE_BUY
	if strings.ToUpper(side) == "SELL" { mt5Type = 1 }
	return m.call("/order", map[string]any{
		"symbol":   symbol,
		"type":     mt5Type,
		"price":    price,
		"volume":   quantity,
	})
}

func (m *MT5Adapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return m.call("/cancel", map[string]any{"ticket": orderID})
}

func (m *MT5Adapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	result, err := m.call("/orders", nil)
	if err != nil { return nil, err }
	orders := make([]map[string]any, 0)
	if arr, ok := result["orders"].([]any); ok {
		for _, o := range arr { orders = append(orders, o.(map[string]any)) }
	}
	return orders, nil
}

// ── Market Data ──
func (m *MT5Adapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	result, err := m.call("/klines", map[string]any{
		"symbol":   symbol,
		"timeframe": interval,
		"count":    limit,
	})
	if err != nil { return nil, err }
	bars, _ := result["bars"].([]any)
	klines := make([][]any, 0, len(bars))
	for _, b := range bars { klines = append(klines, b.([]any)) }
	return klines, nil
}

func (m *MT5Adapter) GetTicker(symbol string) (map[string]any, error) {
	return m.call("/ticker", map[string]any{"symbol": symbol})
}

func (m *MT5Adapter) StartMarketStream(symbols []string) error { return nil }
func (m *MT5Adapter) StartUserStream() error                    { return nil }
