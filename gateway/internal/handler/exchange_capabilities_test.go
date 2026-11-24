package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetExchangesUsesRegistryInsteadOfHardcodedStubs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/config/exchanges", GetExchanges)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/config/exchanges", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var out map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if out["gate"]["status"] != "active" {
		t.Errorf("gate status = %v, want active", out["gate"]["status"])
	}
	if out["gate"]["futures"] != false {
		t.Errorf("gate futures = %v, want false (spot only)", out["gate"]["futures"])
	}
	if out["binance"]["certified"] != true || out["binance"]["transfer"] != true {
		t.Errorf("binance capabilities = %v", out["binance"])
	}
	if out["kucoin"]["status"] != "stub" {
		t.Errorf("kucoin status = %v, want stub", out["kucoin"]["status"])
	}
	if out["okx"]["leverage"] != false {
		t.Errorf("okx leverage = %v, want false (per-order lever)", out["okx"]["leverage"])
	}
}
