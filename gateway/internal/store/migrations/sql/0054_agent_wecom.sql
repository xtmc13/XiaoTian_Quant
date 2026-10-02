-- 0054: Agent 企业微信双向通道（channel 插件，回调模式）
-- 绑定表：企业微信 staff_id（FromUserName）↔ 平台用户（回发走 cgi-bin/message/send 主动推送）
CREATE TABLE IF NOT EXISTS xt_agent_wecom_links (
    user_id INTEGER PRIMARY KEY,
    staff_id TEXT NOT NULL UNIQUE,
    conversation_id TEXT DEFAULT '',
    model TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);
-- 配对验证码：一次性，10 分钟有效
CREATE TABLE IF NOT EXISTS xt_agent_wecom_pair_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
