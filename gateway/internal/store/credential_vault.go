package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// CredentialVault provides Fernet-like encrypted storage for API keys.
// Uses AES-256-GCM with a key derived from a master secret.
type CredentialVault struct {
	mu         sync.RWMutex
	masterKey  []byte
	entries    map[string]*EncryptedCredential // key_alias -> encrypted data
}

// EncryptedCredential holds an encrypted API credential.
type EncryptedCredential struct {
	Alias       string `json:"alias"`
	Exchange    string `json:"exchange"`
	APIKey      string `json:"-"` // never serialized in plaintext
	APISecret   string `json:"-"` // never serialized in plaintext
	Passphrase  string `json:"-"` // optional
	Encrypted   string `json:"encrypted"` // base64(AES-GCM(key+secret+passphrase))
	CreatedAt   int64  `json:"created_at"`
}

var (
	vault     *CredentialVault
	vaultOnce sync.Once
)

// GetVault returns the global credential vault.
func GetVault() *CredentialVault {
	vaultOnce.Do(func() {
		vault = NewCredentialVault(os.Getenv("VAULT_MASTER_KEY"))
	})
	return vault
}

// NewCredentialVault creates a new credential vault.
func NewCredentialVault(masterKey string) *CredentialVault {
	key := deriveKey(masterKey)
	return &CredentialVault{
		masterKey: key,
		entries:   make(map[string]*EncryptedCredential),
	}
}

// deriveKey creates a 32-byte AES key from a master secret.
func deriveKey(secret string) []byte {
	if secret == "" {
		secret = "xiaotian-quant-default-vault-key" // default for dev
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
