-- 0060: 市场订阅归因闭环（provider 建档回写 + 订阅实例化溯源）
-- 日期: 2026-10-10
--
-- 市场 MVP 第五阶段：订阅实例化 + 利润分成归因闭环。
--   xt_market_listings.provider_id   作者在 xt_social_providers 的建档 id
--                                    （approve 时无记录则自动建档并回写；0 = 未建档）
--   ai_bot_instances.origin_listing_id  订阅实例化克隆来源条目 id（'' = 非克隆）
--   grid_bots.origin_listing_id         同上
-- 归因链：克隆实例平仓盈利 → 按 origin_listing_id 找条目 → provider_id +
-- active profit_share 订阅 → xt_social_copy_pnl → 日窗结算 → T+3 → 提现。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE xt_market_listings ADD COLUMN provider_id INTEGER NOT NULL DEFAULT 0;

ALTER TABLE ai_bot_instances ADD COLUMN origin_listing_id TEXT NOT NULL DEFAULT '';

ALTER TABLE grid_bots ADD COLUMN origin_listing_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_aibot_inst_origin ON ai_bot_instances(origin_listing_id);

CREATE INDEX IF NOT EXISTS idx_grid_bots_origin ON grid_bots(origin_listing_id);
