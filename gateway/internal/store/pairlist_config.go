package store

import "time"

// ── Pairlist 配置持久化（P1：重启后保留用户配置的 producer/filter 链）──

const pairlistConfigKey = "pairlist_config_json"

// GetPairlistConfigJSON 读已保存的 pairlist 配置（不存在返回空串）。
func GetPairlistConfigJSON() string {
	if db == nil {
		return ""
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM pairlist_settings WHERE key=?`, pairlistConfigKey).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SavePairlistConfigJSON 保存 pairlist 配置（upsert）。
func SavePairlistConfigJSON(value string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`INSERT INTO pairlist_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		pairlistConfigKey, value, time.Now().UnixMilli())
	return err
}
