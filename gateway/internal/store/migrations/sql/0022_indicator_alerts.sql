-- 0022: 指标信号告警任务（对标 QuantDinger qd_indicator_signal_alerts）
-- 日期: 2026-09-20
--
-- xt_indicator_alerts 用户自定义指标条件告警：
--   name            任务名（通知标题）
--   symbol          交易对（Binance 格式，如 BTCUSDT）
--   interval        K 线周期（1m/5m/15m/30m/1h/4h/1d 等）
--   condition_expr  告警条件表达式（文本），语法见 gateway/internal/alertexpr 包文档：
--     裸标识符: open/high/low/close/volume（最新一根）
--     指标函数（可取尾数简写，如 rsi14/ema20）:
--       rsi(period=14)、ema(period=20)、sma(period=20)、atr(period=14)
--       macd(fast=12,slow=26,signal=9) / macd_signal(...) / macd_hist(...)
--       bb_upper(period=20,mult=2) / bb_mid(...) / bb_lower(...)
--       bb_width(period=20,mult=2)=(upper-lower)/mid、bb_pctb=(close-lower)/(upper-lower)
--     运算符: && || == != > >= < <= + - * / 与括号，支持一元负号
--     示例: "rsi14 < 30 && close > ema20"、"(bb_upper(20,2)-bb_lower(20,2))/bb_mid(20,2) < 0.05"
--   message         自定义通知附言（可空）
--   cooldown_minutes 命中冷却分钟数（默认 60，0=不冷却）
--   last_triggered_at 最近一次命中触发时间（毫秒，0=从未触发）
--   last_value      最近一次求值时的左操作数当前值
--   active          1=参与周期扫描 0=停用
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_indicator_alerts (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL DEFAULT '',
    symbol TEXT NOT NULL DEFAULT '',
    interval TEXT NOT NULL DEFAULT '1h',
    condition_expr TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    cooldown_minutes INTEGER NOT NULL DEFAULT 60,
    last_triggered_at INTEGER NOT NULL DEFAULT 0,
    last_value REAL NOT NULL DEFAULT 0,
    active INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_indicator_alerts_user ON xt_indicator_alerts(user_id, active, updated_at);

CREATE INDEX IF NOT EXISTS idx_indicator_alerts_scan ON xt_indicator_alerts(active, symbol, interval);
