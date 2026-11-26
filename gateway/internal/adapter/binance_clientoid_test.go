package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// clientOidTestEnv 模拟 Binance fapi/spot 按 origClientOrderId 查单。
type clientOidTestEnv struct {
	srv *httptest.Server

	futHits  int
	spotHits int
	lastOID  string

	futMode  string // hit | missing
	spotMode string // hit | missing
}

func newClientOidTestEnv(t *testing.T) *clientOidTestEnv {
	t.Helper()
	env := &clientOidTestEnv{futMode: "hit", spotMode: "missing"}
	mux := http.NewServeMux()

	hitOrder := `{"orderId":998877,"status":"NEW","executedQty":"0","avgPrice":"0"}`
	missing := `{"code":-2013,"msg":"Order does not exist."}`

	handle := func(hits *int, mode *string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*hits++
			env.lastOID = r.URL.Query().Get("origClientOrderId")
			w.Header().Set("Content-Type", "application/json")
			if *mode == "missing" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(missing))
				return
			}
			_, _ = w.Write([]byte(hitOrder))
		}
	}

	mux.HandleFunc("/fapi/v1/order", handle(&env.futHits, &env.futMode))
	mux.HandleFunc("/api/v3/order", handle(&env.spotHits, &env.spotMode))

	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)

	t.Setenv("BINANCE_FAPI_URL", env.srv.URL)
	t.Setenv("BINANCE_REST_URL", env.srv.URL+"/api/v3")
	return env
}

func TestQueryOrderByClientOIDFuturesHit(t *testing.T) {
	env := newClientOidTestEnv(t)
	b := newTestBinanceAdapter()

	q, err := b.QueryOrderByClientOID("BTCUSDT", "xtabc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !q.Found {
		t.Fatal("合约命中应 Found=true")
	}
	if q.ExchangeOrderID != "998877" {
		t.Errorf("exchange order id = %q, want 998877", q.ExchangeOrderID)
	}
	if q.Info.Status != "NEW" {
		t.Errorf("status = %q, want NEW", q.Info.Status)
	}
	if env.lastOID != "xtabc123" {
		t.Errorf("查询未带 origClientOrderId, got %q", env.lastOID)
	}
	if env.spotHits != 0 {
		t.Errorf("合约命中不应再查现货, spotHits=%d", env.spotHits)
	}
}

func TestQueryOrderByClientOIDSpotFallback(t *testing.T) {
	env := newClientOidTestEnv(t)
	env.futMode = "missing"
	env.spotMode = "hit"
	b := newTestBinanceAdapter()

	q, err := b.QueryOrderByClientOID("BTCUSDT", "xtdef456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !q.Found {
		t.Fatal("现货兜底命中应 Found=true")
	}
	if env.futHits != 1 || env.spotHits != 1 {
		t.Errorf("应先合约后现货: fut=%d spot=%d", env.futHits, env.spotHits)
	}
}

func TestQueryOrderByClientOIDNotFound(t *testing.T) {
	env := newClientOidTestEnv(t)
	env.futMode = "missing"
	env.spotMode = "missing"
	b := newTestBinanceAdapter()

	q, err := b.QueryOrderByClientOID("BTCUSDT", "xtnone789")
	if err != nil {
		t.Fatalf("订单不存在不应返回错误: %v", err)
	}
	if q.Found {
		t.Fatal("两边都不存在应 Found=false")
	}
}
