-- 0057: executor 信号体系补用户归属（市场多用户化 P0 越权修复）
-- xt_signals / xt_signal_executions 此前没有用户列，统计端点全局聚合，
-- 泄漏所有用户的信号数与盈亏。
-- 归属规则：webhook 信号落库时取信号源属主（xt_signal_sources.owner_user_id），
-- 执行记录从父信号继承；历史数据与默认源信号保持 user_id=0（无属主）。

ALTER TABLE xt_signals ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE xt_signal_executions ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_signals_user ON xt_signals(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sigexec_user ON xt_signal_executions(user_id, created_at);
