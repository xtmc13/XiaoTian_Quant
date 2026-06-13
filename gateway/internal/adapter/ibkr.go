package adapter

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// IBKRAdapter connects to Interactive Brokers via Client Portal Gateway REST API.
// Requires IB Gateway or TWS with Client Portal enabled on https://localhost:5000.
type IBKRAdapter struct {
	baseURL    string // default https://localhost:5000/v1/api
	accountID  string
	httpClient *http.Client
	mu         sync.RWMutex
	connected  bool
}

func NewIBKRAdapter(baseURL, accountID string) *IBKRAdapter {
	if baseURL == "" {
		baseURL = "https://localhost:5000/v1/api"
	}
	return &IBKRAdapter{
		baseURL:   baseURL,
		accountID: accountID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // localhost
			},
		},
	}
}

func (i *IBKRAdapter) Name() string     { return "ibkr" }
func (i *IBKRAdapter) Start() error     { i.connected = true; return nil }
func (i *IBKRAdapter) Stop() error      { i.connected = false; return nil }
func (i *IBKRAdapter) IsConnected() bool { return i.connected }

func (i *IBKRAdapter) get(path string, params url.Values) (map[string]any, error) {
	u := i.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := i.httpClient.Do(req)
	if err != nil { return nil, fmt.Errorf("ibkr: %w", err) }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result)
	return result, nil
}

func (i *IBKRAdapter) post(path string, body map[string]any) (map[string]any, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", i.baseURL+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := i.httpClient.Do(req)
	if err != nil { return nil, fmt.Errorf("ibkr: %w", err) }
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result, nil
}

// ── Account ──
func (i *IBKRAdapter) GetBalance() ([]map[string]any, error) {
	result, err := i.get("/portfolio/"+i.accountID+"/summary", nil)
	if err != nil { return nil, err }
	// Parse IBKR portfolio summary
	return []map[string]any{{"raw": result}}, nil
}

// ── Positions ──
func (i *IBKRAdapter) GetPositions() ([]map[string]any, error) {
	result, err := i.get("/portfolio/"+i.accountID+"/positions/0", nil)
	if err != nil { return nil, err }
	positions := make([]map[string]any, 0)
	if arr, ok := result["positions"].([]any); ok {
		for _, p := range arr { positions = append(positions, p.(map[string]any)) }
	}
	return positions, nil
}

// ── Orders ──
func (i *IBKRAdapter) PlaceOrder(symbol, side, orderType string, price, quantity float64) (map[string]any, error) {
	body := map[string]any{
		"acctId": i.accountID,
		"conid":  symbol, // IBKR uses contract ID, not symbol
		"orderType": strings.ToUpper(orderType),
		"side":    strings.ToUpper(side),
		"quantity": quantity,
		"tif":     "DAY",
	}
	if strings.ToUpper(orderType) == "LMT" { body["price"] = price }
	return i.post("/iserver/account/"+i.accountID+"/orders", body)
}

func (i *IBKRAdapter) CancelOrder(symbol, orderID string) (map[string]any, error) {
	return i.post("/iserver/account/"+i.accountID+"/orders/"+orderID, map[string]any{"orderId": orderID})
}

func (i *IBKRAdapter) GetOpenOrders(symbol string) ([]map[string]any, error) {
	result, err := i.get("/iserver/account/orders", nil)
	if err != nil { return nil, err }
	orders := make([]map[string]any, 0)
	if arr, ok := result["orders"].([]any); ok {
		for _, o := range arr { orders = append(orders, o.(map[string]any)) }
	}
	return orders, nil
}

func (i *IBKRAdapter) GetKlines(symbol, interval string, limit int) ([][]any, error) {
	return nil, fmt.Errorf("IBKR klines: please use IB Gateway/TWS connected mode — REST API requires running IB Gateway on localhost:5000")
}
func (i *IBKRAdapter) GetTicker(symbol string) (map[string]any, error) {
	return nil, fmt.Errorf("IBKR ticker: please use IB Gateway/TWS connected mode")
}
func (i *IBKRAdapter) StartMarketStream(symbols []string) error {
	return fmt.Errorf("IBKR market stream: requires IB Gateway/TWS running on localhost:4001")
}
func (i *IBKRAdapter) StartUserStream() error {
	return fmt.Errorf("IBKR user stream: requires IB Gateway/TWS running on localhost:4001")
}
