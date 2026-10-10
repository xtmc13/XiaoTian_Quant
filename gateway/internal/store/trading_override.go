package store

import "time"

// ── 实盘总闸运行时覆盖持久化（管理员解锁态重启后保留）──

const tradingLiveOverrideKey = "trading_live_override"

// GetTradingLiveOverride 读已保存的实盘总闸运行时覆盖（不存在返回空串）。
func GetTradingLiveOverride() string {
	if db == nil {
		return ""
	}
	var v string
	if err := db.QueryRow(`SELECT value FROM pairlist_settings WHERE key=?`, tradingLiveOverrideKey).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SaveTradingLiveOverride 保存实盘总闸运行时覆盖（upsert）；空串表示清除
// 覆盖，重启后回落环境变量/配置。
func SaveTradingLiveOverride(value string) error {
	if db == nil {
		return nil
	}
	_, err := db.Exec(`INSERT INTO pairlist_settings (key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		tradingLiveOverrideKey, value, time.Now().UnixMilli())
	return err
}
