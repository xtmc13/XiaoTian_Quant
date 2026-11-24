package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func leverageTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/contract/leverage", ContractLeverageSet)
	return r
}

func postLeverage(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/contract/leverage", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	leverageTestRouter().ServeHTTP(w, req)
	return w
}

func TestContractLeverageSetAppliesOnBinance(t *testing.T) {
	var gotPath, gotLeverage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotPath = r.URL.Path
		gotLeverage = r.Form.Get("leverage")
		_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","leverage":7}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)
	t.Setenv("BINANCE_API_KEY", "k")
	t.Setenv("BINANCE_API_SECRET", "s")

	w := postLeverage(t, `{"exchange":"binance","symbol":"BTCUSDT","leverage":7}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if gotPath != "/fapi/v1/leverage" || gotLeverage != "7" {
		t.Fatalf("exchange call path=%q leverage=%q", gotPath, gotLeverage)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["success"] != true || out["leverage"] != float64(7) {
		t.Fatalf("response = %s", w.Body.String())
	}
}

func TestContractLeverageSetRequiresSymbol(t *testing.T) {
	w := postLeverage(t, `{"leverage":10}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

func TestContractLeverageSetOKXIsNotFaked(t *testing.T) {
	w := postLeverage(t, `{"exchange":"okx","symbol":"BTC-USDT-SWAP","leverage":10}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", w.Code, w.Body.String())
	}
}

func TestContractLeverageSetRejectsFractionalLeverage(t *testing.T) {
	w := postLeverage(t, `{"symbol":"BTCUSDT","leverage":2.5}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}
