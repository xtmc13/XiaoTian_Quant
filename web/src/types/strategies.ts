import type { BacktestResult } from './bots'

export interface StrategyConfig {
  id: string
  name: string
  category: 'contract' | 'spot' | 'grid' | 'freqtrade'
  strategy_type: string
  coin: string
  direction: 'long' | 'short' | 'dual'
  leverage: number
  status: 'draft' | 'running' | 'paused' | 'stopped'
  config_json: string
  created_at: number
  updated_at: number
}

export interface StrategyItem {
  id: string
  name: string
  strategy_name?: string
  symbol?: string
  status: 'running' | 'stopped' | 'error' | 'paused' | 'detecting'
  mode?: 'signal' | 'script' | 'bot'
  strategy_mode?: 'signal' | 'script' | 'bot'
  type?: string
  group_id?: string
  group_name?: string
  initial_capital?: number
  current_equity?: number
  total_pnl?: number
  total_pnl_percent?: number
  leverage?: number
  timeframe?: string
  trade_direction?: 'long' | 'short' | 'both' | 'dual'
  market_type?: 'swap' | 'spot'
  market_category?: string
  indicator_name?: string
  exchange_id?: string
  created_at?: string
  updated_at?: string
  order_count?: number
  first_order_amount?: number
  add_position_spread?: number
  add_position_callback?: number
  take_profit_ratio?: number
  profit_callback?: number
  take_profit_method?: string
  open_indicator?: string
  add_position_indicator?: string
  waterfall_protection?: number
  open_double?: boolean
  trend_indicator?: boolean
  trend_timeframe?: string
  follow_trend?: boolean
  burn_cut?: { enabled: boolean; dual_burn_start: number; global_burn_start: number } | boolean
  close_add_position?: boolean
  trade_count_mode?: 'single' | 'cycle'
  strategy_code?: string
  ai_generated?: boolean
  strategy_type?: string
  config_json?: string
  category?: 'contract' | 'spot' | 'grid' | 'freqtrade'
  coin?: string
  direction?: 'long' | 'short' | 'dual'
  execution_mode?: 'live' | 'paper' | 'signal'
  notification_config?: { channels: string[]; targets: Record<string, unknown> }
  trading_config?: Record<string, unknown>
  add_positions?: AddPositionItem[]
  moving_take_profit_tiers?: MovingTPTier[]
  stop_loss_type?: 'ratio' | 'amount' | 'price'
  open_indicator_period?: string
  add_position_indicator_period?: string
  reverse_take_profit_period?: string
  first_order_multiplier?: number
  burn_global_enabled?: boolean
  burn_global_threshold?: number
  burn_dual_enabled?: boolean
  burn_dual_threshold?: number
}

export interface StrategyTemplate {
  id: string
  name: string
  category: string
  description?: string
  default_config?: Record<string, unknown>
}

export interface StrategyParamDef {
  name: string
  type: 'int' | 'float' | 'string' | 'bool' | 'enum'
  default: unknown
  min?: number
  max?: number
  step?: number
  options?: string[]
  category?: string
  label?: string
  description?: string
}

export interface StrategyParamDefs {
  type: string
  params: StrategyParamDef[]
}

export interface StrategyRanking {
  strategy_id: string
  name: string
  score: number
  win_rate?: number
  profit_factor?: number
  sharpe?: number
  total_return?: number
  max_drawdown?: number
  pnl?: number
  symbol?: string
  trades?: number
  [key: string]: unknown
}

export interface StrategyLog {
  id: string
  strategy_id: string
  level: 'info' | 'warning' | 'error'
  message: string
  created_at: string
}

export interface StrategyGlobalConfig {
  [key: string]: unknown
}

export interface HyperoptJob {
  id: string
  strategy_type: string
  symbol: string
  interval: string
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  best_score: number
  best_params: Record<string, unknown>
  trials_completed: number
  total_trials: number
  created_at: number
  updated_at: number
  [key: string]: unknown
}

export interface HyperoptSpace {
  name: string
  type: string
  low?: number
  high?: number
  choices?: string[]
}

export interface BacktestRequest {
  strategy_id?: string
  symbol: string
  start_date?: string
  end_date?: string
  initial_capital?: number
  initial_balance?: Record<string, number>
  timeframe?: string
  interval?: string
  strategy_type?: string
  script_code?: string
  from?: string
  to?: string
  params?: Record<string, unknown>
  leverage?: number
  direction?: 'long' | 'short' | 'dual' | 'both'
  order_count?: number
  first_order_amount?: number
  add_position_spread?: number
  add_position_callback?: number
  take_profit_ratio?: number
  profit_callback?: number
  trade_count_mode?: 'single' | 'cycle'
  open_indicator?: string
  add_position_indicator?: string
  waterfall_protection?: number
  open_double?: boolean
  trend_indicator?: boolean
  trend_timeframe?: string
  take_profit_method?: string
  reverse_take_profit?: boolean
  reverse_stop_loss?: boolean
  follow_trend?: boolean
  follow_trend_max?: number
  burn_cut?: { enabled: boolean; dual_burn_start: number; global_burn_start: number } | boolean
  custom_reduce?: boolean
  online_order_limit?: number
  profit_protection?: boolean
  close_add_position?: boolean
  stop_loss_ratio?: number
  stop_loss_amount?: number
  stop_loss_price?: number
  first_order_price?: number
  add_positions?: AddPositionItem[]
  moving_take_profit_tiers?: MovingTPTier[]
  stop_loss_type?: 'ratio' | 'amount' | 'price'
  open_indicator_period?: string
  add_position_indicator_period?: string
  reverse_take_profit_period?: string
  first_order_multiplier?: number
  burn_global_enabled?: boolean
  burn_global_threshold?: number
  burn_dual_enabled?: boolean
  burn_dual_threshold?: number
}

export interface AddPositionItem {
  order: number
  multiplier: number
  spread: number
  callback: number
  ema?: boolean
}

export interface MovingTPTier {
  ratio: number
  drawback: number
}

export interface MartinConfig {
  id?: string
  name: string
  strategy_type: 'martin'
  first_order_amount: number
  order_count: number
  add_position_spread: number
  add_position_callback: number
  take_profit_ratio: number
  profit_callback: number
  double_first_order: boolean
  loop_type: 'single' | 'cycle'
  loop_count: number
  enable_add_position: boolean
  flash_crash_protection: number
  symbol?: string
  leverage?: number
  direction?: 'long' | 'short' | 'dual'
  created_at?: string
  updated_at?: string
}

export interface WallStreetConfig {
  id?: string
  name: string
  strategy_type: 'wallstreet'
  first_order_amount: number
  order_count: number
  add_position_spread: number
  add_position_callback: number
  take_profit_ratio: number
  profit_callback: number
  double_first_order: boolean
  loop_type: 'single' | 'cycle'
  loop_count: number
  enable_add_position: boolean
  flash_crash_protection: number
  symbol?: string
  leverage?: number
  direction?: 'long' | 'short' | 'dual'
  created_at?: string
  updated_at?: string
}

export type StrategyConfigUnion = MartinConfig | WallStreetConfig

export interface IndicatorItem {
  id: number
  name: string
  description?: string
  pricing_type: 'free' | 'paid'
  price: number
  vip_free?: boolean
  score?: number
  sample_size?: number
  total_return?: number
  sharpe?: number
  max_drawdown?: number
  applicable_symbols?: string[]
  applicable_timeframes?: string[]
  author: {
    username: string
    nickname?: string
    avatar?: string
  }
  purchase_count?: number
  avg_rating?: number
  view_count?: number
  created_at?: string
  is_purchased?: boolean
  is_own?: boolean
  review_status?: string
  status?: string
  revenue?: number
  rating_count?: number
  updated_at?: string
  [key: string]: unknown
}

export interface IndicatorComment {
  id: number
  rating: number
  content: string
  created_at: number
  user_nickname: string
}

export interface IndicatorDetail {
  id: number
  name: string
  description?: string
  code?: string
  symbol?: string
  interval?: string
  pricing_type: 'free' | 'paid'
  price: number
  score?: number
  total_return?: number
  sharpe?: number
  max_drawdown?: number
  win_rate?: number
  profit_factor?: number
  sample_size?: number
  applicable_symbols?: string[]
  applicable_timeframes?: string[]
  author_id: number
  author_name: string
  purchase_count: number
  avg_rating: number
  rating_count: number
  view_count: number
  created_at: number
  is_purchased: boolean
  is_own: boolean
  [key: string]: unknown
}

export interface IndicatorParseResult {
  success: boolean
  params?: Record<string, unknown>
  error?: string
}

export interface IndicatorValidateResult {
  success: boolean
  error?: string
  params?: Record<string, unknown>
  data?: Record<string, unknown>
  hints?: { severity: string; code: string; params: Record<string, unknown> }[]
}

export interface IndicatorRunResult {
  success: boolean
  result?: Record<string, unknown>
  data?: Record<string, unknown>
  best_params?: Record<string, unknown>
  error?: string
  [key: string]: unknown
}

export interface IndicatorAIGenerateResult {
  success: boolean
  code?: string
  error?: string
}

export interface IndicatorBacktestResult {
  success: boolean
  result?: Record<string, unknown>
  error?: string
  [key: string]: unknown
}

export type CommunityIndicatorItem = IndicatorItem

export interface KPIScore {
  total_score: number
  return_score: number
  sharpe_score: number
  stability_score: number
  popularity_score: number
  overfit_penalty: number
}

export interface OverfitResult {
  score: number
  risk_level: 'low' | 'medium' | 'high' | 'insufficient_data'
  in_sample_return: number
  out_sample_return: number
  return_ratio: number
  stability_score: number
}

export interface StrategyCommunityItem {
  id: number
  name: string
  description?: string
  author: string
  author_name?: string
  author_id: number
  rating: number
  rating_count: number
  download_count: number
  tags: string[]
  created_at: string
  updated_at?: string
  total_return?: number
  sharpe_ratio?: number
  max_drawdown?: number
  win_rate?: number
  total_trades?: number
  profit_factor?: number
  comment_count?: number
  view_count?: number
  kpi_score?: KPIScore
  overfit_risk?: OverfitResult
}

export interface StrategyCommunityDetail extends StrategyCommunityItem {
  code?: string
  params?: Record<string, unknown>
  backtest_result?: BacktestResult
  comments?: CommunityComment[]
}

export interface CommunityComment {
  id: number
  user: string
  rating: number
  content: string
  created_at: string
  [key: string]: unknown
}

export interface LeaderboardEntry {
  rank: number
  strategy_id: string
  name: string
  author: string
  total_return: number
  sharpe: number
  win_rate: number
  subscribers: number
  kpi_score?: KPIScore
  overfit_risk?: OverfitResult
  max_drawdown?: number
  download_count?: number
  comment_count?: number
  rating_count?: number
}

export interface PairlistWhitelist {
  whitelist: string[]
  exchange?: string
  quote_asset?: string
  generated_at: string
}

export interface PairlistConfig {
  producers: { name: string; params: Record<string, unknown> }[]
  filters: { name: string; params: Record<string, unknown> }[]
  [key: string]: unknown
}
