-- 0035: 补齐 indicator_revenue 表（作者收益分成落表）
-- 日期: 2026-10-01
--
-- 背景: community/service.go 的 RecordRevenue / GetAuthorRevenue /
--   GetAuthorRevenueDetails 引用本表，但 schema.go 的 migrateV7 只建了
--   indicator_codes / indicator_purchases / indicator_comments，
--   strategy_overfit 走 migrateV8，本表始终缺失，导致
--   GET /api/community/author/revenue 500（no such table）。
--
-- 语义约定:
--   seller_id 即作者（指标卖方）用户 ID，与 indicator_purchases.seller_id 一致
--   author_share / platform_share 由价格按社区配置抽成比例拆分（和为 price）
--   created_at 为 unix 秒
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS indicator_revenue (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	indicator_id INTEGER NOT NULL,
	buyer_id INTEGER NOT NULL,
	seller_id INTEGER NOT NULL,
	price REAL DEFAULT 0,
	author_share REAL DEFAULT 0,
	platform_share REAL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_revenue_seller ON indicator_revenue(seller_id);
CREATE INDEX IF NOT EXISTS idx_revenue_indicator ON indicator_revenue(indicator_id);
CREATE INDEX IF NOT EXISTS idx_revenue_created ON indicator_revenue(created_at);
