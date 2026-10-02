-- 0053: Agent QQ 双向通道（channel 插件，官方开放平台 WebSocket 网关模式）
-- 绑定表：QQ open_id ↔ 平台用户，附带最近一次会话上下文（chat_type + chat_id，
-- cron 主动投递用；chat_type ∈ c2c/group/channel）
CREATE TABLE IF NOT EXISTS xt_agent_qq_links (
    user_id INTEGER PRIMARY KEY,
    open_id TEXT NOT NULL UNIQUE,
    chat_type TEXT NOT NULL DEFAULT 'c2c',
    chat_id TEXT NOT NULL DEFAULT '',
    conversation_id TEXT DEFAULT '',
    model TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);
-- 配对验证码：一次性，10 分钟有效
CREATE TABLE IF NOT EXISTS xt_agent_qq_pair_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
