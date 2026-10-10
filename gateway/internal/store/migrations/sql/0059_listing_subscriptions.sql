-- 0059: 市场条目订阅扣费链路（listing 订阅）
-- 日期: 2026-10-10
--
-- ai_bot_subscriptions 承载市场条目（xt_market_listings）的订阅关系：
--   listing_id   关联的市场条目 id（空串 = 实例直连旧订阅，语义不变）
--   expires_at   订阅到期时间（Unix 秒；0 = 无到期，free/profit_share 用）
-- status 列已存在（schema.go 建表带 DEFAULT 'active'），本迁移新增取值
-- pending_payment（月费待支付）与 expired（到期治理置停），均不改表约束。
-- 扣费链路：monthly 条目订阅先落 pending_payment + billing 订单
-- （purpose=market_subscription, ref_id=订阅 id），链上核验 paid 后单事务
-- 激活并顺延 30 天；Engine 小时级 tick 把过期的 active 订阅置 expired。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE ai_bot_subscriptions ADD COLUMN listing_id TEXT NOT NULL DEFAULT '';

ALTER TABLE ai_bot_subscriptions ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_aibot_sub_listing ON ai_bot_subscriptions(listing_id);
