-- 0052: Agent 钉钉双向通道（channel 插件，outgoing webhook 模式）
-- 绑定表：钉钉 staff_id ↔ 平台用户（回发走回调 sessionWebhook）
CREATE TABLE IF NOT EXISTS xt_agent_dingtalk_links (
    user_id INTEGER PRIMARY KEY,
    staff_id TEXT NOT NULL UNIQUE,
    conversation_id TEXT DEFAULT '',
    model TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);
-- 配对验证码：一次性，10 分钟有效
CREATE TABLE IF NOT EXISTS xt_agent_dingtalk_pair_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
