-- 0037: Agent 技能（Hermes skills 能力，插件化注册）
CREATE TABLE IF NOT EXISTS xt_agent_skills (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,                  -- 技能名（同一用户内唯一），斜杠 /name 调用
    description TEXT DEFAULT '',         -- 一句话说明（注入目录，供 agent/用户选择）
    body TEXT NOT NULL,                  -- 技能正文：给 agent 执行的程序性指令
    usage_count INTEGER DEFAULT 0,
    source TEXT DEFAULT 'user',          -- user（手动/面板创建）| agent（对话中沉淀）
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(user_id, name)
);
CREATE INDEX IF NOT EXISTS idx_agent_skills_user ON xt_agent_skills(user_id, usage_count DESC);
