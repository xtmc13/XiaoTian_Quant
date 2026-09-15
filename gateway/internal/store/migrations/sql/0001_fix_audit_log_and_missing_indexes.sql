-- 0001: 修复 agent_audit_log 双套列定义冲突 + 补充缺失索引
-- 日期: 2026-09-15
--
-- 背景: agent_audit_log 表(V2)原有列为 token 审计用途(audit_repo.go),
-- 但 admin_helpers.go 的 AddAuditLog/GetAuditLog 使用 actor/action/detail/created_at
-- 四列, 导致后台审计日志写入/查询在运行期静默失败。
-- 两种用途共用一表: 补齐 admin 侧缺失的四列, 双方的写入/查询各自成立。

ALTER TABLE agent_audit_log ADD COLUMN actor TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_audit_log ADD COLUMN action TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_audit_log ADD COLUMN detail TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_audit_log ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_audit_actor ON agent_audit_log(actor);
CREATE INDEX IF NOT EXISTS idx_audit_created ON agent_audit_log(created_at);

-- 盘点发现的缺失索引（高频过滤列，原为全表扫描）
CREATE INDEX IF NOT EXISTS idx_vcode_expires ON xt_verification_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_aibot_sub_billing ON ai_bot_subscriptions(next_billing_at);
CREATE INDEX IF NOT EXISTS idx_aibot_trades_symbol ON ai_bot_trades(symbol);
CREATE INDEX IF NOT EXISTS idx_positions_opened ON positions(opened_at);
