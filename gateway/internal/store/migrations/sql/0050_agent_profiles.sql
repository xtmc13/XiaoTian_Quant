-- 0050: Agent 档案（profiles）：同一用户的多套长期知识空间（记忆/技能/看板按档案隔离，
-- profile_id=0 的行为全局行，任何档案下可见；会话与定时任务不做档案隔离——会话全局可见，
-- 档案隔离的是长期知识）
CREATE TABLE IF NOT EXISTS xt_agent_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL        -- unix 秒
);
CREATE INDEX IF NOT EXISTS idx_agent_profiles_user ON xt_agent_profiles(user_id);
CREATE TABLE IF NOT EXISTS xt_agent_profile_state (
    user_id INTEGER PRIMARY KEY,
    active_profile_id INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE xt_agent_memories ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE xt_agent_skills ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE xt_agent_kanban_cards ADD COLUMN profile_id INTEGER NOT NULL DEFAULT 0;
