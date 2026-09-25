-- 0030: 阶梯智能单（Ladder Smart Orders，对标 CryptoRobotics 终端阶梯单）
-- 日期: 2026-09-25
--
-- xt_ladder_orders 持久化阶梯单整体状态（重启恢复用）：
--   id              阶梯单 ID（lad- 前缀）
--   user_id         归属用户
--   symbol          交易对
--   side            BUY（买入阶梯+目标卖出）或 SELL（卖出阶梯+目标买回）
--   exchange        paper / 交易所名
--   status          active|stopping|completed|cancelled|stopped|failed
--   spec_json       创建时的规格（entries/targets/stop_loss/breakeven/trailing_step）
--   state_json      运行时状态（各档 order_id/成交、当前 SL、均价等）
--   created_at / updated_at  毫秒时间戳
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_ladder_orders (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    symbol TEXT NOT NULL DEFAULT '',
    side TEXT NOT NULL DEFAULT 'BUY',
    exchange TEXT NOT NULL DEFAULT 'paper',
    status TEXT NOT NULL DEFAULT 'active',
    spec_json TEXT NOT NULL DEFAULT '{}',
    state_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ladder_orders_user ON xt_ladder_orders(user_id, status, updated_at);

CREATE INDEX IF NOT EXISTS idx_ladder_orders_status ON xt_ladder_orders(status);
