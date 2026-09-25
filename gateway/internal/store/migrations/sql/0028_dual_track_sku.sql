-- 0028: 双轨计费 SKU（对标 CryptoRobotics 订阅版/利润分成版双 SKU）+ 市场订阅订单
-- 日期: 2026-09-25
--
-- 背景：0017 的 fee_mode=hybrid 语义是"月费+分成同时收"，本迁移引入双轨定价模型：
-- 同一市场条目（信号/机器人 provider）可同时挂"订阅轨"与"分成轨"两个 SKU，
-- 用户订阅时二选一：
--   xt_social_providers.pricing_model:
--     ''            沿用 fee_mode 旧语义（monthly/profit_share/hybrid）
--     subscription  仅订阅轨（月费）
--     profit_share  仅分成轨（盈利抽成 10%-35%）
--     both          双轨并存，用户二选一
--   xt_social_subscriptions.track:
--     ''            旧数据（按 fee_mode 语义参与结算）
--     subscription  订阅轨：走 billing 订单扣费，track_expires_at 为到期毫秒时间戳
--     profit_share  分成轨：免费开通，按 provider 分成比例进日窗跑批结算
--   billing_orders.purpose / ref_id:
--     purpose='plan'（默认）原平台套餐订单；
--     purpose='market_subscription' 时 ref_id=provider id，
--     订单 paid 后发放动作 = 激活该用户对该 provider 的订阅轨（30 天/期顺延）。
--
-- 切轨规则（代码侧强制，不在本迁移）：
--   分成轨 → 订阅轨：随时（创建市场订阅订单，paid 后切换）
--   订阅轨 → 分成轨：仅 track_expires_at 到期后允许
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

ALTER TABLE xt_social_providers ADD COLUMN pricing_model TEXT NOT NULL DEFAULT '';

ALTER TABLE xt_social_subscriptions ADD COLUMN track TEXT NOT NULL DEFAULT '';
ALTER TABLE xt_social_subscriptions ADD COLUMN track_expires_at INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_social_subs_track ON xt_social_subscriptions(track, status);

ALTER TABLE billing_orders ADD COLUMN purpose TEXT NOT NULL DEFAULT 'plan';
ALTER TABLE billing_orders ADD COLUMN ref_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_billing_orders_purpose ON billing_orders(purpose, ref_id, status);
