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

export interface AIRobotConfig {
  id?: string
  model: string
  confidence_threshold: number
  scan_interval_seconds: number
  market_filters: {
    min_volume_24h: number
    max_volatility: number
    trend_timeframe: string
    require_trend_alignment: boolean
    filter_whitelist_only: boolean
  }
  enabled: boolean
  created_at?: string
  updated_at?: string
}

export interface AIStatus {
  signals_today: number
  avg_confidence: number
  filter_rate: number
  win_rate: number
  model: string
  enabled: boolean
}

export interface AISignal {
  id: string
  symbol: string
  side: 'buy' | 'sell'
  confidence: number
  model: string
  indicators: Record<string, number>
  reason?: string
  filters?: string[]
  market_condition?: string
  timestamp: string
  executed: boolean
  created_at: string
}
