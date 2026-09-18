package cra

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func TestCustomCodeID(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   int64
		ok     bool
	}{
		{"nil params", nil, 0, false},
		{"empty params", map[string]any{}, 0, false},
		{"no custom key", map[string]any{"macd": map[string]any{}}, 0, false},
		{"float64 code_id", map[string]any{"custom": map[string]any{"code_id": float64(7), "name": "x"}}, 7, true},
		{"int code_id", map[string]any{"custom": map[string]any{"code_id": 9}}, 9, true},
		{"missing code_id", map[string]any{"custom": map[string]any{"name": "x"}}, 0, false},
		{"wrong type code_id", map[string]any{"custom": map[string]any{"code_id": "abc"}}, 0, false},
	}
	for _, c := range cases {
		got, ok := customCodeID(c.params)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: got (%d,%v), want (%d,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestLastBarSignalMatch(t *testing.T) {
	buyLast := map[string]any{"signals": []any{
		map[string]any{"type": "buy", "data": []any{nil, nil, 101.5}},
	}}
	sellLast := map[string]any{"signals": []any{
		map[string]any{"type": "sell", "data": []any{nil, 99.0, 98.5}},
	}}
	noSignalLast := map[string]any{"signals": []any{
		map[string]any{"type": "buy", "data": []any{101.5, nil, nil}},
	}}
	if !lastBarSignalMatch(buyLast, SideLong) {
		t.Error("buy at last bar should confirm long")
	}
	if lastBarSignalMatch(buyLast, SideShort) {
		t.Error("buy at last bar should NOT confirm short")
	}
	if !lastBarSignalMatch(sellLast, SideShort) {
		t.Error("sell at last bar should confirm short")
	}
	if lastBarSignalMatch(noSignalLast, SideLong) {
		t.Error("nil at last bar should not confirm")
	}
	if lastBarSignalMatch(nil, SideLong) || lastBarSignalMatch(map[string]any{}, SideLong) {
		t.Error("nil/empty output should not confirm")
	}
}

// mockSandbox 返回固定 output 的伪沙箱，并统计请求数。
func mockSandbox(t *testing.T, output map[string]any, calls *int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(calls, 1)
		if r.URL.Path != "/execute" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req sandboxExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Code == "" || len(req.DfJSON) == 0 {
			t.Errorf("request missing code/df_json: %+v", req)
		}
		resp := map[string]any{"success": true, "msg": "ok", "output": output}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestExecuteCustomIndicator(t *testing.T) {
	var calls int64
	srv := mockSandbox(t, map[string]any{"signals": []any{
		map[string]any{"type": "buy", "data": []any{nil, nil, 1.0}},
	}}, &calls)
	defer srv.Close()
	t.Setenv("SANDBOX_URL", srv.URL)

	bars := []model.Bar{
		{Time: 1, Open: 1, High: 1, Low: 1, Close: 1, Volume: 1},
		{Time: 2, Open: 2, High: 2, Low: 2, Close: 2, Volume: 1},
		{Time: 3, Open: 3, High: 3, Low: 3, Close: 3, Volume: 1},
	}
	confirmed, err := executeCustomIndicator("output = {}", SideLong, bars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !confirmed {
		t.Error("buy at last bar should confirm long")
	}
	if atomic.LoadInt64(&calls) != 1 {
		t.Errorf("expected 1 sandbox call, got %d", calls)
	}
}
