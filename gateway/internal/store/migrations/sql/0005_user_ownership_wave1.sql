-- 0005: 第一波 IDOR 加固 —— agent token / 通知表补 user_id 列并回填
-- 日期: 2026-09-19
--
-- 背景: P0 安全项（C3 agent tokens、H7 notifications）。
-- agent_tokens / agent_audit_log / notifications 三张表没有 user_id 列，
-- handler 层无法做越权校验。语义与 0004 一致：
--   user_id = 0  系统/历史无属主资源；user_id > 0 属主用户。
-- 存量行统一归到最早创建的 admin 用户（单用户部署里唯一的用户就是 admin）。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE agent_tokens ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_audit_log ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

-- ── 回填：存量行归到最早创建的 admin ──
UPDATE agent_tokens SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE agent_audit_log SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE notifications SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;

-- ── 列表过滤索引 ──
CREATE INDEX IF NOT EXISTS idx_agent_tokens_user ON agent_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_agent_audit_user ON agent_audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id);
