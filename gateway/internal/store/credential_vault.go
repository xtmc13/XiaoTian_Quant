package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// CredentialVault provides Fernet-like encrypted storage for API keys.
// Uses AES-256-GCM with a key derived from a master secret.
type CredentialVault struct {
	mu        sync.RWMutex
	masterKey []byte
	keySource string                          // 原始主密钥（比对用，绝不落盘）
	entries   map[string]*EncryptedCredential // key_alias -> encrypted data
}

// EncryptedCredential holds an encrypted API credential.
type EncryptedCredential struct {
	Alias      string `json:"alias"`
	Exchange   string `json:"exchange"`
	APIKey     string `json:"-"`         // never serialized in plaintext
	APISecret  string `json:"-"`         // never serialized in plaintext
	Passphrase string `json:"-"`         // optional
	Encrypted  string `json:"encrypted"` // base64(AES-GCM(key+secret+passphrase))
	CreatedAt  int64  `json:"created_at"`
}

var (
	vault     *CredentialVault
	vaultOnce sync.Once
	vaultMu   sync.Mutex // 保护 vault 指针（EnsureVaultReady 可重建单例）
)

// 保险库文件与本机随机主密钥的落盘位置（包级变量，测试可覆盖）。
// 仅存 AES-GCM 密文/随机密钥，无明文凭证。
var (
	VaultFilePath = filepath.Join("runtime", "credentials_vault.json")
	VaultKeyPath  = filepath.Join("runtime", ".vault_key")
)

// GetVault returns the global credential vault.
// 主密钥解析顺序：VAULT_MASTER_KEY env → 本机随机密钥文件（runtime/.vault_key）
// → dev 兜底（仅测试/无任何真实凭证场景；加密真实凭证绝不使用兜底常量）。
// APP_ENV=production 未设主密钥时由 EnsureVaultReady 在启动期 fatal。
func GetVault() *CredentialVault {
	vaultOnce.Do(func() {
		vault = newVaultResolved()
	})
	vaultMu.Lock()
	defer vaultMu.Unlock()
	return vault
}

// newVaultResolved 按当前解析出的主密钥创建保险库并尽力加载文件。
func newVaultResolved() *CredentialVault {
	v := NewCredentialVault(resolveVaultMasterKey())
	if err := v.LoadFromFile(VaultFilePath); err != nil && !os.IsNotExist(err) {
		log.Printf("[vault] 加载保险库文件失败（可能是密钥变化）: %v", err)
	}
	return v
}

// resolvedVaultKey 是进程内唯一生效的保险库主密钥，由 EnsureVaultReady 在
// 启动早期解析并锁定（迁移与读取共用同一把钥匙——修复单例 dev 键加密、
// 文件随机键解密的不一致）。
var resolvedVaultKey string

// resolveVaultMasterKey：已锁定值 → env → 本机密钥文件 → dev 兜底。
func resolveVaultMasterKey() string {
	if resolvedVaultKey != "" {
		return resolvedVaultKey
	}
	if key := os.Getenv("VAULT_MASTER_KEY"); key != "" {
		return key
	}
	if key, err := loadLocalVaultKey(); err == nil && key != "" {
		return key
	}
	return "xiaotian-quant-default-vault-key" // dev fallback：禁止用于真实凭证加密
}

// loadLocalVaultKey 读取本机随机保险库密钥文件。
func loadLocalVaultKey() (string, error) {
	data, err := os.ReadFile(VaultKeyPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ensureLocalVaultKey 生成（或读取）本机随机主密钥，0600 权限落盘 runtime/.vault_key。
// 用于"未设 env 主密钥且需要加密真实凭证"的场景，warn 日志提示多机迁移需设 env。
func ensureLocalVaultKey() (string, error) {
	if key, err := loadLocalVaultKey(); err == nil && key != "" {
		return key, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(VaultKeyPath), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(VaultKeyPath, []byte(key), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

// EnsureVaultReady 启动期调用（与 SECRET_KEY 同级的 isFatalInitErr 处理）：
// APP_ENV=production 且未设 VAULT_MASTER_KEY → fatal；其余场景**先确保本机随机
// 密钥文件存在**（未设 env 时），再锁定进程唯一主密钥并创建保险库单例——保证
// 迁移加密与运行期读取使用同一把钥匙（顺序：键文件 → resolvedVaultKey → 单例）。
func EnsureVaultReady() error {
	if os.Getenv("VAULT_MASTER_KEY") == "" {
		if env := os.Getenv("APP_ENV"); env == "production" || env == "release" {
			return fmt.Errorf("VAULT_MASTER_KEY environment variable is required in production")
		}
		// 预生成本机随机密钥：单例与后续迁移共用（dev 兜底绝不加密真实凭证）。
		if _, err := ensureLocalVaultKey(); err != nil {
			return fmt.Errorf("vault local key: %w", err)
		}
		log.Printf("[vault] 使用本机随机保险库密钥，设置 VAULT_MASTER_KEY env 以便多机迁移")
	}
	key := os.Getenv("VAULT_MASTER_KEY")
	if key == "" {
		key, _ = loadLocalVaultKey()
	}
	resolvedVaultKey = key
	// 纠正提前创建的单例：密钥不一致（如早期以 dev 兜底键初始化）时按锁定
	// 密钥重建并重载文件——保证迁移加密与运行期读取同一把钥匙。
	vaultMu.Lock()
	if vault != nil && vault.keySource != resolvedVaultKey {
		log.Printf("[vault] 保险库单例主密钥与启动解析不一致，按启动密钥重建")
		vault = newVaultResolved()
	}
	vaultMu.Unlock()
	GetVault()
	return nil
}

// GetOrReload 先查内存；miss 时从 VaultFilePath 惰性重载一次再查——兜底
// "另一进程写 vault 文件"的场景（同进程迁移单例内必中，重载为 no-op）。
func (v *CredentialVault) GetOrReload(alias string) (apiKey, apiSecret, passphrase string, err error) {
	apiKey, apiSecret, passphrase, err = v.Get(alias)
	if err == nil {
		return
	}
	if lerr := v.LoadFromFile(VaultFilePath); lerr != nil {
		return
	}
	return v.Get(alias)
}

// ── 文件持久化 ──

// vaultFileEntry 是落盘格式：只存 AES-GCM 密文 blob（主密钥不落盘）。
type vaultFileEntry struct {
	Alias     string `json:"alias"`
	Exchange  string `json:"exchange"`
	Encrypted string `json:"encrypted"`
}

// SaveToFile 把保险库条目（密文）持久化到 path。
func (v *CredentialVault) SaveToFile(path string) error {
	v.mu.RLock()
	entries := make([]vaultFileEntry, 0, len(v.entries))
	for _, e := range v.entries {
		entries = append(entries, vaultFileEntry{Alias: e.Alias, Exchange: e.Exchange, Encrypted: e.Encrypted})
	}
	v.mu.RUnlock()
	data, err := json.MarshalIndent(map[string]any{"entries": entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadFromFile 从 path 恢复保险库条目（密文原样载入内存，Get 时解密）。
func (v *CredentialVault) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var file struct {
		Entries []vaultFileEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, e := range file.Entries {
		v.entries[e.Alias] = &EncryptedCredential{
			Alias:     e.Alias,
			Exchange:  e.Exchange,
			Encrypted: e.Encrypted,
		}
	}
	return nil
}

// NewCredentialVault creates a new credential vault.
func NewCredentialVault(masterKey string) *CredentialVault {
	key := deriveKey(masterKey)
	return &CredentialVault{
		masterKey: key,
		keySource: masterKey,
		entries:   make(map[string]*EncryptedCredential),
	}
}

// deriveKey creates a 32-byte AES key from a master secret.
func deriveKey(secret string) []byte {
	if secret == "" {
		secret = "changeme-please-set-vault-master-key"
	}
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// Store encrypts and stores an API credential.
func (v *CredentialVault) Store(alias, exchange, apiKey, apiSecret, passphrase string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	plaintext := fmt.Sprintf("%s|%s|%s", apiKey, apiSecret, passphrase)

	encrypted, err := v.encrypt([]byte(plaintext))
	if err != nil {
		return fmt.Errorf("vault encrypt: %w", err)
	}

	v.entries[alias] = &EncryptedCredential{
		Alias:     alias,
		Exchange:  exchange,
		Encrypted: encrypted,
	}
	return nil
}

// Get decrypts and returns an API credential.
func (v *CredentialVault) Get(alias string) (apiKey, apiSecret, passphrase string, err error) {
	v.mu.RLock()
	entry, ok := v.entries[alias]
	v.mu.RUnlock()

	if !ok {
		return "", "", "", fmt.Errorf("credential %s not found", alias)
	}

	plaintext, err := v.decrypt(entry.Encrypted)
	if err != nil {
		return "", "", "", fmt.Errorf("vault decrypt: %w", err)
	}

	parts := vaultSplitN(string(plaintext), "|", 3)
	if len(parts) >= 3 {
		return parts[0], parts[1], parts[2], nil
	}
	if len(parts) >= 2 {
		return parts[0], parts[1], "", nil
	}
	return "", "", "", fmt.Errorf("invalid credential format")
}

// Delete removes a credential from the vault.
func (v *CredentialVault) Delete(alias string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.entries, alias)
}

// List returns all stored credential aliases.
func (v *CredentialVault) List() []EncryptedCredential {
	v.mu.RLock()
	defer v.mu.RUnlock()
	result := make([]EncryptedCredential, 0, len(v.entries))
	for _, e := range v.entries {
		result = append(result, EncryptedCredential{
			Alias:    e.Alias,
			Exchange: e.Exchange,
		})
	}
	return result
}

// ── AES-256-GCM ────────────────────────────────────────────────

func (v *CredentialVault) encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(v.masterKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (v *CredentialVault) decrypt(encoded string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(v.masterKey)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return aesGCM.Open(nil, nonce, ciphertext, nil)
}

func vaultSplitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			result = append(result, s)
			return result
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func vaultIndexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ── JSON Export/Import ─────────────────────────────────────────

// ExportJSON exports all credentials as encrypted JSON.
func (v *CredentialVault) ExportJSON() ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return json.MarshalIndent(v.entries, "", "  ")
}
