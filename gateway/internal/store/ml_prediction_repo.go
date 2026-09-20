package store

import (
	"database/sql"
	"errors"
	"sync"
	"time"
)

// ── ML Prediction Repository（预测落盘复用，加速回测/hyperopt）────────
//
// xt_ml_predictions 一张表的读写入口：
//   - (model_name, symbol, bar_time, features_hash) 唯一约束天然去重，
//     同一特征向量的重复预测只落一次
//   - created_at 上的保留策略由 ml.Retrainer 周期清理（默认 30 天）

type MLPredictionRepo struct{ mu sync.Mutex }

func NewMLPredictionRepo() *MLPredictionRepo { return &MLPredictionRepo{} }

// Get 查预测缓存命中。未命中返回 (0, false, nil)；db 未初始化按未命中处理。
func (r *MLPredictionRepo) Get(modelName, symbol string, barTimeMs int64, featuresHash string) (float64, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, false, nil
	}
	var v float64
	err := db.QueryRow(`SELECT prediction FROM xt_ml_predictions
		WHERE model_name=? AND symbol=? AND bar_time=? AND features_hash=?`,
		modelName, symbol, barTimeMs, featuresHash).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return v, true, nil
}

// Insert 落一条预测（INSERT OR IGNORE：并发/重复写不报错，靠唯一约束去重）。
func (r *MLPredictionRepo) Insert(modelName, symbol string, barTimeMs int64, featuresHash string, prediction float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return nil
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO xt_ml_predictions
		(job_id, model_name, symbol, bar_time, features_hash, prediction, created_at)
		VALUES (0,?,?,?,?,?,?)`,
		modelName, symbol, barTimeMs, featuresHash, prediction, time.Now().UnixMilli())
	return err
}

// DeleteOlderThan 清保留期外的预测，返回删除条数。
func (r *MLPredictionRepo) DeleteOlderThan(cutoffMs int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, nil
	}
	res, err := db.Exec(`DELETE FROM xt_ml_predictions WHERE created_at < ?`, cutoffMs)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteForModel 删某模型全部缓存（模型重训成功后失效旧预测）。
func (r *MLPredictionRepo) DeleteForModel(modelName string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, nil
	}
	res, err := db.Exec(`DELETE FROM xt_ml_predictions WHERE model_name = ?`, modelName)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Count 当前缓存条数（测试/状态观测用）。
func (r *MLPredictionRepo) Count() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if db == nil {
		return 0, nil
	}
	var n int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_ml_predictions`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
