package handler

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/arbitrage"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func TestGetExchangeCredentialsReadsUnifiedConfig(t *testing.T) {
	cfg := store.GetConfig()
	if cfg == nil {
		cfg = make(map[string]any)
	}
	cfg["exchanges"] = map[string]any{
		"binance": map[string]any{
			"api_key":    "key1",
			"secret":     "secret1",
			"passphrase": "pass1",
			"testnet":    true,
			"enabled":    true,
		},
	}
	store.SaveConfig(cfg)

	apiKey, secret, passphrase, testnet := getExchangeCredentials("binance")
	if apiKey != "key1" {
		t.Errorf("expected api_key key1, got %s", apiKey)
	}
	if secret != "secret1" {
		t.Errorf("expected secret secret1, got %s", secret)
	}
	if passphrase != "pass1" {
		t.Errorf("expected passphrase pass1, got %s", passphrase)
	}
	if !testnet {
		t.Error("expected testnet true")
	}

	// Also verify fallback through adapter.GetCredential (env vars may affect this).
	apiKey2, _, _, _ := getExchangeCredentials("BINANCE")
	if apiKey2 != "key1" {
		t.Errorf("expected case-insensitive lookup to find key1, got %s", apiKey2)
	}
}

func TestCreateArbitrageClientUnsupported(t *testing.T) {
	_, err := createArbitrageClient("unknown", "k", "s", "", false)
	if err == nil {
		t.Error("expected error for unsupported exchange")
	}
}

func TestCreateArbitrageClientSupported(t *testing.T) {
	for _, name := range []string{"binance", "okx", "mexc", "gateio", "bybit", "coinbase", "kraken", "bitget"} {
		client, err := createArbitrageClient(name, "k", "s", "p", false)
		if err != nil {
			t.Errorf("expected %s client to build, got error: %v", name, err)
			continue
		}
		if client == nil {
			t.Errorf("expected non-nil %s client", name)
		}
	}
}

func TestAutoRegisterExchangesRegistersEnabled(t *testing.T) {
	cfg := store.GetConfig()
	if cfg == nil {
		cfg = make(map[string]any)
	}
	cfg["exchanges"] = map[string]any{
		"binance": map[string]any{
			"api_key": "k",
			"secret":  "s",
			"enabled": true,
		},
		"okx": map[string]any{
			"api_key": "k",
			"secret":  "s",
			"enabled": false,
		},
		"mexc": map[string]any{
			"enabled": true,
		},
	}
	store.SaveConfig(cfg)

	engine := arbitrage.NewEngine(arbitrage.DefaultEngineConfig())
	autoRegisterExchanges(engine)

	if engine.GetClientCount() != 1 {
		t.Errorf("expected 1 registered client, got %d", engine.GetClientCount())
	}
}

func TestAdapterGetCredentialReadsUnifiedConfig(t *testing.T) {
	cfg := store.GetConfig()
	if cfg == nil {
		cfg = make(map[string]any)
	}
	cfg["exchanges"] = map[string]any{
		"bybit": map[string]any{
			"api_key": "bybit_key",
			"secret":  "bybit_secret",
		},
	}
	store.SaveConfig(cfg)

	apiKey, secret, _ := adapter.GetCredential("bybit")
	if apiKey != "bybit_key" {
		t.Errorf("expected bybit_key, got %s", apiKey)
	}
	if secret != "bybit_secret" {
		t.Errorf("expected bybit_secret, got %s", secret)
	}
}
