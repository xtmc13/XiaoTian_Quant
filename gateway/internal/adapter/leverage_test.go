package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBinanceSetLeverageCallsFuturesEndpoint(t *testing.T) {
	var gotPath, gotSymbol, gotLeverage, gotSignature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotPath = r.URL.Path
		gotSymbol = r.Form.Get("symbol")
		gotLeverage = r.Form.Get("leverage")
		gotSignature = r.Form.Get("signature")
		_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","leverage":7,"maxNotionalValue":"1000000"}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)

	b := NewBinanceAdapter("k", "s", false)
	if err := b.SetLeverage("btcusdt", 7); err != nil {
		t.Fatalf("SetLeverage: %v", err)
	}
	if gotPath != "/fapi/v1/leverage" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotSymbol != "BTCUSDT" {
		t.Fatalf("symbol = %q", gotSymbol)
	}
	if gotLeverage != "7" {
		t.Fatalf("leverage = %q", gotLeverage)
	}
	if gotSignature == "" {
		t.Fatal("signature missing")
	}
}

func TestBinanceSetLeverageSurfacesBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":-4028,"msg":"Leverage is not valid"}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_FAPI_URL", srv.URL)

	b := NewBinanceAdapter("k", "s", false)
	err := b.SetLeverage("BTCUSDT", 100)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "Leverage is not valid") {
		t.Fatalf("error should carry exchange message, got %v", err)
	}
}

func TestBybitSetLeverageCallsPositionEndpoint(t *testing.T) {
	var gotPath, gotCategory, gotBuy, gotSell, gotSymbol string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotCategory, _ = body["category"].(string)
		gotBuy, _ = body["buyLeverage"].(string)
		gotSell, _ = body["sellLeverage"].(string)
		gotSymbol, _ = body["symbol"].(string)
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{}}`))
	}))
	defer srv.Close()
	t.Setenv("BYBIT_REST_URL", srv.URL)

	b := NewBybitAdapter("k", "s", false)
	if err := b.SetLeverage("ethusdt", 5); err != nil {
		t.Fatalf("SetLeverage: %v", err)
	}
	if gotPath != "/position/set-leverage" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotCategory != "linear" || gotBuy != "5" || gotSell != "5" || gotSymbol != "ETHUSDT" {
		t.Fatalf("body = category:%q buy:%q sell:%q symbol:%q", gotCategory, gotBuy, gotSell, gotSymbol)
	}
}

func TestSetLeverageRejectsOutOfRangeLocally(t *testing.T) {
	b := NewBinanceAdapter("k", "s", false)
	if err := b.SetLeverage("BTCUSDT", 0); err == nil {
		t.Fatal("expected range error")
	}
	bb := NewBybitAdapter("k", "s", false)
	if err := bb.SetLeverage("", 5); err == nil {
		t.Fatal("expected symbol error")
	}
}
