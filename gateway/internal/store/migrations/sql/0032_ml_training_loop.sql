-- 0032: ML 训练闭环（train → export → hotload → predict → drift → retrain）
-- 日期: 2026-09-25
--
-- 背景: retrainer 只有任务级 last_status 记账（0019），训练运行的关键产物
--   （样本数/特征数/指标/模型版本/训练后端）无处可查，闭环不可观测。
--   xt_ml_training_runs 记录每一次闭环训练的完整运行历史：
--     trigger_src   schedule | manual | drift（trigger 是 SQLite 关键字，避开）
--     trainer       python | python_fallback_sklearn | go_fallback
--     status        success | failed | skipped
--     model_version 模型内容版本（导出 JSON 的 hash 前缀，热加载对账用）
--     metrics_json  训练指标（rmse/r2/accuracy 等，JSON 对象）
--   与 xt_ml_retrain_runs 的分工：后者是任务级成败流水（连续失败计数来源），
--   本表是闭环级产物档案（前端训练历史/闭环状态 API 的数据源）。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_ml_training_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER DEFAULT 0,
    model_name TEXT NOT NULL,
    trigger_src TEXT DEFAULT '',
    trainer TEXT DEFAULT 'python',
    status TEXT NOT NULL,
    error TEXT DEFAULT '',
    symbol TEXT DEFAULT '',
    interval TEXT DEFAULT '',
    bars_loaded INTEGER DEFAULT 0,
    train_samples INTEGER DEFAULT 0,
    test_samples INTEGER DEFAULT 0,
    feature_count INTEGER DEFAULT 0,
    metrics_json TEXT DEFAULT '',
    model_version TEXT DEFAULT '',
    duration_ms INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ml_training_runs_model ON xt_ml_training_runs(model_name, created_at);
CREATE INDEX IF NOT EXISTS idx_ml_training_runs_job ON xt_ml_training_runs(job_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ml_training_runs_created ON xt_ml_training_runs(created_at);
