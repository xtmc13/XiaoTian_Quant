-- 0015: AI 策略复盘报告（对标 QuantDinger strategy_review）
-- 日期: 2026-09-20
--
-- xt_ai_review_reports 保存基于真实成交生成的 AI 复盘记录：
--   scope_type=strategy|bot 标识复盘对象，scope_id 为策略/机器人 id，
--   窗口 [period_start, period_end]（毫秒时间戳）；
--   trades_count/total_pnl/win_rate/max_drawdown 为窗口内成交汇总统计；
--   report_text 为 AI 生成正文（纯文本四段式：总体评价/亮点/问题/改进建议）；
--   model 记录生成所用模型，status=pending|done|failed，failed 时 error 记原因。
-- user_id 归属（多用户越权防护）。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_ai_review_reports (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    scope_type TEXT NOT NULL DEFAULT 'strategy',
    scope_id TEXT NOT NULL DEFAULT '',
    period_start INTEGER NOT NULL DEFAULT 0,
    period_end INTEGER NOT NULL DEFAULT 0,
    trades_count INTEGER NOT NULL DEFAULT 0,
    total_pnl REAL NOT NULL DEFAULT 0,
    win_rate REAL NOT NULL DEFAULT 0,
    max_drawdown REAL NOT NULL DEFAULT 0,
    report_text TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_review_user ON xt_ai_review_reports(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ai_review_scope ON xt_ai_review_reports(scope_type, scope_id, created_at);
