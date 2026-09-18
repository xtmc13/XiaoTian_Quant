package store

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

// MigratePlaintextCredentialsToVault 启动迁移（P0-4）：config.yaml exchanges.*
// 的明文 api_key/secret/passphrase（非空且非 enc: 密文）→ 加密存入保险库并持久化
// → config.yaml 对应值抹空（落盘文件不再含明文密钥）。幂等：已为空则跳过。
// 返回迁移的交易所名；日志只记名称，绝不记密钥内容。
func MigratePlaintextCredentialsToVault() ([]string, error) {
	configMu.Lock()
	defer configMu.Unlock()

	exchanges, _ := configCache["exchanges"].(map[string]any)
	if exchanges == nil {
		return nil, nil
	}

	type plainItem struct {
		name        string
		node        map[string]any
		key, secret string
		passphrase  string
	}
	var items []plainItem
	for name, v := range exchanges {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		key := getString(m, "api_key", "")
		secret := getString(m, "secret", "")
		pass := getString(m, "passphrase", "")
		if key == "" && secret == "" && pass == "" {
			continue
		}
		// 已密文（enc: 前缀）跳过——isEncrypted 由 store.go 提供。
		if (key != "" && isEncrypted(key)) || (secret != "" && isEncrypted(secret)) || (pass != "" && isEncrypted(pass)) {
			continue
		}
		items = append(items, plainItem{name: name, node: m, key: key, secret: secret, passphrase: pass})
	}
	if len(items) == 0 {
		return nil, nil
	}

	// 主密钥：env 优先；未设且需要加密真实凭证 → 本机随机密钥（0600 落盘）。
	if os.Getenv("VAULT_MASTER_KEY") == "" {
		if _, err := loadLocalVaultKey(); err != nil {
			if _, err := ensureLocalVaultKey(); err != nil {
				return nil, fmt.Errorf("vault local key: %w", err)
			}
			log.Printf("[vault] 使用本机随机保险库密钥，设置 VAULT_MASTER_KEY env 以便多机迁移")
		}
	}

	v := GetVault()
	migrated := make([]string, 0, len(items))
	for _, it := range items {
		if err := v.Store(it.name, it.name, it.key, it.secret, it.passphrase); err != nil {
			return migrated, fmt.Errorf("vault store %s: %w", it.name, err)
		}
		// 抹掉 config.yaml 明文（内存 + 随后落盘）。
		it.node["api_key"] = ""
		it.node["secret"] = ""
		it.node["passphrase"] = ""
		migrated = append(migrated, it.name)
	}

	if err := v.SaveToFile(VaultFilePath); err != nil {
		return migrated, fmt.Errorf("vault persist: %w", err)
	}
	encrypted := encryptConfigSecrets(configCache)
	data, err := yaml.Marshal(encrypted)
	if err != nil {
		return migrated, err
	}
	if err := writeConfigFile(data); err != nil {
		return migrated, err
	}
	log.Printf("[vault] 已迁移 %d 个交易所明文凭证进加密保险库并抹除 config.yaml 明文: %v", len(migrated), migrated)
	return migrated, nil
}
