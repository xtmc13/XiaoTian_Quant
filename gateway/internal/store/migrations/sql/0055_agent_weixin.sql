-- 0055: Agent 微信通道（腾讯官方 iLink Bot API：QR 扫码登录 + 长轮询收消息）
-- 机器人登录态：单行表（id 固定为 1），bot_token/baseurl 来自 QR 确认响应，
-- get_updates_buf 为长轮询游标（持久化传递，缺失会重复收消息）
CREATE TABLE IF NOT EXISTS xt_agent_weixin_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    bot_token TEXT NOT NULL DEFAULT '',
    baseurl TEXT NOT NULL DEFAULT '',
    bot_id TEXT NOT NULL DEFAULT '',
    get_updates_buf TEXT NOT NULL DEFAULT '',
    logged_in_at INTEGER NOT NULL DEFAULT 0
);
-- 绑定表：微信用户 id（wxid，形如 xxx@im.wechat）↔ 平台用户
CREATE TABLE IF NOT EXISTS xt_agent_weixin_links (
    user_id INTEGER PRIMARY KEY,
    wxid TEXT NOT NULL UNIQUE,
    conversation_id TEXT DEFAULT '',
    model TEXT DEFAULT '',
    created_at INTEGER NOT NULL
);
-- 配对验证码：一次性，10 分钟有效
CREATE TABLE IF NOT EXISTS xt_agent_weixin_pair_codes (
    code TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER DEFAULT 0
);
