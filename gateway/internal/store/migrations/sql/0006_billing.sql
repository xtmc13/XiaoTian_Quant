-- 0006: Billing 支付计费闭环 —— 订单持久化 + 用户套餐/积分列
-- 日期: 2026-09-19
--
-- 背景: 对标 QuantDinger 补齐支付计费能力。原 billing 为内存 stub，
-- 本迁移新增 billing_orders 订单表（USDT 多链转账 + Stripe 统一入账），
-- 并为 xt_users 增加套餐/到期时间/积分三列用于订单 paid 后的发放。
--
-- 语义约定:
--   billing_orders.status: pending(已创建/待提交哈希) → confirming(链上可查未满足确认数)
--     → paid(已核验/已收款并发货) | failed(核验不通过, 可重新提交 tx_hash)
--     | expired(30 分钟未付款自动过期)
--   billing_orders.amount_usdt: 整数微单位 (1 USDT = 1_000_000)，避免浮点比较
--   xt_users.plan: 当前生效套餐 id ('' = 无)；vip_expires_at: 到期秒级时间戳，
--     -1 = 终身；credits: 积分余额，订单 paid 时按计划累加
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS billing_orders (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	plan_id TEXT NOT NULL,
	chain TEXT NOT NULL,
	address TEXT DEFAULT '',
	amount_usdt INTEGER NOT NULL,
	tx_hash TEXT DEFAULT '',
	status TEXT DEFAULT 'pending',
	fail_reason TEXT DEFAULT '',
	attempts INTEGER DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	confirmed_at INTEGER DEFAULT 0
);

-- ── 列表/核验轮询索引 ──
CREATE INDEX IF NOT EXISTS idx_billing_orders_user ON billing_orders(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_billing_orders_status ON billing_orders(status);
CREATE INDEX IF NOT EXISTS idx_billing_orders_tx ON billing_orders(tx_hash);

-- ── 用户套餐/积分（发放目标列）──
ALTER TABLE xt_users ADD COLUMN plan TEXT DEFAULT '';
ALTER TABLE xt_users ADD COLUMN vip_expires_at INTEGER DEFAULT 0;
ALTER TABLE xt_users ADD COLUMN credits INTEGER DEFAULT 0;
