-- 0026_cleanup_fake_portfolio_snapshots.sql
-- 一次性清理：2026-09-16 权益改真实数据之前，假 paper 种子账户（10 万 USDT）
-- 产生的权益快照会让历史权益曲线出现一次性陡降（真实币安账户当时仅 ~0.2U）。
-- 假数据特征：口径切换日（2026-09-17 00:00 +08 = 1789574400000 ms）之前
-- 且 total_equity >= 90000（该量级只可能来自 10 万假种子）。
-- xt_portfolio_snapshots 仅为权益曲线数据源，删除不影响订单/成交等事实表。
DELETE FROM xt_portfolio_snapshots
WHERE timestamp < 1789574400000 AND total_equity >= 90000;
