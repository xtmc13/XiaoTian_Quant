-- 0049: Agent 本地文件工具检查点（write_file/patch 写入前备份，支持回滚）
CREATE TABLE IF NOT EXISTS xt_agent_file_checkpoints (
    id TEXT PRIMARY KEY,               -- cp_* 前缀
    user_id INTEGER NOT NULL,
    path TEXT NOT NULL,                -- 沙箱根内相对路径
    backup_path TEXT NOT NULL,         -- 备份文件绝对路径（<root>/.checkpoints/<cp_id>.bak）
    size INTEGER NOT NULL DEFAULT 0,   -- 备份时文件字节数
    conversation_id TEXT DEFAULT '',
    created_at INTEGER NOT NULL        -- unix 秒
);
CREATE INDEX IF NOT EXISTS idx_agent_file_cp_user ON xt_agent_file_checkpoints(user_id, created_at DESC);
