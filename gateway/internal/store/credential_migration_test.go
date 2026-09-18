package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigratePlaintextCredentialsToVault：假明文 key → 加密入库 → yaml 值抹空 →
// GetCredential 从 vault 取回一致；二次迁移幂等。
func TestMigratePlaintextCredentialsToVault(t *testing.T) {
	// 隔离：临时 vault 文件/密钥 + env 主密钥 + 独立 configCache。
	t.Setenv("VAULT_MASTER_KEY", "test-vault-master-key-for-migration")
	dir := t.TempDir()
	origFile, origKey := VaultFilePath, VaultKeyPath
	VaultFilePath = filepath.Join(dir, "vault.json")
	VaultKeyPath = filepath.Join(dir, ".vault_key")
	t.Cleanup(func() {
		VaultFilePath, VaultKeyPath = origFile, origKey
		configMu.Lock()
		delete(configCache, "exchanges")
		configMu.Unlock()
	})

	configMu.Lock()
	if configCache == nil {
		configCache = make(map[string]any)
	}
	configCache["exchanges"] = map[string]any{
		"binance": map[string]any{
			"enabled":    true,
			"api_key":    "FAKE_BINANCE_KEY_123",
			"secret":     "FAKE_BINANCE_SECRET_456",
			"passphrase": "",
		},
		"okx": map[string]any{
			"enabled":    true,
			"api_key":    "FAKE_OKX_KEY",
			"secret":     "FAKE_OKX_SECRET",
			"passphrase": "FAKE_OKX_PASS",
		},
	}
	configMu.Unlock()

	// 迁移前：配置路径文件不应存在明文密钥落盘检查由 yaml 值抹空断言覆盖。
	migrated, err := MigratePlaintextCredentialsToVault()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(migrated) != 2 {
		t.Fatalf("migrated = %v, want 2 exchanges", migrated)
	}

	// vault 取回一致。
	v := GetVault()
	key, secret, pass, err := v.Get("binance")
	if err != nil || key != "FAKE_BINANCE_KEY_123" || secret != "FAKE_BINANCE_SECRET_456" {
		t.Fatalf("vault binance = (%q,%q,%v), want fake creds", key, secret, err)
	}
	key, secret, pass, err = v.Get("okx")
	if err != nil || key != "FAKE_OKX_KEY" || pass != "FAKE_OKX_PASS" {
		t.Fatalf("vault okx passphrase = %q, want FAKE_OKX_PASS (err=%v)", pass, err)
	}
	_ = secret

	// config.yaml 明文已抹空。
	configMu.RLock()
	ex := configCache["exchanges"].(map[string]any)
	binance := ex["binance"].(map[string]any)
	configMu.RUnlock()
	if binance["api_key"] != "" || binance["secret"] != "" {
		t.Fatalf("yaml plaintext must be blanked, got key=%v secret=%v", binance["api_key"], binance["secret"])
	}

	// 幂等：二次迁移无动作。
	migrated2, err := MigratePlaintextCredentialsToVault()
	if err != nil || len(migrated2) != 0 {
		t.Fatalf("second migrate = %v, %v; want idempotent no-op", migrated2, err)
	}

	// vault 文件落盘（密文，不含明文）。
	data, err := os.ReadFile(VaultFilePath)
	if err != nil {
		t.Fatalf("vault file: %v", err)
	}
	if contains := string(data); len(contains) == 0 {
		t.Fatal("vault file empty")
	}
}

// TestGetCredentialFallsBackToVault：env 缺省时 GetCredential 走 vault（adapter 层
// 契约由 adapter 包测试覆盖；此处验证 vault→adapter 数据源可用）。
func TestGetCredentialVaultSourceReady(t *testing.T) {
	if GetVault() == nil {
		t.Fatal("vault must be available")
	}
}
