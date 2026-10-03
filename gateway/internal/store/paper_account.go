package store

import "time"

// ── 模拟盘账户状态持久化（开关 + 余额，重启后保留）──

const paperAccountKey = "paper_account_json"

// GetPaperAccountJSON 读已保存的模拟盘账户状态（不存在返回空串）。
func GetPaperAccountJSON() string {
	if db == nil {
		return ""
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM pairlist_settings WHERE key=?`, paperAccountKey).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SavePaperAccountJSON 保存模拟盘账户状态（upsert）。
func SavePaperAccountJSON(value string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`INSERT INTO pairlist_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		paperAccountKey, value, time.Now().UnixMilli())
	return err
}
