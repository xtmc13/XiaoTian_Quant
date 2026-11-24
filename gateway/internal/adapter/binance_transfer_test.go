package adapter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTransferAssetUsesUniversalTransferEndpoint pins the contract the
// account handler relies on: a signed POST to /sapi/v1/asset/transfer with
// the wallet-pair type, and a tranId being returned.
func TestTransferAssetUsesUniversalTransferEndpoint(t *testing.T) {
	var gotPath, gotType, gotAsset, gotAmount, gotKey, gotSignature string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = r.ParseForm()
		gotPath = r.URL.Path
		gotType = r.Form.Get("type")
		gotAsset = r.Form.Get("asset")
		gotAmount = r.Form.Get("amount")
		gotKey = r.Header.Get("X-MBX-APIKEY")
		gotSignature = r.Form.Get("signature")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tranId":123456,"status":"OK"}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_SAPI_REST_URL", srv.URL)

	b := NewBinanceAdapter("test-key", "test-secret", false)
	tranID, err := b.TransferAsset("spot", "futures", "btc", 0.5)
	if err != nil {
		t.Fatalf("TransferAsset: %v", err)
	}
	if tranID != 123456 {
		t.Fatalf("tranId = %d, want 123456", tranID)
	}
	if gotPath != "/sapi/v1/asset/transfer" {
		t.Errorf("path = %q", gotPath)
	}
	if gotType != "MAIN_UMFUTURE" {
		t.Errorf("type = %q, want MAIN_UMFUTURE", gotType)
	}
	if gotAsset != "BTC" {
		t.Errorf("asset = %q, want BTC", gotAsset)
	}
	if gotAmount != "0.5" {
		t.Errorf("amount = %q, want 0.5", gotAmount)
	}
	if gotKey != "test-key" {
		t.Errorf("X-MBX-APIKEY = %q", gotKey)
	}
	if gotSignature == "" {
		t.Error("signature missing")
	}
}

func TestTransferAssetRejectsUnknownRoute(t *testing.T) {
	b := NewBinanceAdapter("k", "s", false)
	_, err := b.TransferAsset("futures", "earn", "USDT", 10)
	if err == nil {
		t.Fatal("expected error for unsupported route")
	}
	if !strings.Contains(err.Error(), "unsupported binance transfer route") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTransferAssetSurfacesExchangeRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":-1100,"msg":"insufficient balance"}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_SAPI_REST_URL", srv.URL)

	b := NewBinanceAdapter("k", "s", false)
	_, err := b.TransferAsset("futures", "funding", "USDT", 100)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("error should carry exchange message, got %v", err)
	}
}

// A non-OK status must not be treated as success even when tranId is present.
func TestTransferAssetRejectsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tranId":9,"status":"FAIL"}`))
	}))
	defer srv.Close()
	t.Setenv("BINANCE_SAPI_REST_URL", srv.URL)

	b := NewBinanceAdapter("k", "s", false)
	if _, err := b.TransferAsset("futures", "funding", "USDT", 100); err == nil {
		t.Fatal("expected non-OK status to fail")
	}
}
