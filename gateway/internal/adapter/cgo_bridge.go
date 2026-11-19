//go:build cgo && xtengine

// Package adapter provides the production CGo bridge to the Rust matching engine.
package adapter

/*
#cgo LDFLAGS: -lxt_matching
#cgo linux LDFLAGS: -ldl -lm
#cgo windows LDFLAGS: -lws2_32 -luserenv

#include <stdlib.h>

extern char* engine_create(const char* symbol);
extern void engine_destroy(const char* symbol);
extern char* engine_submit_order(const char* json);
extern char* engine_cancel_order(const char* symbol, unsigned long long order_id);
extern char* engine_snapshot(const char* symbol, unsigned int depth);
extern unsigned long long engine_trade_count(const char* symbol);
extern char* engine_get_trades(const char* symbol, unsigned int limit);
extern void free_string(char* ptr);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"unsafe"
)

type MatchingEngine struct {
	symbol         string
	mu             sync.Mutex
	balance        BalanceProvider
	onFill         func(orderID uint64, filledQty, avgPrice float64)
	initialization error
}

var (
	engines   = make(map[string]*MatchingEngine)
	enginesMu sync.Mutex
)

func NewMatchingEngine(symbol string) *MatchingEngine {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	if eng, ok := engines[symbol]; ok {
		return eng
	}

	eng := &MatchingEngine{symbol: symbol}
	if strings.TrimSpace(symbol) == "" || strings.IndexByte(symbol, 0) >= 0 {
		eng.initialization = fmt.Errorf("invalid matching-engine symbol")
		return eng
	}

	cs := C.CString(symbol)
	defer C.free(unsafe.Pointer(cs))
	result, err := rustString(C.engine_create(cs))
	if err != nil {
		eng.initialization = err
		return eng
	}
	var response struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		eng.initialization = fmt.Errorf("decode Rust engine creation response: %w", err)
		return eng
	}
	if response.Error != "" {
		eng.initialization = fmt.Errorf("Rust engine creation failed: %s", response.Error)
		return eng
	}
	if response.Status != "ok" {
		eng.initialization = fmt.Errorf("Rust engine creation returned unexpected status %q", response.Status)
		return eng
	}

	engines[symbol] = eng
	return eng
}

func rustString(ptr *C.char) (string, error) {
	if ptr == nil {
		return "", fmt.Errorf("Rust FFI returned a null string")
	}
	defer C.free_string(ptr)
	return C.GoString(ptr), nil
}

type rustOrderRequest struct {
	Symbol         string   `json:"symbol"`
	Side           string   `json:"side"`
	OrderType      string   `json:"order_type"`
	Price          float64  `json:"price"`
	Quantity       float64  `json:"quantity"`
	UserID         uint64   `json:"user_id"`
	AvailableBase  *float64 `json:"available_base,omitempty"`
	AvailableQuote *float64 `json:"available_quote,omitempty"`
}

type rustTrade struct {
	ID          uint64  `json:"id"`
	BuyOrderID  uint64  `json:"buy_order_id"`
	SellOrderID uint64  `json:"sell_order_id"`
	Price       float64 `json:"price"`
	Quantity    float64 `json:"quantity"`
}

type rustFill struct {
	OrderID   uint64  `json:"order_id"`
	FilledQty float64 `json:"filled_qty"`
	AvgPrice  float64 `json:"avg_price"`
}

type rustSubmitResponse struct {
	Error     string      `json:"error"`
	Status    string      `json:"status"`
	OrderID   uint64      `json:"order_id"`
	Side      string      `json:"side"`
	OrderType string      `json:"order_type"`
	Price     float64     `json:"price"`
	Quantity  float64     `json:"quantity"`
	Filled    float64     `json:"filled"`
	Trades    []rustTrade `json:"trades"`
	Fills     []rustFill  `json:"fills"`
}

func (e *MatchingEngine) SubmitOrder(side, orderType string, price, quantity float64, userID uint64) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialization != nil {
		return nil, e.initialization
	}
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if math.IsNaN(price) || math.IsInf(price, 0) ||
		(orderType == "limit" && price <= 0) ||
		(orderType == "market" && price < 0) {
		return nil, fmt.Errorf("invalid price for %s order", orderType)
	}
	if side != "buy" && side != "sell" {
		return nil, fmt.Errorf("side must be buy or sell")
	}
	if orderType != "limit" && orderType != "market" {
		return nil, fmt.Errorf("order_type must be limit or market")
	}

	request := rustOrderRequest{
		Symbol:    e.symbol,
		Side:      side,
		OrderType: orderType,
		Price:     price,
		Quantity:  quantity,
		UserID:    userID,
	}
	if e.balance != nil && userID != 0 {
		base, quote := splitSymbolAssets(e.symbol)
		switch {
		case orderType == "limit" && side == "buy":
			available := e.balance.Available(userID, quote)
			if err := validateAvailableBalance(quote, available); err != nil {
				return nil, err
			}
			if price*quantity > available {
				return nil, fmt.Errorf("insufficient balance: need %.8f %s, available %.8f", price*quantity, quote, math.Max(available, 0))
			}
		case orderType == "limit" && side == "sell":
			available := e.balance.Available(userID, base)
			if err := validateAvailableBalance(base, available); err != nil {
				return nil, err
			}
			if quantity > available {
				return nil, fmt.Errorf("insufficient balance: need %.8f %s, available %.8f", quantity, base, math.Max(available, 0))
			}
		case orderType == "market" && side == "buy":
			available := e.balance.Available(userID, quote)
			if err := validateAvailableBalance(quote, available); err != nil {
				return nil, err
			}
			if !math.IsInf(available, 1) {
				capped := math.Max(available, 0)
				request.AvailableQuote = &capped
			}
		case orderType == "market" && side == "sell":
			available := e.balance.Available(userID, base)
			if err := validateAvailableBalance(base, available); err != nil {
				return nil, err
			}
			if available <= 0 {
				return nil, fmt.Errorf("insufficient balance: no %s available to sell", base)
			}
			if !math.IsInf(available, 1) {
				request.AvailableBase = &available
			}
		}
	}

	orderJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode order for Rust matching engine: %w", err)
	}
	cs := C.CString(string(orderJSON))
	defer C.free(unsafe.Pointer(cs))
	result, err := rustString(C.engine_submit_order(cs))
	if err != nil {
		return nil, err
	}

	var response rustSubmitResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		return nil, fmt.Errorf("decode Rust order response: %w", err)
	}
	if response.Error != "" {
		return nil, fmt.Errorf("%s", response.Error)
	}
	if response.Status == "" {
		return nil, fmt.Errorf("Rust order response omitted status")
	}

	trades := make([]any, len(response.Trades))
	for i, trade := range response.Trades {
		trades[i] = map[string]any{
			"id":            trade.ID,
			"buy_order_id":  trade.BuyOrderID,
			"sell_order_id": trade.SellOrderID,
			"price":         trade.Price,
			"quantity":      trade.Quantity,
		}
	}
	fills := make([]any, len(response.Fills))
	for i, fill := range response.Fills {
		fills[i] = map[string]any{
			"order_id":   fill.OrderID,
			"filled_qty": fill.FilledQty,
			"avg_price":  fill.AvgPrice,
		}
		if e.onFill != nil {
			e.onFill(fill.OrderID, fill.FilledQty, fill.AvgPrice)
		}
	}

	return map[string]any{
		"status":     response.Status,
		"order_id":   response.OrderID,
		"side":       response.Side,
		"order_type": response.OrderType,
		"price":      response.Price,
		"quantity":   response.Quantity,
		"filled":     response.Filled,
		"trades":     trades,
		"fills":      fills,
	}, nil
}

func (e *MatchingEngine) CancelOrder(orderID uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialization != nil {
		return e.initialization
	}
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	result, err := rustString(C.engine_cancel_order(cs, C.ulonglong(orderID)))
	if err != nil {
		return err
	}
	var response struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		return fmt.Errorf("decode Rust cancel response: %w", err)
	}
	if response.Error != "" {
		return fmt.Errorf("%s", response.Error)
	}
	if response.Status != "ok" {
		return fmt.Errorf("Rust cancel returned unexpected status %q", response.Status)
	}
	return nil
}

func (e *MatchingEngine) Snapshot(depth int) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialization != nil {
		return nil, e.initialization
	}
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	result, err := rustString(C.engine_snapshot(cs, C.uint(depth)))
	if err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		return nil, fmt.Errorf("decode Rust snapshot response: %w", err)
	}
	if errMsg, ok := response["error"].(string); ok {
		return nil, fmt.Errorf("%s", errMsg)
	}
	return response, nil
}

func (e *MatchingEngine) TradeCount() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialization != nil {
		return 0
	}
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	return uint64(C.engine_trade_count(cs))
}

func (e *MatchingEngine) GetTrades(limit int) ([]map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialization != nil {
		return nil, e.initialization
	}
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	result, err := rustString(C.engine_get_trades(cs, C.uint(limit)))
	if err != nil {
		return nil, err
	}
	var trades []map[string]any
	if err := json.Unmarshal([]byte(result), &trades); err != nil {
		return nil, fmt.Errorf("decode Rust trades response: %w", err)
	}
	return trades, nil
}

func (e *MatchingEngine) Destroy() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialization != nil {
		return
	}
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	C.engine_destroy(cs)
	enginesMu.Lock()
	delete(engines, e.symbol)
	enginesMu.Unlock()
}

type BalanceProvider interface {
	Available(userID uint64, asset string) float64
}

func (e *MatchingEngine) SetBalanceProvider(provider BalanceProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.balance = provider
}

func (e *MatchingEngine) SetOnFill(fn func(orderID uint64, filledQty, avgPrice float64)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onFill = fn
}
