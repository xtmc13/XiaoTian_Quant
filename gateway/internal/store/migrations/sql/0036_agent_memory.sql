-- 0036: Agent 记忆（Hermes memory 能力，插件化注册）
CREATE TABLE IF NOT EXISTS xt_agent_memories (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    scope TEXT DEFAULT 'user',           -- user（仅自己）| global（预留）
    kind TEXT DEFAULT 'fact',            -- fact | preference | observation | market_note
    content TEXT NOT NULL,
    source_conversation_id TEXT DEFAULT '',
    importance INTEGER DEFAULT 1,        -- 1-5，越大越重要
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_memory_user ON xt_agent_memories(user_id, updated_at DESC);
