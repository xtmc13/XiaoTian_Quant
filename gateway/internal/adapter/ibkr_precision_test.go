package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

/* ── IBKR 下单精度：保守本地规整（无公开 symbol 规则 REST）────── */

// newIBKRPrecisionEnv 在 ibkrBaseMux 之上追加订单捕获桩。
func newIBKRPrecisionEnv(t *testing.T) (*IBKRAdapter, func() map[string]any, *atomic.Int32) {
	t.Helper()
	mux := ibkrBaseMux()
	var orderHits atomic.Int32
	var lastOrder map[string]any
	mux.HandleFunc("/iserver/account/DU123456/orders", func(w http.ResponseWriter, r *http.Request) {
		orderHits.Add(1)
		body, _ := io.ReadAll(r.Body)
		var parsed struct {
			Orders []map[string]any `json:"orders"`
		}
		_ = json.Unmarshal(body, &parsed)
		if len(parsed.Orders) > 0 {
			lastOrder = parsed.Orders[0]
		}
		fmt.Fprint(w, `[{"id":"PREC-1","message":[]}]`)
	})
	a := newIBKRTestEnv(t, mux)
	return a, func() map[string]any { return lastOrder }, &orderHits
}

// 限价单：qty FLOOR 到 4 位小数；price 按 0.01 tick ROUND。
func TestIBKRPlaceOrderPrecisionNormalization(t *testing.T) {
	a, lastOrder, hits := newIBKRPrecisionEnv(t)

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190.256, 2.123456789)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	o := lastOrder()
	if o["quantity"] != "2.1234" {
		t.Fatalf("quantity = %v, want 2.1234 (4dp floor)", o["quantity"])
	}
	if o["price"] != "190.26" {
		t.Fatalf("price = %v, want 190.26 (0.01 tick round)", o["price"])
	}
	if hits.Load() != 1 {
		t.Fatalf("order hits = %d", hits.Load())
	}
}

// sub-dollar 价格：tick 切到 0.0001（低价股/加密档位）。
func TestIBKRPlaceOrderSubDollarTick(t *testing.T) {
	a, lastOrder, _ := newIBKRPrecisionEnv(t)

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 0.56789, 100)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if got := lastOrder()["price"]; got != "0.5679" {
		t.Fatalf("price = %v, want 0.5679 (0.0001 tick)", got)
	}
}

// 碎尘数量：qty floor 4 位后为 0（0.00001），本地拒绝，不发请求。
func TestIBKRPlaceOrderRejectsDustQuantity(t *testing.T) {
	a, _, hits := newIBKRPrecisionEnv(t)

	_, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, 0.00001)
	if err == nil {
		t.Fatal("dust quantity must be rejected locally")
	}
	var ce *OrderConstraintError
	if !errors.As(err, &ce) {
		t.Fatalf("want OrderConstraintError, got %v", err)
	}
	if !strings.Contains(err.Error(), "positive quantity") {
		t.Fatalf("error should mention quantity rule: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("no order must reach IBKR, hits = %d", hits.Load())
	}
}

// 止损单：auxPrice 同样按 tick 规整；市价单不带价格字段。
func TestIBKRPlaceStopAndMarketOrderPrecision(t *testing.T) {
	a, lastOrder, _ := newIBKRPrecisionEnv(t)

	if _, err := a.PlaceOrder("AAPL", "SELL", "STOP", 180.256, 10); err != nil {
		t.Fatalf("stop order: %v", err)
	}
	stp := lastOrder()
	if stp["orderType"] != "STP" || stp["auxPrice"] != "180.26" {
		t.Fatalf("stop order fields: %v", stp)
	}

	if _, err := a.PlaceOrder("AAPL", "BUY", "MARKET", 0, 3.123456789); err != nil {
		t.Fatalf("market order: %v", err)
	}
	mkt := lastOrder()
	if mkt["orderType"] != "MKT" {
		t.Fatalf("market type = %v", mkt["orderType"])
	}
	if mkt["quantity"] != "3.1234" {
		t.Fatalf("market quantity = %v, want 3.1234", mkt["quantity"])
	}
	if _, hasPrice := mkt["price"]; hasPrice {
		t.Fatal("market order must not carry price")
	}
}

// 浮点尾差：0.1+0.2 不得以 "0.30000000000000004" 上线。
func TestIBKRPlaceOrderFloatTail(t *testing.T) {
	a, lastOrder, _ := newIBKRPrecisionEnv(t)

	qty := 0.1 + 0.2
	if _, err := a.PlaceOrder("AAPL", "BUY", "LIMIT", 190, qty); err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if got := lastOrder()["quantity"]; got != "0.3000" {
		t.Fatalf("quantity = %v, want 0.3000 (float tail cleaned)", got)
	}
}
