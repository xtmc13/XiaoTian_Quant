-- 0058: 市场条目通用上架对象（object_kind / object_id）
-- 日期: 2026-10-10
--
-- xt_market_listings 此前只关联 AI 机器人实例（bot_instance_id）。
-- 本迁移把上架对象泛化，让网格机器人（grid_bots）也能进入考核上架：
--   object_kind  ai_bot（默认，存量语义不变）| grid_bot
--   object_id    上架对象 id（ai_bot=实例 id，grid_bot=grid_bots.id）
-- 存量行回填 object_id = bot_instance_id；考核统计与成交明细按 object_kind
-- 分派数据源（grid_bot_snapshots / grid_bot_trades）。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE xt_market_listings ADD COLUMN object_kind TEXT NOT NULL DEFAULT 'ai_bot';

ALTER TABLE xt_market_listings ADD COLUMN object_id TEXT NOT NULL DEFAULT '';

UPDATE xt_market_listings SET object_id = bot_instance_id WHERE object_kind = 'ai_bot' AND object_id = '';

CREATE INDEX IF NOT EXISTS idx_market_listings_object ON xt_market_listings(object_kind, object_id);
