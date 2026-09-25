package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resetVault 清空包级保险库单例/Once/锁定密钥（测试内模拟"进程重启"）。
func resetVault() {
	vaultMu.Lock()
	vault = nil
	vaultMu.Unlock()
	vaultOnce = sync.Once{}
	resolvedVaultKey = ""
}

// isolateVault 把 vault 文件/密钥文件指向临时目录，并在测试结束后复位全局状态。
func isolateVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origFile, origKey := VaultFilePath, VaultKeyPath
	VaultFilePath = filepath.Join(dir, "vault.json")
	VaultKeyPath = filepath.Join(dir, ".vault_key")
	resetVault()
	t.Cleanup(func() {
		VaultFilePath, VaultKeyPath = origFile, origKey
		resetVault()
	})
	return dir
}

// TestVaultLocalKeyGeneratedThenReusedAcrossRestart：首启生成随机密钥（64 hex，
// 0600），"重启"后复用同一文件而不是重新随机——已存凭证重启不丢。
func TestVaultLocalKeyGeneratedThenReusedAcrossRestart(t *testing.T) {
	t.Setenv("VAULT_MASTER_KEY", "")
	t.Setenv("APP_ENV", "")
	isolateVault(t)

	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("first EnsureVaultReady: %v", err)
	}
	info, err := os.Stat(VaultKeyPath)
	if err != nil {
		t.Fatalf("key file must exist after first boot: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file perm = %o, want 0600", perm)
	}
	keyData, err := os.ReadFile(VaultKeyPath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	key1 := strings.TrimSpace(string(keyData))
	if len(key1) != 64 {
		t.Fatalf("generated key len = %d, want 64 hex chars", len(key1))
	}

	// 以首启密钥写入一条凭证并落盘。
	if err := GetVault().Store("okx", "okx", "K1", "S1", "P1"); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := GetVault().SaveToFile(VaultFilePath); err != nil {
		t.Fatalf("save vault: %v", err)
	}

	// 模拟重启：单例/锁定密钥全部清空，再次 EnsureVaultReady。
	resetVault()
	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("second EnsureVaultReady: %v", err)
	}
	keyData2, err := os.ReadFile(VaultKeyPath)
	if err != nil {
		t.Fatalf("read key file after restart: %v", err)
	}
	if strings.TrimSpace(string(keyData2)) != key1 {
		t.Fatal("key file must be reused across restarts, not regenerated")
	}
	k, s, p, err := GetVault().Get("okx")
	if err != nil || k != "K1" || s != "S1" || p != "P1" {
		t.Fatalf("post-restart decrypt = (%q,%q,%q,%v), want stored creds", k, s, p, err)
	}
}

// TestVaultEnvKeyTakesPriority：VAULT_MASTER_KEY 优先于本机密钥文件（多机迁移场景）。
func TestVaultEnvKeyTakesPriority(t *testing.T) {
	isolateVault(t)
	// 本机文件里放一把旧 key，env 必须胜出。
	if err := os.WriteFile(VaultKeyPath, []byte("local-file-key"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	t.Setenv("VAULT_MASTER_KEY", "env-master-key")

	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("EnsureVaultReady: %v", err)
	}
	if got := GetVault().keySource; got != "env-master-key" {
		t.Fatalf("vault keySource = %q, want env key", got)
	}
	if err := GetVault().Store("binance", "binance", "K", "S", ""); err != nil {
		t.Fatalf("store: %v", err)
	}
	// 用 env key 新实例可解密；用本机文件 key 的实例解不开。
	if err := GetVault().SaveToFile(VaultFilePath); err != nil {
		t.Fatalf("save: %v", err)
	}
	fresh := NewCredentialVault("env-master-key")
	if err := fresh.LoadFromFile(VaultFilePath); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if k, _, _, err := fresh.Get("binance"); err != nil || k != "K" {
		t.Fatalf("env-key instance must decrypt, got (%q,%v)", k, err)
	}
	wrong := NewCredentialVault("local-file-key")
	if err := wrong.LoadFromFile(VaultFilePath); err != nil {
		t.Fatalf("reload wrong-key: %v", err)
	}
	if _, _, _, err := wrong.Get("binance"); !errors.Is(err, ErrVaultDecrypt) {
		t.Fatalf("local-file key must fail with ErrVaultDecrypt, got %v", err)
	}
}

// TestVaultCorruptedKeyFile：密钥文件存在但为空/全空白 → 明确报错且绝不覆盖
// 原文件（静默重生成会孤儿化全部已加密凭证）。
func TestVaultCorruptedKeyFile(t *testing.T) {
	t.Setenv("VAULT_MASTER_KEY", "")
	t.Setenv("APP_ENV", "")
	isolateVault(t)

	for _, content := range []string{"", "  \n\t "} {
		if err := os.WriteFile(VaultKeyPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write corrupt key file: %v", err)
		}
		resetVault()
		err := EnsureVaultReady()
		if !errors.Is(err, ErrVaultKeyCorrupted) {
			t.Fatalf("corrupt content %q: err = %v, want ErrVaultKeyCorrupted", content, err)
		}
		data, _ := os.ReadFile(VaultKeyPath)
		if string(data) != content {
			t.Fatalf("corrupt key file must not be overwritten, got %q", string(data))
		}
	}
}

// TestVaultKeyFilePermSelfHeal：权限宽于 0600 的密钥文件在读取时自愈为 0600。
func TestVaultKeyFilePermSelfHeal(t *testing.T) {
	t.Setenv("VAULT_MASTER_KEY", "")
	t.Setenv("APP_ENV", "")
	isolateVault(t)

	if err := os.WriteFile(VaultKeyPath, []byte("loose-perm-key"), 0o644); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("EnsureVaultReady: %v", err)
	}
	info, err := os.Stat(VaultKeyPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("perm after self-heal = %o, want 0600", perm)
	}
}

// TestVaultDecryptErrorDistinctFromNotFound：解密失败（ErrVaultDecrypt）与
// 凭证未配置（not found）必须可区分。
func TestVaultDecryptErrorDistinctFromNotFound(t *testing.T) {
	isolateVault(t)

	va := NewCredentialVault("key-A")
	if err := va.Store("okx", "okx", "K", "S", "P"); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := va.SaveToFile(VaultFilePath); err != nil {
		t.Fatalf("save: %v", err)
	}

	vb := NewCredentialVault("key-B")
	if err := vb.LoadFromFile(VaultFilePath); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, _, _, err := vb.Get("okx"); !errors.Is(err, ErrVaultDecrypt) {
		t.Fatalf("wrong-key decrypt err = %v, want ErrVaultDecrypt", err)
	}
	if _, _, _, err := vb.Get("never-configured"); err == nil || errors.Is(err, ErrVaultDecrypt) {
		t.Fatalf("not-found err = %v, must be non-nil and NOT ErrVaultDecrypt", err)
	}
}

// TestMigrateVaultKeyToEnv：旧本机密钥文件加密的凭证，在设置 VAULT_MASTER_KEY
// 后被重加密到新 key；幂等；旧 key 文件保留（env 撤销时仍可回退）。
func TestMigrateVaultKeyToEnv(t *testing.T) {
	t.Setenv("VAULT_MASTER_KEY", "")
	t.Setenv("APP_ENV", "")
	isolateVault(t)

	// 首启：生成本机密钥并写入凭证（旧 key 加密）。
	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("first EnsureVaultReady: %v", err)
	}
	oldKey := GetVault().keySource
	if err := GetVault().Store("okx", "okx", "OKX_K", "OKX_S", "OKX_P"); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := GetVault().SaveToFile(VaultFilePath); err != nil {
		t.Fatalf("save: %v", err)
	}

	// 轮换：设置 env 新 key，模拟重启。
	t.Setenv("VAULT_MASTER_KEY", "new-env-master-key")
	resetVault()
	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("second EnsureVaultReady: %v", err)
	}
	n, err := MigrateVaultKeyToEnv()
	if err != nil || n != 1 {
		t.Fatalf("migrate = (%d, %v), want (1, nil)", n, err)
	}
	k, s, p, err := GetVault().Get("okx")
	if err != nil || k != "OKX_K" || s != "OKX_S" || p != "OKX_P" {
		t.Fatalf("post-rotation decrypt = (%q,%q,%q,%v)", k, s, p, err)
	}

	// 落盘密文只能被新 key 解开。
	fresh := NewCredentialVault("new-env-master-key")
	if err := fresh.LoadFromFile(VaultFilePath); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if k, _, _, err := fresh.Get("okx"); err != nil || k != "OKX_K" {
		t.Fatalf("new-key reload decrypt = (%q,%v)", k, err)
	}
	stale := NewCredentialVault(oldKey)
	if err := stale.LoadFromFile(VaultFilePath); err != nil {
		t.Fatalf("reload stale: %v", err)
	}
	if _, _, _, err := stale.Get("okx"); !errors.Is(err, ErrVaultDecrypt) {
		t.Fatalf("old key must no longer decrypt after rotation, got %v", err)
	}

	// 幂等：二次迁移 no-op。
	if n, err := MigrateVaultKeyToEnv(); err != nil || n != 0 {
		t.Fatalf("second migrate = (%d, %v), want (0, nil)", n, err)
	}
	// 旧 key 文件保留。
	if _, err := os.Stat(VaultKeyPath); err != nil {
		t.Fatalf("old key file must be kept for rollback: %v", err)
	}
}

// TestMigrateVaultKeyToEnvNoEnv：未设 env 时不做任何事。
func TestMigrateVaultKeyToEnvNoEnv(t *testing.T) {
	t.Setenv("VAULT_MASTER_KEY", "")
	isolateVault(t)
	if n, err := MigrateVaultKeyToEnv(); err != nil || n != 0 {
		t.Fatalf("no-env migrate = (%d, %v), want (0, nil)", n, err)
	}
}

// TestMigrateVaultKeyToEnvUndecryptable：新旧 key 都解不开的条目被明确汇总报告
// （区别于静默跳过），进程不中断。
func TestMigrateVaultKeyToEnvUndecryptable(t *testing.T) {
	t.Setenv("APP_ENV", "")
	isolateVault(t)

	// 用第三把 key 加密落盘（新旧 key 都不是它）。
	vx := NewCredentialVault("key-X")
	if err := vx.Store("binance", "binance", "K", "S", ""); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := vx.SaveToFile(VaultFilePath); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.WriteFile(VaultKeyPath, []byte("old-file-key-Z"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	t.Setenv("VAULT_MASTER_KEY", "new-env-key-Y")
	resetVault()
	if err := EnsureVaultReady(); err != nil {
		t.Fatalf("EnsureVaultReady: %v", err)
	}
	n, err := MigrateVaultKeyToEnv()
	if err == nil || n != 0 {
		t.Fatalf("migrate = (%d, %v), want (0, undecryptable report)", n, err)
	}
	if !strings.Contains(err.Error(), "binance") {
		t.Fatalf("error must name undecryptable aliases, got: %v", err)
	}
}
