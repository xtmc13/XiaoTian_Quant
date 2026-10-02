-- 0051: Agent 飞书双向通道（channel 插件，webhook 模式）
-- 绑定表：飞书 open_id ↔ 平台用户（chat_id 为消息回发目标）
CREATE TABLE IF NOT EXISTS xt_agent_feishu_links (
    user_id INTEGER PRIMARY KEY,
    open_id TEXT NOT NULL UNIQUE,
    chat_id TEXT DEFAULT '',
    model TEXT DEFAULT '',
    conversation_id TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);
-- 配对验证码：一次性，10 分钟有效
CREATE TABLE IF NOT EXISTS xt_agent_feishu_pair_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
