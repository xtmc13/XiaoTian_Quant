-- 0046: Agent 用户级 AI 覆盖（P3-B）
-- 每用户一行：provider 非空时插入解析链（请求覆盖之后、agent.ai.provider 之前），
-- api_key 非空时覆盖该 provider 的全局/env 凭证；api_key 永不回传前端。
CREATE TABLE IF NOT EXISTS xt_agent_user_ai (
    user_id INTEGER PRIMARY KEY,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    api_key TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
)
