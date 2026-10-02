-- 0038: Agent Telegram 双向通道（channel 插件）
-- 配对链接：telegram_chat_id ↔ 平台用户
CREATE TABLE IF NOT EXISTS xt_agent_telegram_links (
    user_id INTEGER PRIMARY KEY,
    telegram_chat_id INTEGER NOT NULL UNIQUE,
    telegram_username TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);
-- 配对验证码：一次性，10 分钟有效
CREATE TABLE IF NOT EXISTS xt_agent_telegram_pair_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
