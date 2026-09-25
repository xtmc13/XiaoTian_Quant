package store

import (
	"sync"
	"time"
)

// ── 外部数据源缓存 Repository ──
// 表结构见 migrations/sql/0031_dataprovider_cache.sql。
// 实现 dataprovider.CacheStore 接口（UpsertDataProviderCache / GetDataProviderCache），
// 由 main.go 在 store 就绪后注入 dataprovider.Service。

// DataProviderCacheRecord 是 xt_dataprovider_cache 的行记录。
type DataProviderCacheRecord struct {
	Source    string `json:"source"`
	CacheKey  string `json:"cache_key"`
	Payload   string `json:"payload"`
	FetchedAt int64  `json:"fetched_at"` // epoch 毫秒
	ExpiresAt int64  `json:"expires_at"` // epoch 毫秒
}

// DataProviderRepo provides typed kv access for xt_dataprovider_cache。
type DataProviderRepo struct{ mu sync.RWMutex }

func NewDataProviderRepo() *DataProviderRepo { return &DataProviderRepo{} }

// UpsertDataProviderCache 写入/覆盖某源缓存；时间为 epoch 毫秒（0 自动补当前）。
func (r *DataProviderRepo) UpsertDataProviderCache(source, cacheKey, payload string, fetchedAt, expiresAt int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cacheKey == "" {
		cacheKey = "default"
	}
	if fetchedAt == 0 {
		fetchedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(`INSERT INTO xt_dataprovider_cache (source, cache_key, payload, fetched_at, expires_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(source, cache_key) DO UPDATE SET payload=excluded.payload,
			fetched_at=excluded.fetched_at, expires_at=excluded.expires_at`,
		source, cacheKey, payload, fetchedAt, expiresAt)
	return err
}

// GetDataProviderCache 读取某源缓存；未命中时 err 非 nil（sql.ErrNoRows）。
func (r *DataProviderRepo) GetDataProviderCache(source, cacheKey string) (payload string, fetchedAt, expiresAt int64, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if cacheKey == "" {
		cacheKey = "default"
	}
	err = db.QueryRow(`SELECT payload, fetched_at, expires_at FROM xt_dataprovider_cache
		WHERE source = ? AND cache_key = ?`, source, cacheKey).Scan(&payload, &fetchedAt, &expiresAt)
	return payload, fetchedAt, expiresAt, err
}

// DeleteExpiredDataProviderCache 清理过期缓存（后台定期调用，防御表膨胀）。
func (r *DataProviderRepo) DeleteExpiredDataProviderCache() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := db.Exec(`DELETE FROM xt_dataprovider_cache WHERE expires_at < ?`, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
