-- 0045: Telegram 平台投递 P2（会话移交 handoff）
-- 绑定表追加当前移交会话 id（空 = 未绑定会话，消息按无会话 headless 执行）
ALTER TABLE xt_agent_telegram_links ADD COLUMN conversation_id TEXT NOT NULL DEFAULT '';
-- 移交验证码：一次性，10 分钟有效（网页端生成 → TG /handoff <code> 消费）
CREATE TABLE IF NOT EXISTS xt_agent_handoff_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    conversation_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
