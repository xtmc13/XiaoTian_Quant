-- 0048: Agent 看板任务卡（对标 hermes-agent kanban，插件化注册）
CREATE TABLE IF NOT EXISTS xt_agent_kanban_cards (
    id TEXT PRIMARY KEY,                  -- kb_* 前缀
    user_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    column TEXT NOT NULL DEFAULT 'todo' CHECK (column IN ('todo', 'doing', 'done')),
    assignee TEXT DEFAULT '',             -- user | agent（空 = 未指派）
    created_by TEXT DEFAULT 'user',       -- user（面板手动）| agent（助手工具创建）
    comment TEXT DEFAULT '',              -- 完成时的总结备注
    created_at INTEGER NOT NULL,          -- unix 秒
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_kanban_user ON xt_agent_kanban_cards(user_id, updated_at DESC);
