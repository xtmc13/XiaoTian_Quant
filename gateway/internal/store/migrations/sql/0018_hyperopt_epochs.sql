-- Hyperopt epoch 持久化：优化任务每一轮 trial 的结果落库，
-- 供 epochs 过滤器查询与“最优超参一键回写策略”使用。
-- 注意：语句内不要出现分号（分号是语句分隔符）。

CREATE TABLE IF NOT EXISTS xt_hyperopt_epochs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    job_id TEXT NOT NULL,
    strategy_id TEXT NOT NULL DEFAULT '',
    trial_id INTEGER NOT NULL DEFAULT 0,
    params TEXT NOT NULL DEFAULT '{}',
    metrics TEXT NOT NULL DEFAULT '{}',
    loss REAL,
    loss_name TEXT NOT NULL DEFAULT '',
    applied INTEGER NOT NULL DEFAULT 0,
    applied_at INTEGER,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_hyperopt_epochs_job ON xt_hyperopt_epochs(job_id, created_at);

CREATE INDEX IF NOT EXISTS idx_hyperopt_epochs_user ON xt_hyperopt_epochs(user_id, created_at);
