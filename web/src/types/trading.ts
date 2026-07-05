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
  tp_sl_config?: {
    tp1_pct: number
    tp2_pct: number
    tp3_pct: number
    sl_pct: number
    position_size_pct: number
  }
}

export interface ContractStatus {
  leverage: number
  available_margin: number
  margin_ratio: number
  liquidation_price?: number
  wallet_balance: number
  margin_balance: number
  maintenance_margin: number
  unrealized_pnl: number
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

export interface ContractMarginInfo {
  wallet_balance: number
  available_balance: number
  margin_balance: number
  maintenance_margin: number
  unrealized_pnl: number
  realized_pnl_today: number
  liquidation_price?: number
  leverage: number
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
