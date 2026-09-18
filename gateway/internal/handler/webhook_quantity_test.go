package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTradingViewWebhookQuantityRequired：缺失/<=0 quantity 且未配置默认 → 400。
func TestTradingViewWebhookQuantityRequired(t *testing.T) {
	r := setupRouter()
	r.POST("/webhook/tv", TradingViewWebhook)

	cases := []struct {
		name string
		body string
	}{
		{"missing quantity", `{"symbol":"BTCUSDT","action":"buy"}`},
		{"zero quantity", `{"symbol":"BTCUSDT","action":"buy","quantity":0}`},
		{"negative quantity", `{"symbol":"BTCUSDT","action":"buy","quantity":-1}`},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/webhook/tv", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assertEq(t, w.Code, http.StatusBadRequest, tc.name+" must be 400")
		if !strings.Contains(w.Body.String(), "quantity required") {
			t.Fatalf("%s: body must contain quantity required, got %s", tc.name, w.Body.String())
		}
	}
}

// TestResolveWebhookQuantity：显式值优先；默认 0 → -1（拒绝）。
func TestResolveWebhookQuantity(t *testing.T) {
	if q := resolveWebhookQuantity(map[string]any{"quantity": 0.5}); q != 0.5 {
		t.Fatalf("explicit quantity = %v", q)
	}
	if q := resolveWebhookQuantity(map[string]any{"qty": 0.2}); q != 0.2 {
		t.Fatalf("explicit qty = %v", q)
	}
	if q := resolveWebhookQuantity(map[string]any{"quantity": 0}); q != -1 {
		t.Fatalf("missing/zero quantity must be -1 (config default 0), got %v", q)
	}
	if q := resolveWebhookQuantity(map[string]any{}); q != -1 {
		t.Fatalf("empty body must be -1, got %v", q)
	}
}
