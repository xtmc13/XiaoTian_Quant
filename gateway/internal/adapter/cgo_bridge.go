//go:build cgo && xtengine
// +build cgo,xtengine

// Package adapter provides CGo bridge to the Rust matching engine.
// The Rust library is compiled as a cdylib (.dll on Windows, .so on Linux, .dylib on macOS).
//
// This path is opt-in via the xtengine tag (benchmark/参照 only, not the main
// path — the pure-Go engine in matching.go is the production default and is
// used whenever xtengine is absent, regardless of CGO).
//
// Build:
//
//	cd engine && cargo build --release
//	go build -tags xtengine ./cmd/server/
package adapter

/*
#cgo LDFLAGS: -L${SRCDIR}/../../../engine/target/release -lxt_matching
#cgo linux LDFLAGS: -ldl -lm
#cgo windows LDFLAGS: -lws2_32 -luserenv

#include <stdlib.h>

// Rust FFI declarations
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
	"sync"
	"unsafe"
)

// MatchingEngine wraps the Rust matching engine via CGo.
type MatchingEngine struct {
	symbol string
	mu     sync.Mutex
}

var (
	engines   = make(map[string]*MatchingEngine)
	enginesMu sync.Mutex
)

// NewMatchingEngine creates or returns an existing Rust engine for a symbol.
func NewMatchingEngine(symbol string) *MatchingEngine {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	if eng, ok := engines[symbol]; ok {
		return eng
	}
	eng := &MatchingEngine{symbol: symbol}
	cs := C.CString(symbol)
	defer C.free(unsafe.Pointer(cs))
	cstr := C.engine_create(cs)
	result := C.GoString(cstr)
	C.free_string(cstr)
	var resp map[string]any
	json.Unmarshal([]byte(result), &resp)
	if resp["status"] == "ok" {
		engines[symbol] = eng
		return eng
	}
	return eng
}

// SubmitOrder sends an order JSON to the Rust engine and returns the result.
func (e *MatchingEngine) SubmitOrder(side string, orderType string, price, quantity float64, userID uint64) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	orderJSON := fmt.Sprintf(
		`{"symbol":"%s","side":"%s","order_type":"%s","price":%f,"quantity":%f,"user_id":%d}`,
		e.symbol, side, orderType, price, quantity, userID,
	)
	cs := C.CString(orderJSON)
	defer C.free(unsafe.Pointer(cs))

	cstr := C.engine_submit_order(cs)
	result := C.GoString(cstr)
	C.free_string(cstr)
	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		return nil, err
	}
	if errMsg, ok := resp["error"]; ok {
		return nil, fmt.Errorf("%v", errMsg)
	}
	return resp, nil
}

// CancelOrder cancels an order by ID.
func (e *MatchingEngine) CancelOrder(orderID uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))

	cstr := C.engine_cancel_order(cs, C.ulonglong(orderID))
	result := C.GoString(cstr)
	C.free_string(cstr)
	var resp map[string]any
	json.Unmarshal([]byte(result), &resp)
	if errMsg, ok := resp["error"]; ok {
		return fmt.Errorf("%v", errMsg)
	}
	return nil
}

// Snapshot returns the order book snapshot (bids and asks).
func (e *MatchingEngine) Snapshot(depth int) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))

	cstr := C.engine_snapshot(cs, C.uint(depth))
	result := C.GoString(cstr)
	C.free_string(cstr)
	var resp map[string]any
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// TradeCount returns the total number of trades executed.
func (e *MatchingEngine) TradeCount() uint64 {
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	return uint64(C.engine_trade_count(cs))
}

// GetTrades returns recent trades.
func (e *MatchingEngine) GetTrades(limit int) ([]map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))

	cstr := C.engine_get_trades(cs, C.uint(limit))
	result := C.GoString(cstr)
	C.free_string(cstr)
	var trades []map[string]any
	if err := json.Unmarshal([]byte(result), &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

// DestroyEngine removes the engine from memory.
func (e *MatchingEngine) Destroy() {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	cs := C.CString(e.symbol)
	defer C.free(unsafe.Pointer(cs))
	C.engine_destroy(cs)
	delete(engines, e.symbol)
}

// BalanceProvider 与纯 Go 引擎（matching.go）同名的资金校验接口，
// 供 internal/service 统一编译。Rust FFI 侧不做资金校验（该路径仅为基准参照），
// 注入后被忽略。
type BalanceProvider interface {
	Available(userID uint64, asset string) float64
}

// SetBalanceProvider 在 FFI 路径为 no-op：资金校验是纯 Go 引擎的生产能力。
func (e *MatchingEngine) SetBalanceProvider(p BalanceProvider) {}

// SetOnFill 在 FFI 路径为 no-op：成交回写钩子是纯 Go 引擎的生产能力，
// FFI 路径的成交只能经 GetTrades 轮询读取。
func (e *MatchingEngine) SetOnFill(fn func(orderID uint64, filledQty, avgPrice float64)) {}
