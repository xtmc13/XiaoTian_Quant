-- 0024: 回测偏差检测任务（对标 freqtrade lookahead-analysis / recursive-analysis）
-- 日期: 2026-09-25
--
-- xt_analysis_jobs 回测可信度质检异步任务：
--   kind          lookahead（前视偏差变体回测）| recursive（递归/前缀稳定性）
--   status        running | completed | failed
--   strategy_type 原生回测策略类型（与 /backtest/run 同一枚举）
--   params_json   请求参数快照（symbol/interval/from/to/阈值等）
--   result_json   分析结论（LookaheadResult / RecursiveResult），完成时写入
--   error         失败原因
--   completed_at  完成时间（毫秒，0=未完成）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_analysis_jobs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    kind TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running',
    symbol TEXT NOT NULL DEFAULT '',
    interval TEXT NOT NULL DEFAULT '1h',
    strategy_type TEXT NOT NULL DEFAULT '',
    params_json TEXT NOT NULL DEFAULT '{}',
    result_json TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_analysis_jobs_user ON xt_analysis_jobs(user_id, updated_at);

CREATE INDEX IF NOT EXISTS idx_analysis_jobs_kind ON xt_analysis_jobs(kind, status);
