-- 0047: Telegram 模型覆盖持久化（P3-B）
-- /model <name> 写入本列（空 = 跟随网关默认）；重启后由绑定表恢复，headless 入站执行透传。
ALTER TABLE xt_agent_telegram_links ADD COLUMN model TEXT NOT NULL DEFAULT ''
