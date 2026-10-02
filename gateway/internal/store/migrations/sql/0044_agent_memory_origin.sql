-- 0044: Agent 记忆 origin 列（学习闭环：manual 手动新增 | agent 助手主动记录 | auto 自动沉淀）
ALTER TABLE xt_agent_memories ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual';
