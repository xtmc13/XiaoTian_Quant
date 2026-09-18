package adapter_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/adapter"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// writeTempConfig 写一个含明文交易所凭证的临时 config.yaml 并指向 store。
func writeTempConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := []byte(`server:
    port: "9999"
exchanges:
    binance:
        enabled: true
        api_key: FAKE_BINANCE_KEY_123
        secret: FAKE_BINANCE_SECRET_456
        passphrase: ""
    okx:
        enabled: true
        api_key: FAKE_OKX_KEY
        secret: FAKE_OKX_SECRET
        passphrase: FAKE_OKX_PASS
`)
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	store.SetConfigPath(path)
	store.LoadConfig()
}

// TestMigrateThenGetCredentialSameProcess 回归测试（线上事故：迁移日志正常但
// 运行时 GetCredential 返回空——单例 dev 键加密 vs 文件随机键解密不一致）：
// 同一进程 EnsureVaultReady→迁移→adapter.GetCredential 必须直接取回一致值；
// 模拟重启按同一密钥文件重载也必须能解密。
func TestMigrateThenGetCredentialSameProcess(t *testing.T) {
	t.Setenv("VAULT_MASTER_KEY", "")
	dir := t.TempDir()
	writeTempConfig(t, dir)

	origFile, origKeyPath := store.VaultFilePath, store.VaultKeyPath
	store.VaultFilePath = filepath.Join(dir, "vault.json")
	store.VaultKeyPath = filepath.Join(dir, ".vault_key")
	t.Cleanup(func() { store.VaultFilePath, store.VaultKeyPath = origFile, origKeyPath })

	// 与 main.go 同序：EnsureVaultReady（锁定进程唯一密钥）→ 迁移。
	if err := store.EnsureVaultReady(); err != nil {
		t.Fatalf("EnsureVaultReady: %v", err)
	}
	migrated, err := store.MigratePlaintextCredentialsToVault()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(migrated) != 2 {
		t.Fatalf("migrated = %v, want [binance okx]", migrated)
	}

	// ① 同进程读取：GetCredential 必须取回（绝不返回空）。
	key, secret, pass := adapter.GetCredential("binance")
	if key != "FAKE_BINANCE_KEY_123" || secret != "FAKE_BINANCE_SECRET_456" || pass != "" {
		t.Fatalf("GetCredential(binance) = (%q,%q,%q), want fake creds", key, secret, pass)
	}
	key, secret, pass = adapter.GetCredential("okx")
	if key != "FAKE_OKX_KEY" || secret != "FAKE_OKX_SECRET" || pass != "FAKE_OKX_PASS" {
		t.Fatalf("GetCredential(okx) = (%q,%q,%q), want fake creds", key, secret, pass)
	}

	// ② 模拟重启：按密钥文件重新解析主密钥、新实例从文件重载 → 仍须解密成功。
	keyData, err := os.ReadFile(store.VaultKeyPath)
	if err != nil {
		t.Fatalf("read vault key file: %v", err)
	}
	restartVault := store.NewCredentialVault(strings.TrimSpace(string(keyData)))
	if err := restartVault.LoadFromFile(store.VaultFilePath); err != nil {
		t.Fatalf("restart reload: %v", err)
	}
	k, s, _, err := restartVault.Get("binance")
	if err != nil || k != "FAKE_BINANCE_KEY_123" || s != "FAKE_BINANCE_SECRET_456" {
		t.Fatalf("restart decrypt mismatch: (%q,%q,%v)", k, s, err)
	}
}
