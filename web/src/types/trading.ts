export interface ProtectionStatus {
  global_blocked: boolean
  global_reason?: string
  global_resume_in?: string
  pair_blocks: Record<
    string,
    {
      reason: string
      resume_in: string
      permanent: boolean
    }
  >
}

export interface ProtectionConfigItem {
  name: string
  params: Record<string, unknown>
}

export interface ExecutorStatus {
  active_positions: number
  pending_signals: number
  today_executed: number
  today_pnl: number
  tp1_executed: number
  tp2_executed: number
  tp3_executed: number
  sl_triggered: number
  status: 'running' | 'stopped' | 'error'
  updated_at?: string
}

export interface ExecutorPosition {
  id: string
  symbol: string
  side: 'LONG' | 'SHORT'
  entry_price: number
  current_price: number
  quantity: number
  unrealized_pnl: number
  realized_pnl: number
  tp1_price?: number
  tp2_price?: number
  tp3_price?: number
  sl_price?: number
  tp1_hit: boolean
  tp2_hit: boolean
  tp3_hit: boolean
  status: 'open' | 'partial_closed' | 'closed'
  opened_at: string
  updated_at?: string
}

export interface ExecutionRecord {
  id: string
  signal_id: string
  bot_id?: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: 'entry' | 'tp1' | 'tp2' | 'tp3' | 'sl'
  price: number
  quantity: number
  pnl?: number
  pnl_pct?: number
  executed_at: string
  metadata?: Record<string, unknown>
}

export interface SignalSource {
  id: string
  name: string
  type: 'webhook' | 'api' | 'internal'
  webhook_url?: string
  enabled: boolean
  signal_count_today: number
  signal_count_total: number
  last_signal_at?: string
  created_at?: string
  /** 定价模型：free=免费 / fixed_monthly=固定月费 / profit_share=盈利分成 */
  fee_model?: string
  /** 费率百分比（profit_share 时有效） */
  fee_percent?: number
  tp_sl_config?: {
    tp1_pct: number
    tp2_pct: number
    tp3_pct: number
    sl_pct: number
    position_size_pct: number
  }
}

/** GET /executor/stats 统计数据（与后端契约对齐）。 */
export interface ExecutorPnlPoint {
  date: string
  pnl: number
  signals: number
}

export interface ExecutorSymbolStat {
  symbol: string
  success_rate: number
  pnl: number
  signals: number
}

export interface ExecutorStats {
  total_signals: number
  today_signals: number
  success_rate: number
  tp1_rate: number
  tp2_rate: number
  tp3_rate: number
  avg_signals_per_day: number
  total_pnl: number
  pnl_curve: ExecutorPnlPoint[]
  by_symbol: ExecutorSymbolStat[]
}

// 与后端对齐（P1 假展示修复）：网关无交易所账户级数据源，数值无真实值时为
// null，前端显示 "--"。wallet/margin/maintenance/unrealized 后端从未提供 → 移除。
export interface ContractStatus {
  leverage: number | null
  available_margin: number | null
  margin_ratio: number | null
  liquidation_price: number | null
  margin_mode: 'isolated' | 'cross'
}

export interface ContractParams {
  leverage: number
  direction: 'long' | 'short' | 'both'
  margin_mode: 'isolated' | 'cross'
  open_indicator: 'macd_golden' | 'macd_death' | 'ema_counter' | 'ema_follow' | 'none'
  indicator_timeframe: '5m' | '15m' | '30m' | '1h' | '4h' | '8h'
  enable_trend_following: boolean
  max_positions: number
  symbol?: string
}

// 与后端 contractMarginInfo 对齐（P1：合约页假展示数据修复）——网关无交易所
// 账户级数据源，数值字段无真实值时为 null，前端显示 "--" 绝不显示 0/编造值。
export interface ContractMarginInfo {
  leverage: number | null
  available_margin: number | null
  margin_ratio: number | null
  liquidation_price: number | null
  max_positions: number | null
  direction: string
  margin_mode: 'isolated' | 'cross'
}

export interface LiquidationPriceResult {
  symbol: string
  entry_price: number
  side: string
  leverage: number
  margin_mode: string
  liquidation_price: number
}

export interface TradingSafetyResponse {
  locked: boolean
  paper_mode: boolean
  reason?: string
  unlocked_at?: number
}
