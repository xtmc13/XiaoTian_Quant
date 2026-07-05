package store

import (
	"strings"
	"testing"
)

func TestArbitrageTradeRepoNoDB(t *testing.T) {
	repo := NewArbitrageTradeRepo()
	// Without a database connection, operations may error but must not panic.
	_ = repo.Create(&ArbitrageTradeRecord{ID: "test", Symbol: "BTCUSDT"})
	_, _ = repo.GetByID("test")
	_, _ = repo.ListActive()
	_, _ = repo.ListHistory(10)
}

func TestBuildFilterIgnoresUnknownColumns(t *testing.T) {
	allowed := map[string]bool{"symbol": true, "status": true}
	filter := map[string]any{
		"symbol":                        "BTCUSDT",
		"status":                        "completed",
		"id OR '1'='1' --":             "injected",
		"symbol; DROP TABLE trades; --": "injected",
	}
	args, where := buildFilter(filter, allowed)

	if where == "" {
		t.Fatal("expected non-empty WHERE clause")
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d", len(args))
	}
	if strings.Contains(where, "DROP TABLE") || strings.Contains(where, "OR '1'='1'") {
		t.Errorf("WHERE clause contains injected SQL: %s", where)
	}
}

func TestConfigSecretEncryptionRoundTrip(t *testing.T) {
	t.Setenv("XIAOTIAN_CONFIG_KEY", "test-key-for-encryption")

	cfg := map[string]any{
		"exchanges": map[string]any{
			"binance": map[string]any{
				"api_key":    "my_api_key",
				"secret":     "my_secret",
				"passphrase": "my_passphrase",
				"enabled":    true,
			},
		},
	}

	encrypted := encryptConfigSecrets(cfg)
	exchanges := encrypted["exchanges"].(map[string]any)
	binance := exchanges["binance"].(map[string]any)

	if !isEncrypted(binance["api_key"].(string)) {
		t.Error("api_key was not encrypted")
	}
	if !isEncrypted(binance["secret"].(string)) {
		t.Error("secret was not encrypted")
	}
	if binance["enabled"].(bool) != true {
		t.Error("non-secret field was modified")
	}

	decrypted := decryptConfigSecrets(encrypted)
	exchanges = decrypted["exchanges"].(map[string]any)
	binance = exchanges["binance"].(map[string]any)

	if binance["api_key"].(string) != "my_api_key" {
		t.Errorf("api_key decryption failed: %s", binance["api_key"])
	}
	if binance["secret"].(string) != "my_secret" {
		t.Errorf("secret decryption failed: %s", binance["secret"])
	}
	if binance["passphrase"].(string) != "my_passphrase" {
		t.Errorf("passphrase decryption failed: %s", binance["passphrase"])
	}
}
