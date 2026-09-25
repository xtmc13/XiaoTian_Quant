-- 0027: AI 交易决策门（对标 QuantDinger ai_decision_filter / JEV 决策门）
-- 日期: 2026-09-25
--
-- xt_ai_gate_decisions 入场单 AI 审批时间线（可审计）：
--   decision       approve | reject | abstain | bypassed_exit | skipped | fail_open
--   allowed        1=放行 0=拦截（reject 且置信度达阈值，或 abstain_action=block 时）
--   confidence     LLM 自评置信度 0-1（skipped/fail_open 为 0）
--   reasons_json   LLM 结构化理由数组
--   fail_open      1=本次为故障放行（provider 错误/超时/解析失败/未配置）
--   degrade_reason fail_open 的具体原因（不含密钥，错误串已截断）
--   request_hash   决策上下文 JSON 的 sha256（审计对账，不存完整 prompt）
--   context_json   截断后的决策上下文摘要（<=4KB，绝不含 API key）
--   order_id       关联的 OMS 订单 id（决策先于下单，放行后回写）
--   executed       1=订单最终成交（PlaceOrder 终态回写）
--
-- 注意: 语句内不要出现分号(字符串/注释里也不行)——迁移按分号切分执行。

CREATE TABLE IF NOT EXISTS xt_ai_gate_decisions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '',
    symbol TEXT NOT NULL DEFAULT '',
    side TEXT NOT NULL DEFAULT '',
    order_type TEXT NOT NULL DEFAULT '',
    market_type TEXT NOT NULL DEFAULT 'spot',
    position_side TEXT NOT NULL DEFAULT '',
    quantity REAL NOT NULL DEFAULT 0,
    ref_price REAL NOT NULL DEFAULT 0,
    notional REAL NOT NULL DEFAULT 0,
    decision TEXT NOT NULL DEFAULT '',
    allowed INTEGER NOT NULL DEFAULT 1,
    confidence REAL NOT NULL DEFAULT 0,
    reasons_json TEXT NOT NULL DEFAULT '[]',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    fail_open INTEGER NOT NULL DEFAULT 0,
    degrade_reason TEXT NOT NULL DEFAULT '',
    request_hash TEXT NOT NULL DEFAULT '',
    context_json TEXT NOT NULL DEFAULT '',
    order_id TEXT NOT NULL DEFAULT '',
    executed INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ai_gate_decisions_user ON xt_ai_gate_decisions(user_id, created_at);

CREATE INDEX IF NOT EXISTS idx_ai_gate_decisions_decision ON xt_ai_gate_decisions(decision, created_at);

CREATE INDEX IF NOT EXISTS idx_ai_gate_decisions_symbol ON xt_ai_gate_decisions(symbol, created_at);
