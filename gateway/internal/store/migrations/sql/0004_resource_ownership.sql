-- 0004: 资源级 ownership —— 用户资源表补 user_id 列并回填
-- 日期: 2026-09-19
--
-- 背景: 多用户化 P0 安全项（对标 QuantDinger position ownership）。
-- 单用户时代创建的表大多没有 user_id 列，handler 层无法做越权校验。
-- 本迁移为下列资源表补列；存量行统一归到最早创建的 admin 用户
-- （单用户部署里唯一的用户就是 admin，行为不变）。
--
-- 语义约定:
--   user_id = 0  表示"系统/历史无属主"资源: 全局引擎数据(webhook/系统下单)、
--                以及 notification_routes 里的系统级默认规则——
--                对所有人可见，仅 admin 可改。
--   user_id > 0  表示属主用户：仅属主与 admin 可访问。
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

-- ── 现货网格机器人子表（主表 grid_bots.user_id 已存在）──
ALTER TABLE grid_bot_trades ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE grid_bot_snapshots ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

-- ── 订单成交 / 持仓 / 组合快照 ──
ALTER TABLE trades ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE positions ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE xt_portfolio_snapshots ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

-- ── 回测记录 ──
ALTER TABLE xt_backtests ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

-- ── 套利任务（跨所 + 三角）──
ALTER TABLE arbitrage_trades ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE triangular_trades ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

-- ── 通知路由规则：0=系统规则(所有人可见, 仅 admin 可改)，新规则记属主 ──
ALTER TABLE notification_routes ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;

-- ── 回填：存量行归到最早创建的 admin（notification_routes 保持 0=系统）──
-- 网格机器人子表随主表属主。
UPDATE grid_bot_trades SET user_id = COALESCE((SELECT user_id FROM grid_bots WHERE grid_bots.id = grid_bot_trades.bot_id), user_id) WHERE user_id = 0;
UPDATE grid_bot_snapshots SET user_id = COALESCE((SELECT user_id FROM grid_bots WHERE grid_bots.id = grid_bot_snapshots.bot_id), user_id) WHERE user_id = 0;
UPDATE trades SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE positions SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE xt_portfolio_snapshots SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE xt_backtests SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE arbitrage_trades SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;
UPDATE triangular_trades SET user_id = COALESCE((SELECT MIN(id) FROM xt_users WHERE role='admin'), 0) WHERE user_id = 0;

-- ── 列表过滤索引 ──
CREATE INDEX IF NOT EXISTS idx_trades_user ON trades(user_id);
CREATE INDEX IF NOT EXISTS idx_positions_user ON positions(user_id);
CREATE INDEX IF NOT EXISTS idx_port_snap_user ON xt_portfolio_snapshots(user_id);
CREATE INDEX IF NOT EXISTS idx_backtests_user ON xt_backtests(user_id);
CREATE INDEX IF NOT EXISTS idx_arb_trades_user ON arbitrage_trades(user_id);
CREATE INDEX IF NOT EXISTS idx_triangular_trades_user ON triangular_trades(user_id);
CREATE INDEX IF NOT EXISTS idx_notify_routes_user ON notification_routes(user_id);
