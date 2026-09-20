-- 0020: Affiliate 推荐码体系（对标 CryptoRobotics 推荐/促销码）
-- 日期: 2026-09-20
--
-- 背景: 补齐老用户推荐新用户的裂变闭环。新用户注册时可携带推荐码，
-- 注册成功记录推荐关系（xt_users.referred_by）；被推荐用户任意支付订单
-- （USDT 链上核验通过 / Stripe webhook 成功）置 paid 时，按推荐人
-- 推荐码的 commission_pct 给推荐人发放积分（xt_referral_earnings
-- order_id 唯一约束保证幂等，重复触发不重复发放）。
--
-- 语义约定:
--   xt_users.referred_by: 推荐人 user_id，NULL = 无推荐人（历史数据保持 NULL）
--   xt_referral_codes.code: 'XT' + 8 位大写字母数字，全局唯一；active=0 停用
--   xt_referral_codes.commission_pct: 0-100，默认 10；API 层限制 0-50 防滥用
--   xt_referral_earnings.status: credited(已入账) | reversed(已冲正，预留)
--   佣金口径: 积分，1 积分 = $0.01（与 Stripe 美分对齐），
--     credits = 订单微单位金额 / 10000 * pct / 100（先转美分再按比例，向下取整）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

-- ── 推荐关系（加列方式对齐 0006 对 xt_users 的 ALTER）──
ALTER TABLE xt_users ADD COLUMN referred_by INTEGER;

-- ── 推荐码表（一人一个码）──
CREATE TABLE IF NOT EXISTS xt_referral_codes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL UNIQUE,
	code TEXT NOT NULL UNIQUE,
	commission_pct INTEGER NOT NULL DEFAULT 10,
	active INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_referral_codes_user ON xt_referral_codes(user_id);

-- ── 佣金入账记录（order_id 唯一 = 幂等键）──
CREATE TABLE IF NOT EXISTS xt_referral_earnings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	referrer_user_id INTEGER NOT NULL,
	referred_user_id INTEGER NOT NULL,
	order_id TEXT NOT NULL UNIQUE,
	order_amount INTEGER NOT NULL,
	credits INTEGER NOT NULL,
	status TEXT NOT NULL DEFAULT 'credited',
	created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_referral_earnings_referrer ON xt_referral_earnings(referrer_user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_referral_earnings_referred ON xt_referral_earnings(referred_user_id);
