export interface User {
  id: string
  username: string
  email?: string
  role: 'admin' | 'user'
}

export interface Position {
  id: string
  symbol: string
  side: 'LONG' | 'SHORT'
  leverage: number
  entry_price: number
  mark_price: number
  liquidation_price?: number
  margin: number
  unrealized_pnl: number
  quantity: number
  [key: string]: unknown
}

export interface Order {
  id: string
  symbol: string
  side: 'BUY' | 'SELL'
  type: 'LIMIT' | 'MARKET' | 'STOP_LIMIT'
  price: number
  quantity: number
  status: 'NEW' | 'FILLED' | 'CANCELLED' | 'PARTIALLY_FILLED'
  created_at: string
  [key: string]: unknown
}

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

export interface PortfolioSummary {
  total_equity: number
  total_pnl: number
  total_pnl_pct: number
  spot_balance: number
  futures_balance: number
  futures_unrealized_pnl: number
  futures_wallet_balance: number
  funding_balance: number
  earn_balance: number
  margin_used?: number
  available_balance?: number
  drawdown_pct?: number
  position_count?: number
  other_exchanges: Record<string, number>
  usd_cny_rate: number
  conversion_rate: number
  preferred_currency: string
  exchanges: {
    name: string
    exchange: string
    balance: number
    connected: boolean
    enabled: boolean
    configured: boolean
  }[]
}

export interface DashboardSummary {
  total_equity: number
  total_pnl: number
  equity_curve: { time: number; value: number }[]
  ai_agents: {
    name: string
    status: string
    detail: string
  }[]
  ai_logs: { time: string; message: string }[]
  calendar: Record<string, number>
  win_rate?: number
  profit_factor?: number
  max_drawdown?: number
  total_trades?: number
}

export interface WSTick {
  type: 'tick'
  symbol: string
  price: number
  ts: number
}

export interface WSOrderBook {
  type: 'orderbook'
  symbol: string
  bids: [number, number][]
  asks: [number, number][]
}

export type WSEvent = WSTick | WSOrderBook | { type: string; [key: string]: any }

/* ═══════════════════════════════════════════════════════════════════
   Extended types — added during type-safety refactor
   ═══════════════════════════════════════════════════════════════════ */

/* ── API Response envelope ── */
export interface ApiResponse<T> {
  success: boolean
  data?: T
  error?: {
    code: string
    message: string
    details?: unknown
  }
  meta?: {
    timestamp: number
    requestId: string
  }
}

/* ── Market ── */
export interface TickerSnapshot {
  symbol: string
  price: number
  change_24h?: number
  change_pct_24h?: number
  volume_24h?: number
  high_24h?: number
  low_24h?: number
}

export interface KlineBar {
  timestamp: number
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface OrderBook {
  symbol: string
  bids: [number, number][]
  asks: [number, number][]
  ts?: number
}

export interface Trade {
  id: string
  symbol: string
  price: number
  quantity: number
  side: 'buy' | 'sell'
  time: number
  [key: string]: unknown
}

/* ── Portfolio ── */
export interface Balance {
  asset: string
  free: number
  locked: number
  total: number
  [key: string]: unknown
}

export interface PortfolioPosition {
  symbol: string
  quantity: number
  avg_entry_price: number
  current_price?: number
  unrealized_pnl: number
  realized_pnl?: number
  side?: 'LONG' | 'SHORT'
  margin?: number
  liquidation_price?: number
  [key: string]: unknown
}

export interface EquitySnapshot {
  timestamp: number
  total_equity: number
  drawdown?: number
  [key: string]: unknown
}

export interface CalendarMonth {
  month_key: string
  year: number
  month: number
  days_in_month?: number
  first_weekday?: number
  days: Record<string, number>
  total: number
  win_days: number
  lose_days: number
}

/* ── Strategy / Bot ── */
export interface BotConfig {
  id: string
  name: string
  strategy_type: string
  symbol: string
  status: 'running' | 'paused' | 'stopped' | 'error'
  config: Record<string, unknown>
  created_at: number
  updated_at: number
  [key: string]: unknown
}

export interface BacktestResult {
  id: string
  strategy_id: string
  symbol: string
  start_date: string
  end_date: string
  initial_capital: number
  final_equity: number
  total_return_pct: number
  max_drawdown_pct: number
  sharpe_ratio?: number
  win_rate?: number
  profit_factor?: number
  total_trades: number
  trades: BacktestTrade[]
  equity_curve: { time: number; equity: number }[]
  report?: Record<string, unknown>
  params?: Record<string, unknown>
  [key: string]: unknown
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

/* ── AI / ML ── */
export interface AIModelAnalysis {
  model: string
  name: string
  sentiment: 'bullish' | 'bearish' | 'neutral'
  analysis: string
  content?: string
  [key: string]: unknown
}

export interface AIAnalysisResult {
  symbol: string
  consensus: 'bullish' | 'bearish' | 'neutral'
  analyses: AIModelAnalysis[]
}

export interface MLModelInfo {
  model_id: string
  model_type: string
  task_type: string
  trained_at: string
  metrics: Record<string, number>
  feature_count: number
}

export interface MLTrainResult {
  success: boolean
  model_id: string
  symbol: string
  bars_loaded: number
  features_generated: number
  train_samples: number
  test_samples: number
  metrics: Record<string, number>
  feature_names: string[]
  duration_ms: number
  error?: string
  feature_count?: number
  [key: string]: unknown
}

/* ── Risk Control ── */
export interface ProtectionStatus {
  global_blocked: boolean
  global_reason?: string
  global_resume_in?: string
  pair_blocks: Record<string, {
    reason: string
    resume_in: string
    permanent: boolean
  }>
}

export interface ProtectionConfigItem {
  name: string
  params: Record<string, unknown>
}

/* ── Hyperopt ── */
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

/* ── Indicator / Community ── */
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

/* ── Billing ── */
export interface BillingPlan {
  id: string
  name: string
  name_en: string
  price: number
  credits: number | string
  period_days: number
}

export interface ChainInfo {
  chain: string
  address: string
  memo: string
}

export interface BillingOrder {
  order_id: string
  status: string
  plan_id: string
  chain: string
  tx_hash?: string
  created_at: number
}

/* ── Notification ── */
export interface NotificationItem {
  id: number
  title: string
  message: string
  content?: string
  level?: string
  category?: string
  type: 'info' | 'success' | 'warning' | 'error'
  read: boolean
  created_at: number
  link?: string
}

/* ═══════════════════════════════════════════════════════════════════
   API-specific types — added during type-safety refactor
   ═══════════════════════════════════════════════════════════════════ */

/* ── Auth ── */
export interface AuthUser {
  id: number
  username: string
  role: string
  nickname: string
  email?: string
}

export interface AuthLoginResponse {
  access_token: string
  token_type: string
  user: AuthUser
}

/* ── User Profile ── */
export interface UserProfile {
  id: number
  username: string
  nickname: string
  email: string
  role: string
  is_active: number
  email_verified: number
  created_at: string
  credits: number
  is_vip: boolean
  referral_code: string
  referral_count: number
}

export interface NotificationSettings {
  channels: Record<string, boolean>
}

/* ── Strategy ── */
export interface StrategyItem {
  id: string
  name: string
  strategy_name?: string
  symbol?: string
  status: 'running' | 'stopped' | 'error' | 'paused'
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

/* ── Backtest ── */
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
}

/* ── AI ── */
export interface AISnapshot {
  symbol: string
  price: number
  change_24h?: number
  sentiment: 'bullish' | 'bearish' | 'neutral'
  indicators: Record<string, number>
  signals: { source: string; side: 'buy' | 'sell'; strength: number }[]
}

export interface AIGenerateRequest {
  prompt: string
  symbol?: string
  timeframe?: string
  strategy_type?: string
}

export interface AIGenerateResponse {
  success: boolean
  strategy_code?: string
  params?: Record<string, unknown>
  explanation?: string
  error?: string
}

export interface AIMultiAgentRequest {
  symbol: string
  timeframe?: string
  agents?: string[]
}

export interface AIMultiAgentResponse {
  consensus: 'bullish' | 'bearish' | 'neutral'
  confidence: number
  votes: { agent: string; sentiment: 'bullish' | 'bearish' | 'neutral'; reasoning: string }[]
}

export interface AIChatResponse {
  reply: string
  model?: string
  latency_ms?: number
  [key: string]: unknown
}

export interface AIQuickScan {
  symbols: { symbol: string; score: number; trend: 'up' | 'down' | 'sideways' }[]
  timestamp: number
}

export interface AIAutoTradeConfig {
  enabled: boolean
  strategy_type?: string
  symbol?: string
  max_position_size?: number
  risk_per_trade?: number
}

export interface AIModel {
  id: string
  name: string
  provider: string
  enabled: boolean
}

/* ── Agent ── */
export interface AgentToken {
  id: string
  name: string
  token: string
  scopes: string[]
  created_at: string
  expires_at?: string
  last_used?: string
  [key: string]: unknown
}

export interface AgentAIConfig {
  model: string
  temperature: number
  max_tokens: number
  system_prompt?: string
}

export interface AgentCCSwitchStatus {
  enabled: boolean
  mode: 'auto' | 'manual'
  current_model?: string
}

/* ── Config / Settings ── */
export interface RawConfig {
  [key: string]: unknown
}

export interface ExchangeTestResult {
  success: boolean
  message?: string
  status?: string
  detail?: string
  balance?: number
}

export interface ExchangeSaveResult {
  success: boolean
  id?: string
}

export interface AgentModel {
  id: string
  name: string
  provider: string
  enabled: boolean
}

export interface DefaultSettings {
  [key: string]: unknown
}

export interface UISettings {
  theme?: 'dark' | 'light'
  language?: string
  chart_style?: string
}

export interface ExchangeSettings {
  id: string
  name: string
  api_key?: string
  api_secret?: string
  secret?: string
  passphrase?: string
  testnet?: boolean
  enabled: boolean
  [key: string]: unknown
}

/* ── Health ── */
export interface HealthStatus {
  status: 'healthy' | 'degraded' | 'unhealthy'
  version?: string
  uptime?: number
}

export interface ComponentHealth {
  name: string
  status: 'healthy' | 'degraded' | 'unhealthy'
  latency_ms?: number
  message?: string
}

/* ── Strategy Community ── */
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

/* ── Notifications / Routes ── */
export interface NotifyRoute {
  id: string
  channel: string
  enabled: boolean
  events?: string[]
  config?: Record<string, unknown>
  [key: string]: unknown
}

/* ── Pairlist ── */
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

/* ── Advanced Orders ── */
export interface OCOOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  quantity: number
  price: number
  stop_price: number
  limit_price?: number
  status: string
  created_at: string
  [key: string]: unknown
}

export interface BracketOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  quantity: number
  entry_price: number
  take_profit_price: number
  stop_loss_price: number
  status: string
  created_at: string
}

export interface IcebergOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  total_quantity: number
  visible_quantity: number
  price: number
  filled_quantity: number
  slice_size?: number
  executed_quantity?: number
  status: string
  created_at: string
  [key: string]: unknown
}

export interface TrailingOrder {
  id: string
  symbol: string
  side: 'BUY' | 'SELL' | 'buy' | 'sell'
  quantity: number
  entry_price: number
  trailing_percent: number
  current_stop_price: number
  status: string
  created_at: string
}

/* ── Arbitrage ── */
export interface ArbitrageConfig {
  enabled: boolean
  min_spread_pct: number
  max_position_size: number
  exchanges: string[]
  symbols: string[]
  dry_run: boolean
}

export interface ArbitrageStatus {
  running: boolean
  started_at?: string
  last_scan_at?: string
  scan_count: number
  opportunity_count: number
  stats?: Record<string, unknown>
  [key: string]: unknown
}

export interface ArbitrageOpportunity {
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  spread_pct: number
  spread_abs?: number
  estimated_profit: number
  timestamp: number
  [key: string]: unknown
}

export interface ArbitragePosition {
  id: string
  symbol: string
  long_exchange: string
  short_exchange: string
  long_qty: number
  short_qty: number
  entry_spread: number
  current_pnl: number
  status: 'open' | 'closing' | 'closed'
  opened_at: string
  closed_at?: string
  [key: string]: unknown
}

export interface ArbitrageHistoryItem {
  id: string
  symbol: string
  buy_exchange: string
  sell_exchange: string
  buy_price: number
  sell_price: number
  quantity: number
  profit: number
  profit_pct: number
  executed_at: string
  [key: string]: unknown
}

export interface ArbitrageExchange {
  name: string
  enabled: boolean
  connected: boolean
  latency_ms?: number
  last_error?: string
}

/* ── Indicator API ── */
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

/* ── Community ── */
export type CommunityIndicatorItem = IndicatorItem

/* ── Admin ── */
export interface AdminUser {
  id: number
  username: string
  nickname?: string
  email: string
  role: string
  is_active?: number
  created_at: string
  [key: string]: unknown
}

export interface AdminStats {
  total_users: number
  active_users: number
  admin_count?: number
  user_count?: number
  total_orders?: number
  total_revenue?: number
  daily_active?: number
  monthly_active?: number
  system?: {
    goroutines?: number
    heap_alloc_mb?: number
    uptime_seconds?: number
    go_version?: string
  }
  trading?: {
    total_orders?: number
    pending_orders?: number
    total_trades?: number
    active_strategies?: number
  }
  [key: string]: unknown
}

export interface AdminAuditLog {
  id: number
  user_id?: number
  actor?: string
  action: string
  details?: string
  detail?: string
  created_at: number
  [key: string]: unknown
}

/* ── RL (Reinforcement Learning) ── */
export interface RLTrainConfig {
  model_id?: string
  algorithm: 'qlearning' | 'ppo' | 'a2c' | 'sac'
  n_actions?: number
  symbol: string
  interval: string
  lookback_days?: number
  episodes?: number
  learning_rate?: number
  discount?: number
  epsilon?: number
  window_size?: number
  initial_balance?: number
  commission?: number
  use_tensorboard?: boolean
}

export interface RLTrainResult {
  success: boolean
  model_id: string
  algorithm: string
  n_actions: number
  episodes: number
  final_balance: number
  total_pnl: number
  best_reward: number
  avg_reward_last_10: number
  q_table_size?: number
  episode_rewards: number[]
  tensorboard_url?: string
  duration_ms: number
  error?: string
}

export interface RLPredictResult {
  success: boolean
  model_id: string
  action: number
  action_name: string
  confidence: number
  position: number
}

export interface RLEvalResult {
  success: boolean
  model_id: string
  total_return_pct: number
  sharpe_ratio: number
  max_drawdown_pct: number
  win_rate: number
  trades: number
  avg_trade_return: number
  metrics: Record<string, unknown>
}

export interface RLModelInfo {
  model_id: string
  model_type: string
  task_type: string
  trained_at: string
  metrics: Record<string, number>
  feature_count: number
}

export interface RLJobProgress {
  current_episode: number
  total_episodes: number
  current_step: number
  total_steps: number
  best_reward: number
  current_balance: number
  epsilon?: number
  q_table_size?: number
  mean_reward?: number
  loss?: number
}

export interface RLJob {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  algorithm: string
  n_actions: number
  symbol: string
  interval: string
  config?: Record<string, unknown>
  result?: RLTrainResult
  error?: string
  progress?: RLJobProgress
  created_at: string
  started_at?: string
  completed_at?: string
  tensorboard_run_id?: string
}

export interface RLWorkerInfo {
  worker_id: string
  status: string
  current_job?: string
  last_seen: string
  pid: number
}

export interface RLWorkerStatus {
  workers: RLWorkerInfo[]
  queue_length: number
  redis_connected: boolean
}

/* ── TensorBoard ── */
export interface TensorBoardScalar {
  tag: string
  step: number
  wall_time: number
  value: number
}

export interface TensorBoardRun {
  run_id: string
  run_name: string
  model_type: string
  model_id: string
  started_at: string
  updated_at: string
  status: string
  tags: string[]
  scalars?: TensorBoardScalar[]
}

export interface TensorBoardSummary {
  runs: TensorBoardRun[]
  total_runs: number
}

export interface TensorBoardQueryResult {
  run_id: string
  scalars: Record<string, TensorBoardScalar[]>
}
