package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func transferTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/account/transfer", Transfer)
	return r
}

func postTransfer(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/account/transfer", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	transferTestRouter().ServeHTTP(w, req)
	return w
}

// The handler must reach the exchange and report its transfer id; a success
// response without an exchange-confirmed movement is the bug being fixed.
func TestTransferCallsExchangeAndReturnsTranID(t *testing.T) {
	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotType = r.Form.Get("type")
		_, _ = w.Write([]byte(`{"tranId":777,"status":"OK"}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_SAPI_REST_URL", srv.URL)
	t.Setenv("BINANCE_API_KEY", "k")
	t.Setenv("BINANCE_API_SECRET", "s")

	w := postTransfer(t, `{"from":"spot","to":"futures","currency":"USDT","amount":25}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("success = %v", out["success"])
	}
	if out["tran_id"] != float64(777) {
		t.Fatalf("tran_id = %v", out["tran_id"])
	}
	if gotType != "MAIN_UMFUTURE" {
		t.Fatalf("exchange type = %q, want MAIN_UMFUTURE", gotType)
	}
}

func TestTransferRejectsUnsupportedExchange(t *testing.T) {
	w := postTransfer(t, `{"from":"spot","to":"futures","currency":"USDT","amount":10,"exchange":"okx"}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
}

func TestTransferRejectsUnsupportedWallet(t *testing.T) {
	w := postTransfer(t, `{"from":"spot","to":"earn","currency":"USDT","amount":10}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
}

// Buy/Swap were fabricated successes; they must now fail closed.
func TestBuyCryptoFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/buy", BuyCrypto)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/buy", bytes.NewBufferString(`{"currency":"BTC","amount":100}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
}

func TestSwapCurrencyFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/swap", SwapCurrency)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/swap", bytes.NewBufferString(`{"from_currency":"BTC","to_currency":"ETH","amount":0.1}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
}
