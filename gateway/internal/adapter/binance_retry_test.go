package adapter

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinanceRetriesOn429ThenSucceeds(t *testing.T) {
	var hits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/fapi/v1/order", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if atomic.LoadInt32(&hits) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
			return
		}
		_, _ = w.Write([]byte(`{"orderId":1,"status":"NEW"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)

	b := newTestBinanceAdapter()
	b.retry.BaseDelay = time.Millisecond
	b.retry.MaxRetries = 3

	res, err := b.PlaceFuturesOrder("BTCUSDT", "BUY", "LIMIT", 60000, 0.001, 10, "LONG", "xt-1")
	if err != nil {
		t.Fatalf("should succeed after 429 retries: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("hits=%d, want 3", got)
	}
	if res["status"] != "NEW" {
		t.Fatalf("status=%v, want NEW", res["status"])
	}
}

func TestBinanceCheckClockOffset(t *testing.T) {
	serverMs := time.Now().UnixMilli() + 500
	mux := http.NewServeMux()
	mux.HandleFunc("/fapi/v1/time", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"serverTime":` + strconv.FormatInt(serverMs, 10) + `}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)

	b := newTestBinanceAdapter()
	off, exceeded, err := b.CheckClockOffset(DefaultClockSkewThresholdMs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if off < 400 || off > 600 {
		t.Fatalf("offset=%d, want ~500", off)
	}
	if exceeded {
		t.Fatal("500ms skew should not exceed 1000ms threshold")
	}
}
