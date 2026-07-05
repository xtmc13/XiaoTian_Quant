export interface BotConfig {
  id: string
  name: string
  strategy_type: string
  symbol: string
  status: 'running' | 'paused' | 'stopped' | 'error'
  config: Record<string, unknown>
  created_at: number
  updated_at: number
}

export interface BacktestResult {
  id: string
  strategy_id: string
  symbol: string
  start_date: string
  end_date: string
  initial_balance: number
  final_equity: number
  total_return_pct: number
  max_drawdown_pct: number
  sharpe_ratio?: number
  sortino_ratio?: number
  calmar_ratio?: number
  win_rate?: number
  profit_factor?: number
  total_trades: number
  trades: BacktestTrade[]
  equity_curve: { time: number; equity: number }[]
  report: BacktestReport
  params?: Record<string, unknown>
  source?: string
}

export interface BacktestReport {
  initial_balance: number
  final_equity: number
  total_return_pct: number
  max_drawdown_pct: number
  sharpe_ratio: number
  sortino_ratio?: number
  calmar_ratio?: number
  win_rate_pct: number
  total_trades: number
  profit_factor: number
  recovery_factor?: number
  winning_trades?: number
  losing_trades?: number
  avg_win?: number
  avg_loss?: number
  best_trade?: number
  worst_trade?: number
  max_consec_wins?: number
  max_consec_loss?: number
  var_95?: number
  cvar_95?: number
  volatility?: number
  monthly_returns?: Record<string, number>
  yearly_returns?: Record<string, number>
}

export interface BacktestTrade {
  id: string
  symbol: string
  side: 'BUY' | 'SELL'
  entry_price: number
  exit_price: number
  quantity: number
  pnl: number
  pnl_pct: number
  entry_time: number
  exit_time: number
  [key: string]: unknown
}

export interface AIBotCatalogItem {
  id: string
  name: string
  description?: string
  strategy_type: string
  market_type: 'spot' | 'futures'
  risk_level: 'low' | 'medium' | 'high'
  fee_model: 'free' | 'monthly' | 'profit_share'
  fee_percent?: number
  monthly_fee?: number
  performance_json?: string
  config_json?: string
  is_builtin?: number
  is_active?: number
  created_at?: number
  updated_at?: number
}

export interface AIBotPerformance {
  avg_monthly_profit?: number
  total_profit?: number
  win_rate?: number
  sharpe?: number
  max_drawdown?: number
  [key: string]: unknown
}

export interface AIBotInstance {
  id: string
  user_id: number
  catalog_id?: string
  name: string
  strategy_type: string
  symbol: string
  market_type: 'spot' | 'futures'
  status: 'running' | 'stopped' | 'paused' | 'error'
  execution_mode: 'live' | 'paper' | 'signal'
  config_json?: string
  exchange_id?: string
  unrealized_pnl?: number
  realized_pnl?: number
  total_return_pct?: number
  max_drawdown_pct?: number
  sharpe_ratio?: number
  win_rate?: number
  total_trades?: number
  initial_balance?: number
  error_message?: string
  created_at?: number
  updated_at?: number
  started_at?: number
  stopped_at?: number
}

export interface AIBotSubscription {
  id: number
  user_id: number
  bot_instance_id: string
  fee_type: 'profit_share' | 'monthly'
  fee_percent?: number
  monthly_fee?: number
  next_billing_at?: number
  status: 'active' | 'cancelled' | 'expired'
  created_at?: number
}

export interface AIBotSnapshot {
  id: number
  bot_instance_id: string
  total_equity: number
  unrealized_pnl: number
  realized_pnl: number
  total_return_pct: number
  timestamp: number
}

export interface AIBotTrade {
  id: number
  bot_instance_id: string
  symbol: string
  side: string
  entry_price: number
  exit_price: number
  quantity: number
  pnl: number
  pnl_pct: number
  tp_price: number
  sl_price: number
  close_reason: string
  opened_at: number
  closed_at: number
}

export interface AIBotAnalytics {
  bot: AIBotInstance
  snapshots: AIBotSnapshot[]
}

export interface AIBotCreateRequest {
  catalog_id?: string
  name?: string
  strategy_type?: string
  symbol?: string
  market_type?: 'spot' | 'futures'
  execution_mode?: 'live' | 'paper' | 'signal'
  config_json?: string
  exchange_id?: string
  initial_balance?: number
  leverage?: number
  risk_level?: 'low' | 'medium' | 'high'
}
