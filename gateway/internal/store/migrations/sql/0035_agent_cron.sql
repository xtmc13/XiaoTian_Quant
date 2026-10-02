-- 0035: Agent 定时任务（Hermes cron 能力，插件化注册）
CREATE TABLE IF NOT EXISTS xt_agent_cron_jobs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    prompt TEXT NOT NULL,
    schedule TEXT NOT NULL,              -- 5 段 cron 表达式：分 时 日 月 周
    timezone TEXT DEFAULT 'Asia/Shanghai',
    channel TEXT DEFAULT 'web',          -- web | telegram | lark | dingtalk | email
    enabled INTEGER DEFAULT 1,
    next_run_at INTEGER NOT NULL,
    last_run_at INTEGER DEFAULT 0,
    last_status TEXT DEFAULT '',         -- ok | error
    last_result TEXT DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_cron_due ON xt_agent_cron_jobs(enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_agent_cron_user ON xt_agent_cron_jobs(user_id, updated_at);
