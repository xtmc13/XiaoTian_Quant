import axios, { AxiosInstance, AxiosRequestConfig, AxiosError, type Method } from 'axios'
import { useToastStore } from '@/stores/toastStore'
import {
  type DashboardSummary,
  type PortfolioSummary,
  type PortfolioPosition,
  type EquitySnapshot,
  type CalendarMonth,
  type KlineBar,
  type OrderBook,
  type Trade,
  type TickerSnapshot,
  type Order,
  type ProtectionStatus,
  type ProtectionConfigItem,
  type HyperoptJob,
  type HyperoptSpace,
  type MLModelInfo,
  type MLTrainResult,
  type IndicatorItem,
  type IndicatorComment,
  type IndicatorDetail,
  type BillingPlan,
  type ChainInfo,
  type BillingOrder,
  type BacktestResult,
  type BotConfig,
  type NotificationItem,
  type StrategyItem,
  type StrategyLog,
  type StrategyTemplate,
  type StrategyParamDefs,
  type StrategyRanking,
  type StrategyGlobalConfig,
  type BacktestRequest,
  type AISnapshot,
  type AIGenerateRequest,
  type AIGenerateResponse,
  type AIMultiAgentRequest,
  type AIMultiAgentResponse,
  type AIChatResponse,
  type AIQuickScan,
  type AIAutoTradeConfig,
  type AIModel,
  type AIStatus,
  type AgentToken,
  type AgentAIConfig,
  type AgentCCSwitchStatus,
  type RawConfig,
  type ExchangeTestResult,
  type ExchangeSaveResult,
  type AgentModel,
  type DefaultSettings,
  type UISettings,
  type ExchangeSettings,
  type ExchangeConfiguredStatus,
  type StrategyCommunityItem,
  type StrategyCommunityDetail,
  type CommunityComment,
  type LeaderboardEntry,
  type OverfitResult,
  type NotifyRoute,
  type PairlistWhitelist,
  type PairlistConfig,
  type OCOOrder,
  type BracketOrder,
  type IcebergOrder,
  type ArbitrageConfig,
  type ArbitrageStatus,
  type ArbitrageOpportunity,
  type ArbitragePosition,
  type ArbitrageHistoryItem,
  type ArbitragePerformance,
  type ArbitrageExchange,
  type TriangularConfig,
  type TriangularOpportunity,
  type TriangularTrade,
  type TriangularPerformance,
  type IndicatorParseResult,
  type IndicatorValidateResult,
  type IndicatorRunResult,
  type IndicatorAIGenerateResult,
  type IndicatorBacktestResult,
  type AdminUser,
  type AdminStats,
  type AdminAuditLog,
  type AIAnalysisResult,
  type RLTrainResult,
  type RLPredictResult,
  type RLEvalResult,
  type RLModelInfo,
  type RLJob,
  type RLWorkerStatus,
  type TensorBoardSummary,
  type TensorBoardQueryResult,
  type TensorBoardRun,
  type MarketSnapshotResponse,
  type IndicesSnapshot,
  type SentimentSnapshot,
  type CalendarSnapshot,
  type MartinConfig,
  type WallStreetConfig,
  type ExecutorStatus,
  type ExecutorPosition,
  type ExecutionRecord,
  type SignalSource,
  type AIRobotConfig,
  type AISignal,
  type ContractParams,
  type ContractMarginInfo,
  type LiquidationPriceResult,
  type AIBotCatalogItem,
  type AIBotInstance,
  type AIBotSubscription,
  type AIBotAnalytics,
  type AIBotCreateRequest,
  type AIBotTrade,
} from '@/types'


// ── Timeout presets (XiaoTianQuant style) ──
const TIMEOUTS: Record<string, number> = {
  default: 30000,
  ai: 180000,
  backtest: 600000,
  analysis: 180000,
}

function getTimeout(url: string): number {
  if (url.includes('/ai/') || url.includes('/generate')) return TIMEOUTS.ai
  if (url.includes('/backtest')) return TIMEOUTS.backtest
  if (url.includes('/analysis')) return TIMEOUTS.analysis
  return TIMEOUTS.default
}

// ── Unified response envelope ──
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

class ApiError extends Error {
  status: number
  code: string
  constructor(message: string, status: number, code = 'UNKNOWN') {
    super(message)
    this.status = status
    this.code = code
    this.name = 'ApiError'
  }
}

// ── Retry config ──
const MAX_RETRIES = 2
const RETRY_DELAY = 1000

// ── Create axios instance ──
const axiosInstance: AxiosInstance = axios.create({
  baseURL: '/api',
  headers: { 'Content-Type': 'application/json' },
})

// ── Request interceptor ──
axiosInstance.interceptors.request.use((config) => {
  const token = localStorage.getItem('xt-token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
    config.headers['Access-Token'] = token
  }
  // i18n
  const appLang = localStorage.getItem('xt-lang') || navigator.language || 'zh-CN'
  config.headers['X-App-Lang'] = appLang
  // Auto timeout based on URL
  config.timeout = getTimeout(config.url || '')
  // Prevent cache
  if (config.method === 'get') {
    config.params = { ...config.params, _t: Date.now() }
  }
  // Retry counter
  config.headers['X-Retry-Count'] = config.headers['X-Retry-Count'] || '0'
  return config
})

// ── Response interceptor ──
let isRedirectingToLogin = false
axiosInstance.interceptors.response.use(
  (response) => {
    // If backend wraps with { success: true, data: ..., meta: ... }, unwrap it
    const data = response.data
    if (data && typeof data === 'object' && 'success' in data && 'data' in data && 'meta' in data) {
      response.data = data.data
    }
    return response
  },
  async (error: AxiosError) => {
    const config = error.config as AxiosRequestConfig & { __retryCount?: number }

    // Retry logic for network errors / 5xx (except 401/403)
    if (config && !config.__retryCount) {
      config.__retryCount = 0
    }
    if (
      config &&
      config.__retryCount! < MAX_RETRIES &&
      (!error.response || (error.response.status >= 500 && error.response.status !== 501))
    ) {
      config.__retryCount!++
      await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY * config.__retryCount!))
      return axiosInstance(config)
    }

    if (error.response) {
      const status = error.response.status
      const data = error.response.data as Record<string, unknown>

      // 401 Unauthorized → redirect to login (prevent loop)
      if (status === 401) {
        if (!isRedirectingToLogin) {
          isRedirectingToLogin = true
          localStorage.removeItem('xt-token')
          localStorage.removeItem('xt-auth')
          window.location.href = '/login'
          setTimeout(() => { isRedirectingToLogin = false }, 3000)
        }
        return Promise.reject(new ApiError('登录已过期，请重新登录', 401, 'UNAUTHORIZED'))
      }

      // 403 Forbidden → show backend message
      if (status === 403) {
        const msg = (data?.msg as string) || (data?.message as string) || '权限不足'
        // Show toast for 403
        try {
          useToastStore.getState().addToast({ type: 'warning', message: msg, duration: 5000 })
        } catch { /* ignore */ }
        return Promise.reject(new ApiError(msg, 403, 'FORBIDDEN'))
      }

      // 429 Rate limit
      if (status === 429) {
        const msg = '请求过于频繁，请稍后再试'
        try {
          useToastStore.getState().addToast({ type: 'warning', message: msg, duration: 6000 })
        } catch { /* ignore */ }
        return Promise.reject(new ApiError(msg, 429, 'RATE_LIMIT'))
      }

      // 5xx Server error
      if (status >= 500) {
        // Try wrapped error format first: { error: { message: ... } }
        const wrappedError = data?.error as Record<string, unknown> | undefined
        const msg = (wrappedError?.message as string) || (data?.message as string) || (data?.error as string) || '服务器错误，请稍后重试'
        try {
          useToastStore.getState().addToast({ type: 'error', message: msg, duration: 6000 })
        } catch { /* ignore */ }
        return Promise.reject(new ApiError(msg, status, (wrappedError?.code as string) || (data?.code as string) || 'SERVER_ERROR'))
      }

      // Backend wraps errors as { error: { code, message } }
      const wrappedError = data?.error as Record<string, unknown> | undefined
      const message = (wrappedError?.message as string) || (data?.message as string) || `请求失败 (${status})`
      const code = (wrappedError?.code as string) || (data?.code as string) || 'HTTP_ERROR'
      // Show toast for client errors (4xx except 401/403/429)
      if (status >= 400 && status !== 401 && status !== 403 && status !== 429) {
        try {
          useToastStore.getState().addToast({ type: 'error', message, duration: 5000 })
        } catch { /* ignore */ }
      }
      return Promise.reject(new ApiError(message, status, code))
    }

    if (error.request) {
      const msg = '网络错误，请检查连接'
      try {
        useToastStore.getState().addToast({ type: 'error', message: msg, duration: 5000 })
      } catch { /* ignore */ }
      return Promise.reject(new ApiError(msg, 0, 'NETWORK_ERROR'))
    }

    return Promise.reject(new ApiError(error.message, 0, 'UNKNOWN'))
  }
)

// ── Generic request helpers ──
async function request<T>(method: string, path: string, body?: unknown, config?: AxiosRequestConfig): Promise<T> {
  const resp = await axiosInstance.request<ApiResponse<T>>({
    method: method as Method,
    url: path,
    data: body,
    ...config,
  })
  return resp.data as T
}

export const api = {
  get: <T>(path: string, config?: AxiosRequestConfig) => request<T>('GET', path, undefined, config),
  post: <T>(path: string, body?: unknown, config?: AxiosRequestConfig) => request<T>('POST', path, body, config),
  put: <T>(path: string, body?: unknown, config?: AxiosRequestConfig) => request<T>('PUT', path, body, config),
  del: <T>(path: string, config?: AxiosRequestConfig) => request<T>('DELETE', path, undefined, config),
}

// ── Billing ──
export const billingApi = {
  plans: () => api.get<BillingPlan[]>('/billing/plans'),
  chains: () => api.get<ChainInfo[]>('/billing/chains'),
  createOrder: (data: { plan_id: string; chain: string; tx_hash: string }) =>
    api.post<BillingOrder>('/billing/orders', data),
}

// ── Auth ──
export const authApi = {
  login: (username: string, password: string) =>
    api.post<{ access_token: string; token_type: string; user: { id: number; username: string; role: string; nickname: string } }>('/auth/login', { username, password }),

  loginCode: (email: string, code: string) =>
    api.post<{ access_token: string; token_type: string; user: { id: number; username: string; role: string; nickname: string } }>('/auth/login-code', { email, code }),

  register: (data: { username: string; password: string; email: string; code: string; nickname?: string }) =>
    api.post<{ access_token: string; token_type: string; user: { id: number; username: string; role: string; nickname: string } }>('/auth/register', data),

  sendCode: (email: string, code_type: string) =>
    api.post<{ detail: string; email: string }>('/auth/send-code', { email, code_type }),

  resetPassword: (email: string, code: string, password: string) =>
    api.post<{ detail: string }>('/auth/reset-password', { email, code, password }),

  me: () => api.get<{ username: string; role: string }>('/auth/me'),
}

// ── User Profile ──
export const userApi = {
  profile: () => api.get<{
    id: number; username: string; nickname: string; email: string;
    role: string; is_active: number; email_verified: number;
    created_at: string; credits: number; is_vip: boolean;
    referral_code: string; referral_count: number;
  }>('/user/profile'),

  updateProfile: (data: { nickname?: string; email?: string }) =>
    api.put<{ detail: string }>('/user/profile', data),

  changePassword: (oldPassword: string, newPassword: string) =>
    api.post<{ detail: string }>('/user/change-password', { old_password: oldPassword, new_password: newPassword }),

  notificationSettings: () =>
    api.get<{ channels: Record<string, boolean> }>('/user/notification-settings'),

  saveNotificationSettings: (channels: Record<string, boolean>) =>
    api.put<{ detail: string }>('/user/notification-settings', { channels }),
}

// ── Dashboard ──
export const dashboardApi = {
  summary: () => api.get<DashboardSummary>('/dashboard/summary'),
}

// ── Portfolio ──
export const portfolioApi = {
  summary: () => api.get<PortfolioSummary>('/portfolio/summary'),
  positions: () => api.get<{ positions: PortfolioPosition[] }>('/portfolio/positions'),
  snapshots: (days?: number) => api.get<{ snapshots: EquitySnapshot[] }>(`/portfolio/snapshots${days ? '?days=' + days : ''}`),
  calendar: (year?: number, month?: number) =>
    api.get<{ months: CalendarMonth[] }>(`/portfolio/calendar?year=${year || new Date().getFullYear()}&month=${month || new Date().getMonth() + 1}`),
}

// ── Market ──
export const marketApi = {
  klines: (symbol: string, interval = '1h', limit = 200, from?: number, to?: number) =>
    api.get<{ klines: KlineBar[] } & Record<string, unknown>>('/market/klines', { params: { symbol, interval, limit, from, to } })
      .then((d) => {
        const klines = d?.klines ?? (d?.data as Record<string, unknown>)?.klines ?? (Array.isArray(d) ? d : [])
        return Array.isArray(klines) ? klines : []
      }),
  orderBook: (symbol: string, depth = 20) =>
    api.get<OrderBook>('/market/orderbook', { params: { symbol, depth } }),
  trades: (symbol: string, limit = 50) =>
    api.get<{ trades: Trade[] }>('/market/trades', { params: { symbol, limit } })
      .then((d) => Array.isArray(d?.trades) ? d.trades : Array.isArray(d) ? d : []),
  snapshot: (symbol?: string) =>
    api.get<MarketSnapshotResponse>(`/market/snapshot${symbol ? '?symbol=' + symbol : ''}`),
  symbolSearch: (q: string) => api.get<{ symbols: string[] }>(`/symbols/search?q=${q}`),
  status: () => api.get<{ status: string }>('/status'),
  // Binance public API — funding rate & mark price
  fundingRate: async (symbol: string) => {
    try {
      const resp = await axios.get(`https://fapi.binance.com/fapi/v1/premiumIndex?symbol=${symbol}`)
      const data = resp.data as Record<string, unknown>
      return {
        fundingRate: parseFloat(String(data.lastFundingRate ?? 0)),
        markPrice: parseFloat(String(data.markPrice ?? 0)),
        nextFundingTime: Number(data.nextFundingTime ?? 0),
      }
    } catch {
      return { fundingRate: 0, markPrice: 0, nextFundingTime: 0 }
    }
  },
  markPrice: async (symbol: string) => {
    try {
      const resp = await axios.get(`https://fapi.binance.com/fapi/v1/premiumIndex?symbol=${symbol}`)
      const data = resp.data as Record<string, unknown>
      return parseFloat(String(data.markPrice ?? 0))
    } catch {
      return 0
    }
  },
}

// ── Orders ──
export const orderApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<{ orders: Order[] }>(`/orders${qs}`).then(d => d?.orders ?? [])
  },
  place: (order: Record<string, unknown>) => api.post<Order>('/orders', order),
  cancel: (id: string) => api.post<{ success: boolean }>(`/orders/${id}/cancel`),
  cancelAll: () => api.post<{ success: boolean }>('/orders/cancel-all'),
  history: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<{ orders: Order[] }>(`/orders/history${qs}`).then(d => d?.orders ?? [])
  },
}

// ── Account ──
export const accountApi = {
  balance: (symbol?: string) => api.get<{ balances: { asset: string; free: number; locked: number; total: number }[]; currencies?: { currency: string; available: number; total: number }[] }>(`/account/balance${symbol ? '?symbol=' + symbol : ''}`),
  transfer: (data: { from: string; to: string; currency: string; amount: number }) =>
    api.post<{ success: boolean; message: string }>('/account/transfer', data),
  buy: (data: { currency: string; amount: number; payment_method?: string }) =>
    api.post<{ success: boolean; order_id: string; message: string }>('/account/buy', data),
  swap: (data: { from_currency: string; to_currency: string; amount: number }) =>
    api.post<{ success: boolean; order_id: string; rate: number; message: string }>('/account/swap', data),
}

// ── Trades ──
export const tradesApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<{ trades: Trade[] }>(`/trades${qs}`).then(d => d?.trades ?? [])
  },
}

// ── Strategies ──
export const strategyApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<StrategyItem[]>(`/strategies/configs${qs}`)
  },
  get: (id: string) => api.get<StrategyItem>(`/strategies/configs/${id}`),
  create: (data: Partial<StrategyItem>) => api.post<{ id: string; success: boolean }>('/strategies/configs', data),
  update: (id: string, data: Partial<StrategyItem>) => api.put<{ success: boolean }>(`/strategies/configs/${id}`, data),
  delete: (id: string) => api.del<{ success: boolean }>(`/strategies/configs/${id}`),
  start: (id: string) => api.post<{ success: boolean }>(`/strategies/configs/${id}/start`),
  stop: (id: string) => api.post<{ success: boolean }>(`/strategies/configs/${id}/stop`),
  batchStart: (ids: string[]) => api.post<{ success: boolean; started: number }>('/strategies/configs/batch-start', { ids }),
  batchStop: (ids: string[]) => api.post<{ success: boolean; stopped: number }>('/strategies/configs/batch-stop', { ids }),
  logs: (strategyId?: string) =>
    api.get<StrategyLog[]>(`/strategies/logs${strategyId ? '?strategy_id=' + strategyId : ''}`),
  clearLogs: (strategyId?: string) =>
    api.del<{ success: boolean }>(`/strategies/logs${strategyId ? '?strategy_id=' + strategyId : ''}`),
  templates: (category = 'spot') => api.get<StrategyTemplate[]>(`/strategies/templates?category=${category}`),
  createTemplate: (data: Partial<StrategyTemplate>) => api.post<{ id: string; success: boolean }>('/strategies/templates', data),
  deleteTemplate: (id: string) => api.del<{ success: boolean }>(`/strategies/templates/${id}`),
  global: () => api.get<{ config: StrategyGlobalConfig }>('/strategies/global').then(d => d?.config ?? {}),
  saveGlobal: (data: StrategyGlobalConfig) => api.put<{ success: boolean }>('/strategies/global', data),
  spot: () => api.get<StrategyItem[]>('/strategies/spot'),
  contract: () => api.get<StrategyItem[]>('/strategies/contract'),
  ranking: () => api.get<StrategyRanking[]>('/strategies/ranking'),
  paramDefs: (type: string) => api.get<StrategyParamDefs>(`/strategies/param-defs?type=${type}`),
}

// ── Backtest ──
export const backtestApi = {
  run: (config: BacktestRequest) => api.post<BacktestResult>('/backtest/run', config, { timeout: TIMEOUTS.backtest }),
  native: (config: BacktestRequest) => api.post<BacktestResult>('/native/backtest', config, { timeout: TIMEOUTS.backtest }),
}

// ── AI ──
export const aiApi = {
  snapshot: (symbol?: string) =>
    api.get<AISnapshot>(`/ai/snapshot${symbol ? '?symbol=' + symbol : ''}`),
  klines: (symbol: string, interval?: string) =>
    api.get<{ klines: KlineBar[] }>(`/ai/klines?symbol=${symbol}&interval=${interval || '1h'}`),
  generate: (data: AIGenerateRequest) => api.post<AIGenerateResponse>('/ai/generate', data, { timeout: TIMEOUTS.ai }),
  multiAgent: (data: AIMultiAgentRequest) => api.post<AIMultiAgentResponse>('/ai/multi-agent', data, { timeout: TIMEOUTS.ai }),
  backtest: (data: BacktestRequest) => api.post<BacktestResult>('/ai/backtest', data, { timeout: TIMEOUTS.backtest }),
  optimize: (data: Record<string, unknown>) => api.post<{ iteration_history: { iteration: number; sharpe: number; return: number; max_drawdown?: number; win_rate?: number; total_trades?: number; params?: Record<string, unknown> }[]; best_sharpe?: number; best_return?: number; symbol?: string; strategy_type?: string }>('/ai/optimize', data, { timeout: TIMEOUTS.ai }),
  deploy: (data: Record<string, unknown>) => api.post<{ success: boolean; strategy_id: string }>('/ai/deploy', data),
  analyze: (data: Record<string, unknown>) => api.post<AIAnalysisResult>('/ai/analyze', data, { timeout: TIMEOUTS.analysis }),
  quickScan: () => api.get<AIQuickScan>('/ai/quickscan'),
  chat: (message: string) => api.post<AIChatResponse>('/ai/chat', { message }),
  models: () => api.get<AIModel[]>('/ai/models'),
  autoTradeGet: () => api.get<AIAutoTradeConfig>('/auto-trade/config'),
  autoTradeSave: (config: AIAutoTradeConfig) => api.put<AIAutoTradeConfig>('/auto-trade/config', config),
}

// ── Chat ──
export const chatApi = {
  send: (message: string) => api.post<AIChatResponse>('/chat/send', { message }),
}

// ── Agent ──
export const agentApi = {
  tokens: () => api.get<AgentToken[]>('/agent/tokens'),
  createToken: (data: Partial<AgentToken>) => api.post<AgentToken>('/agent/tokens', data),
  deleteToken: (id: string) => api.del<{ success: boolean }>(`/agent/tokens/${id}`),
  ccSwitchStatus: () => api.get<AgentCCSwitchStatus>('/agent/cc-switch'),
  aiConfig: () => api.get<AgentAIConfig>('/agent/ai-config'),
  saveAIConfig: (data: AgentAIConfig) => api.put<AgentAIConfig>('/agent/ai-config', data),
  chat: (message: string) => api.post<AIChatResponse>('/agent/chat', { message }),
}

// ── Config (raw store config) ──
export const configApi = {
  get: () => api.get<RawConfig>('/config'),
  save: (data: RawConfig) => api.put<RawConfig>('/config', data),
  exchangeTest: (data: ExchangeSettings) => api.post<ExchangeTestResult>('/exchange/test', data),
  exchangeSave: (data: ExchangeSettings) => api.post<ExchangeSaveResult>('/exchange/save', data),
  currencyGet: () => api.get<{ currency: string }>('/settings/currency'),
  currencySet: (currency: string) => api.put<{ currency: string }>('/settings/currency', { currency }),
  aiTest: (data: Record<string, unknown>) => api.post<{ success: boolean }>('/ai/test', data),
  aiSave: (data: Record<string, unknown>) => api.post<{ success: boolean }>('/ai/save', data),
  // Dynamic config endpoints (backend-driven)
  getMarkets: () => api.get<{ symbols: Array<{ symbol: string; base: string; quote: string; precision: { price: number; quantity: number } }> }>('/config/markets'),
  getIndices: () => api.get<{ heatmap: Record<string, string[]>; global_indices: Array<{ symbol: string; name: string; region: string }> }>('/config/indices'),
  getExchanges: () => api.get<{ exchanges: Array<{ key: string; label: string; status: string; supports: string[] }> }>('/config/exchanges'),
  getAIModels: () => api.get<{ providers: Array<{ key: string; label: string; models: string[]; baseUrl: string }> }>('/config/ai-models'),
  getRate: () => api.get<{ rate: number; from: string; to: string; timestamp: number }>('/config/rate'),
  exchangesConfigured: () => api.get<Record<string, ExchangeConfiguredStatus>>('/exchanges/configured'),
}

// ── Settings ──
export const settingsApi = {
  agentModels: () => api.get<AgentModel[]>('/settings/agent/models'),
  defaults: () => api.get<DefaultSettings>('/settings/defaults'),
  saveDefaults: (data: DefaultSettings) => api.post<DefaultSettings>('/settings/defaults', data),
  saveUI: (data: UISettings) => api.post<UISettings>('/settings/ui', data),
  exchangeTest: (id: string) => api.post<ExchangeTestResult>(`/settings/exchange/${id}/test`),
  exchangeSave: (id: string, data: ExchangeSettings) => api.put<ExchangeSettings>(`/settings/exchange/${id}`, data),
  aiTest: (id: string) => api.post<{ success: boolean }>(`/settings/ai/${id}/test`),
  aiSave: (id: string, data: Record<string, unknown>) => api.put<{ success: boolean }>(`/settings/ai/${id}`, data),
}

// ── Strategy Community ──
export const strategyCommunityApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; sort_by?: string }) =>
    api.get<{ items: StrategyCommunityItem[]; total: number }>('/community/strategies', { params }),
  detail: (id: number) => api.get<StrategyCommunityDetail>(`/community/strategies/${id}`),
  publish: (data: Partial<StrategyCommunityItem>) => api.post<{ success: boolean }>('/community/strategies/publish', data),
  comment: (id: number, content: string) => api.post<{ success: boolean; comment_id?: number }>(`/community/strategies/${id}/comment`, { content }),
  rate: (id: number, rating: number) => api.post<{ success: boolean }>(`/community/strategies/${id}/rate`, { rating }),
  leaderboard: (sortBy?: string, limit?: number) =>
    api.get<StrategyCommunityItem[]>('/community/strategies/leaderboard', { params: { sort_by: sortBy, limit } }),
  trending: (limit?: number) =>
    api.get<StrategyCommunityItem[]>('/community/strategies/trending', { params: { limit } }),
  overfit: (id: number) =>
    api.get<OverfitResult>(`/community/strategies/${id}/overfit`),
}

// ── ML ──
export const mlApi = {
  train: (config: Record<string, unknown>) => api.post<MLTrainResult>('/ml/train', config, { timeout: 120000 }),
  predict: (data: Record<string, unknown>) => api.post<{ prediction: number; confidence: number }>('/ml/predict', data),
  list: () => api.get<{ models: MLModelInfo[] }>('/ml/models').then(d => d?.models ?? []),
  detail: (id: string) => api.get<MLModelInfo>(`/ml/models/${id}`),
  deleteModel: (id: string) => api.del<{ success: boolean }>(`/ml/models/${id}`),
  importance: (id: string) => api.get<{ importance: { feature: string; score: number }[] }>(`/ml/models/${id}/importance`),
  generateFeatures: (data: Record<string, unknown>) => api.post<{ features: string[] }>('/ml/features', data),
  health: () => api.get<{ status: string }>('/ml/health'),
  deploy: (data: Record<string, unknown>) => api.post<{ success: boolean; strategy_id: string }>('/ml/deploy', data),
  strategyModels: () => api.get<{ models: MLModelInfo[] }>('/ml/strategy-models').then(d => d?.models ?? []),
}

// ── RL (Reinforcement Learning) ──
export const rlApi = {
  train: (config: Record<string, unknown>) => api.post<RLTrainResult | { job_id: string; status: string; message: string }>('/rl/train', config, { timeout: 300000 }),
  predict: (data: Record<string, unknown>) => api.post<RLPredictResult>('/rl/predict', data),
  evaluate: (data: Record<string, unknown>) => api.post<RLEvalResult>('/rl/evaluate', data),
  list: () => api.get<{ models: RLModelInfo[] }>('/rl/models').then(d => d?.models ?? []),
  deleteModel: (id: string) => api.del<{ success: boolean }>(`/rl/models/${id}`),
  getJob: (id: string) => api.get<RLJob>(`/rl/jobs/${id}`),
  cancelJob: (id: string) => api.post<{ success: boolean }>(`/rl/jobs/${id}/cancel`),
  getWorkerStatus: () => api.get<RLWorkerStatus>('/rl/worker/status'),
  startWorker: (config: Record<string, unknown>) => api.post<{ success: boolean; message: string; worker_pid?: number; command?: string; error?: string }>('/rl/worker/start', config),
}

// ── TensorBoard ──
export const tensorboardApi = {
  listRuns: () => api.get<TensorBoardSummary>('/tensorboard/runs'),
  queryScalars: (data: Record<string, unknown>) => api.post<TensorBoardQueryResult>('/tensorboard/scalars', data),
  getRun: (id: string) => api.get<TensorBoardRun>(`/tensorboard/runs/${id}`),
  deleteRun: (id: string) => api.del<{ success: boolean }>(`/tensorboard/runs/${id}`),
}

// ── Protection / Risk Control ──
export const protectionApi = {
  status: () => api.get<ProtectionStatus>('/protection/status'),
  getConfig: () => api.get<{ protections: ProtectionConfigItem[] }>('/protection/config'),
  config: (data: { protections: ProtectionConfigItem[] }) =>
    api.post<{ success: boolean }>('/protection/config', data),
  reset: (scope?: 'global' | 'pair' | 'all', symbol?: string) =>
    api.post<{ success: boolean }>('/protection/reset', undefined, { params: { scope, symbol } }),
  recordTrade: (data: {
    symbol: string
    side: string
    entry_price: number
    exit_price: number
    quantity: number
    pnl: number
    pnl_pct: number
    is_stoploss: boolean
    exit_time?: number
  }) => api.post<{ success: boolean }>('/protection/trade', data),
}

// ── Pairlist ──
export const pairlistApi = {
  whitelist: (exchange?: string, quoteAsset?: string) =>
    api.get<PairlistWhitelist>('/pairlist/whitelist', { params: { exchange, quote_asset: quoteAsset } }),
  refresh: (exchange?: string, quoteAsset?: string) =>
    api.get<PairlistWhitelist>('/pairlist/refresh', { params: { exchange, quote_asset: quoteAsset } }),
  config: () => api.get<PairlistConfig>('/pairlist/config'),
  configure: (data: PairlistConfig) =>
    api.post<PairlistConfig>('/pairlist/config', data),
}

// ── Advanced Orders ──
export const advancedOrderApi = {
  oco: {
    place: (data: Partial<OCOOrder>) => api.post<OCOOrder>('/orders/oco', data),
    list: () => api.get<OCOOrder[]>('/orders/oco'),
    cancel: (id: string) => api.del<{ success: boolean }>(`/orders/oco/${id}`),
  },
  bracket: {
    place: (data: Partial<BracketOrder>) => api.post<BracketOrder>('/orders/bracket', data),
    list: () => api.get<BracketOrder[]>('/orders/bracket'),
    cancel: (id: string) => api.del<{ success: boolean }>(`/orders/bracket/${id}`),
  },
  iceberg: {
    place: (data: Partial<IcebergOrder>) => api.post<IcebergOrder>('/orders/iceberg', data),
    list: () => api.get<IcebergOrder[]>('/orders/iceberg'),
    cancel: (id: string) => api.del<{ success: boolean }>(`/orders/iceberg/${id}`),
  },
}

const SEC_TO_NS = 1e9

// ── Arbitrage ──
export const arbitrageApi = {
  config: () =>
    api.get<{ config: ArbitrageConfig }>('/arbitrage/config').then((r) => {
      const cfg = r.config
      return { ...cfg, poll_interval: cfg.poll_interval / SEC_TO_NS }
    }),

  updateConfig: (data: ArbitrageConfig) => {
    const payload = { ...data, poll_interval: data.poll_interval * SEC_TO_NS }
    return api.post<{ config: ArbitrageConfig }>('/arbitrage/config', payload).then((r) => ({
      ...r.config,
      poll_interval: r.config.poll_interval / SEC_TO_NS,
    }))
  },

  start: () => api.post<{ status: string }>('/arbitrage/start'),
  stop: () => api.post<{ status: string }>('/arbitrage/stop'),
  status: () => api.get<ArbitrageStatus>('/arbitrage/status'),
  performance: () => api.get<ArbitragePerformance>('/arbitrage/performance'),

  opportunity: () =>
    api.get<{ opportunity: ArbitrageOpportunity | null }>('/arbitrage/opportunity').then((r) =>
      r.opportunity ? [r.opportunity] : []
    ),

  positions: () =>
    api.get<{ positions: ArbitragePosition[] }>('/arbitrage/positions').then((r) => r.positions ?? []),

  history: (limit?: number) =>
    api.get<{ history: ArbitrageHistoryItem[] }>('/arbitrage/history', { params: { limit } }).then(
      (r) => r.history ?? []
    ),

  exchanges: () =>
    api.get<{ registered_count: number; exchanges: string[] }>('/arbitrage/exchanges'),

  registerExchange: (data: Partial<ArbitrageExchange>) =>
    api.post<{ status: string; exchange: string }>('/arbitrage/exchanges', data),

  execute: (data: {
    symbol: string
    buy_exchange: string
    sell_exchange: string
    buy_price: number
    sell_price: number
    quantity: number
  }) => api.post<{ status: string; opportunity: ArbitrageOpportunity }>('/arbitrage/execute', data),

  closePosition: (id: string, sell_price: number) =>
    api.post<{ status: string }>(`/arbitrage/positions/${id}/close`, { sell_price }),

  failPosition: (id: string) =>
    api.post<{ status: string }>(`/arbitrage/positions/${id}/fail`),
}

// ── Triangular Arbitrage ──

export const triangularApi = {
  config: () =>
    api.get<{ config: TriangularConfig }>('/triangular/config').then((r) => r.config),

  updateConfig: (data: TriangularConfig) =>
    api.post<{ config: TriangularConfig }>('/triangular/config', data).then((r) => r.config),

  start: () => api.post<{ status: string }>('/triangular/start'),
  stop: () => api.post<{ status: string }>('/triangular/stop'),
  status: () => api.get<{ running: boolean; stats: Record<string, unknown> }>('/triangular/status'),
  performance: () => api.get<TriangularPerformance>('/triangular/performance'),

  opportunity: () =>
    api.get<{ opportunity: TriangularOpportunity | null }>('/triangular/opportunity').then((r) =>
      r.opportunity ? [r.opportunity] : []
    ),

  positions: () =>
    api.get<{ positions: TriangularTrade[] }>('/triangular/positions').then((r) => r.positions ?? []),

  history: (limit?: number) =>
    api.get<{ history: TriangularTrade[] }>('/triangular/history', { params: { limit } }).then(
      (r) => r.history ?? []
    ),

  execute: (data: {
    exchange: string
    cycle: string[]
    start_qty: number
  }) =>
    api.post<{ status: string; opportunity: TriangularOpportunity }>('/triangular/execute', data),

  closePosition: (id: string) =>
    api.post<{ status: string }>(`/triangular/positions/${id}/close`),

  failPosition: (id: string) =>
    api.post<{ status: string }>(`/triangular/positions/${id}/fail`),
}

// ── Hyperopt ──
export const hyperoptApi = {
  start: (data: Record<string, unknown>) => api.post<{ job_id: string }>('/hyperopt/start', data, { timeout: 600000 }),
  jobs: () => api.get<{ jobs: HyperoptJob[] }>('/hyperopt/jobs').then(d => d?.jobs ?? []),
  job: (id: string) => api.get<HyperoptJob>(`/hyperopt/jobs/${id}`),
  cancel: (id: string) => api.post<{ success: boolean }>(`/hyperopt/jobs/${id}/cancel`),
  delete: (id: string) => api.del<{ success: boolean }>(`/hyperopt/jobs/${id}`),
  spaces: (strategy?: string) => api.get<{ spaces: HyperoptSpace[] }>('/hyperopt/spaces', { params: { strategy } }).then(d => d?.spaces ?? []),
}

// ── Notifications ──
export const notificationApi = {
  list: (params?: { limit?: number; offset?: number; unread?: boolean }) =>
    api.get<{ notifications: NotificationItem[]; total: number }>('/notifications', { params }).then(d => d?.notifications ?? []),
  unreadCount: () => api.get<{ count: number }>('/notifications/unread-count').then(d => d?.count ?? 0),
  markRead: (id: number) => api.post<{ success: boolean }>(`/notifications/${id}/read`),
  markAllRead: () => api.post<{ success: boolean }>('/notifications/read-all'),
  clear: () => api.del<{ success: boolean }>('/notifications'),
}

// ── Notify Routes ──
export const notifyRouteApi = {
  list: () => api.get<{ rules: NotifyRoute[] }>('/notify/routes').then(d => d?.rules ?? []),
  save: (rule: Partial<NotifyRoute>) => api.post<{ id: string }>('/notify/routes', rule),
  delete: (id: string) => api.del<{ success: boolean }>(`/notify/routes/${id}`),
  test: (channel: string, message?: string) => api.post<{ success: boolean }>('/notify/test', { channel, message: message || '测试消息' }),
}

// ── Indicators ──
export const indicatorApi = {
  // --- New contract-based API ---
  parse: (code: string) => api.post<{ success: boolean; params?: Record<string, unknown>; error?: string }>('/indicator/parse', { code }),
  validate: (code: string) => api.post<{ success: boolean; error?: string; params?: Record<string, unknown> }>('/indicator/validate', { code }),
  save: (data: Record<string, unknown>) => api.post<{ id: number; success: boolean }>('/indicator/save', data),
  list: () => api.get<{ items: IndicatorItem[]; total: number }>('/indicator/list').then(d => d?.items ?? []),
  get: (id: number) => api.get<IndicatorDetail>(`/indicator/${id}`),
  delete: (id: number) => api.del<{ success: boolean }>(`/indicator/${id}`),
  applyParamDefaults: (code: string, indicatorParams: Record<string, unknown>) =>
    api.post<{ params: Record<string, unknown> }>('/indicator/applyParamDefaults', { code, indicatorParams }).then(d => d?.params ?? {}),

  // --- Legacy API (keep for compatibility) ---
  listLegacy: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<IndicatorItem[]>(`/indicator/getIndicators${qs}`).then(d => d ?? [])
  },
  create: (data: Partial<IndicatorDetail>) => api.post<IndicatorDetail>('/indicator/saveIndicator', data),
  update: (id: number, data: Partial<IndicatorDetail>) => api.put<IndicatorDetail>(`/indicator/${id}`, data),
  saveAs: (data: Partial<IndicatorDetail>) => api.post<IndicatorDetail>('/indicator/saveIndicator', { ...data, is_new_copy: true }),
  publish: (id: number, data: Record<string, unknown>) => api.post<{ success: boolean }>('/indicator/publish', { id, ...data }),
  decrypt: (userId: number, indicatorId: number) =>
    api.post<{ key: string; success: boolean }>('/indicator/getDecryptKey', { user_id: userId, indicator_id: indicatorId }),
  backtest: (data: Record<string, unknown>) => api.post<IndicatorBacktestResult>('/indicator/backtest', data, { timeout: TIMEOUTS.backtest }),
  aiGenerate: (data: Record<string, unknown>) => api.post<IndicatorAIGenerateResult>('/indicator/ai-generate', data, { timeout: TIMEOUTS.ai }),
  aiGenerateStream: (
    data: { prompt: string; existingCode?: string },
    handlers: {
      onCodeChunk?: (chunk: string) => void
      onStatus?: (status: string) => void
      onValidation?: (result: { success: boolean; error?: string; params?: Record<string, unknown> }) => void
      onCodeReplace?: (code: string) => void
      onDebug?: (info: { event: string; data: Record<string, unknown> }) => void
      onDone?: () => void
      onError?: (err: string) => void
    }
  ) => {
    const token = localStorage.getItem('xt-token') || ''
    const url = `/api/indicator/ai-generate`
    return new Promise<void>((resolve, reject) => {
      fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${token}`,
          'Access-Token': token,
        },
        body: JSON.stringify(data),
      }).then(async (response) => {
        if (!response.ok) {
          const text = await response.text()
          reject(new Error(`HTTP ${response.status}: ${text}`))
          return
        }
        const reader = response.body?.getReader()
        if (!reader) {
          reject(new Error('No response body'))
          return
        }
        const decoder = new TextDecoder()
        let buffer = ''
        let currentEvent = ''
        let currentData = ''

        const flushEvent = () => {
          if (!currentEvent) { currentEvent = 'message' }
          if (currentData === '') { currentEvent = ''; return }
          try {
            if (currentEvent === 'code_chunk') {
              handlers.onCodeChunk?.(currentData)
            } else if (currentEvent === 'status') {
              handlers.onStatus?.(currentData)
            } else if (currentEvent === 'validation') {
              handlers.onValidation?.(JSON.parse(currentData))
            } else if (currentEvent === 'code') {
              const parsed = JSON.parse(currentData)
              handlers.onCodeReplace?.(parsed.code || '')
            } else if (currentEvent === 'debug') {
              handlers.onDebug?.(JSON.parse(currentData))
            } else if (currentEvent === 'done') {
              handlers.onDone?.()
            }
          } catch (e) {
            // Non-JSON payloads for simple events
            if (currentEvent === 'code_chunk') handlers.onCodeChunk?.(currentData)
            else if (currentEvent === 'status') handlers.onStatus?.(currentData)
            else if (currentEvent === 'done') handlers.onDone?.()
          }
          currentEvent = ''
          currentData = ''
        }

        try {
          while (true) {
            const { done, value } = await reader.read()
            if (done) break
            buffer += decoder.decode(value, { stream: true })
            const lines = buffer.split('\n')
            buffer = lines.pop() || ''
            for (const line of lines) {
              if (line.startsWith('event:')) {
                currentEvent = line.slice(6).trim()
              } else if (line.startsWith('data:')) {
                if (currentData !== '') currentData += '\n'
                currentData += line.slice(5).trim()
              } else if (line.trim() === '') {
                flushEvent()
              }
            }
          }
          // Process remaining buffer
          if (buffer) {
            const lines = buffer.split('\n')
            for (const line of lines) {
              if (line.startsWith('event:')) {
                currentEvent = line.slice(6).trim()
              } else if (line.startsWith('data:')) {
                if (currentData !== '') currentData += '\n'
                currentData += line.slice(5).trim()
              } else if (line.trim() === '') {
                flushEvent()
              }
            }
          }
          flushEvent()
          resolve()
        } catch (e: unknown) {
          const msg = e instanceof Error ? e.message : 'Stream error'
          handlers.onError?.(msg)
          reject(e)
        }
      }).catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : 'Network error'
        handlers.onError?.(msg)
        reject(err)
      })
    })
  },
  run: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/indicator/execute', data),
  execute: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/indicator/execute', data),
  watchlist: () => api.get<{ items: IndicatorItem[] }>('/watchlist').then(d => d?.items ?? []),
  addWatchlist: (data: Record<string, unknown>) => api.post<{ success: boolean }>('/watchlist', data),
  kline: (params: Record<string, string | number>) => api.get<{ klines: KlineBar[] }>('/indicator/kline', { params }).then(d => d?.klines ?? []),
  experiment: {
    run: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/experiment/run', data, { timeout: TIMEOUTS.backtest }),
    sensitivity: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/experiment/sensitivity', data, { timeout: TIMEOUTS.backtest }),
    walkForward: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/experiment/walk-forward', data, { timeout: TIMEOUTS.backtest }),
    aiOptimize: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/experiment/ai-optimize', data, { timeout: TIMEOUTS.backtest }),
    structuredTune: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/experiment/structured-tune', data, { timeout: TIMEOUTS.backtest }),
  },
}

// ── Social Trading ──
export const socialApi = {
  providers: () => api.get<{ providers: any[] }>('/social/providers').then(d => d?.providers ?? []),
  follow: (providerId: number, followerId: number) =>
    api.post<{ success: boolean }>(`/social/providers/${providerId}/follow`, undefined, { params: { follower_id: followerId } }),
  unfollow: (providerId: number, followerId: number) =>
    api.post<{ success: boolean }>(`/social/providers/${providerId}/unfollow`, undefined, { params: { follower_id: followerId } }),
  signals: (providerId?: number, limit?: number) =>
    api.get<{ signals: any[] }>('/social/signals', { params: { provider_id: providerId, limit } }).then(d => d?.signals ?? []),
  publishSignal: (data: Record<string, unknown>) => api.post<{ signal: any }>('/social/signals', data),
  followerConfigs: (followerId: number) =>
    api.get<{ configs: any[] }>('/social/followers/configs', { params: { follower_id: followerId } }).then(d => d?.configs ?? []),
  saveFollowerConfig: (data: {
    provider_id: number
    follower_id: number
    enabled: boolean
    multiplier: number
    max_position: number
    max_daily_loss: number
    slippage_pct: number
    auto_execute: boolean
    symbols: string[]
  }) => api.post<{ success: boolean }>('/social/followers/configs', data),
}

// ── On-Chain Data ──
export const onchainApi = {
  ethMetrics: () => api.get<any>('/onchain/eth/metrics'),
  btcMetrics: () => api.get<any>('/onchain/btc/metrics'),
  exchangeFlow: (exchange?: string) => api.get<any>('/onchain/exchange-flow', { params: { exchange } }),
  whaleAlerts: (minUSD?: number) => api.get<{ alerts: any[] }>('/onchain/whale-alerts', { params: { min_usd: minUSD } }).then(d => d?.alerts ?? []),
  btcSignal: () => api.get<any>('/onchain/signal/btc'),
  ethSignal: () => api.get<any>('/onchain/signal/eth'),
}

// ── Community ──
export const communityApi = {
  market: (params?: { page?: number; page_size?: number; keyword?: string; pricing_type?: string; sort_by?: string }) => {
    const qs = params ? '?' + new URLSearchParams(Object.entries(params).filter(([_, v]) => v !== undefined) as [string, string][]).toString() : ''
    return api.get<{ items: IndicatorItem[]; total: number }>(`/community/indicators${qs}`).then(d => d?.items ?? [])
  },
  publish: (data: { indicatorId: number; pricingType?: string; price?: number }) =>
    api.post<{ success: boolean }>('/community/publish', data),
  purchase: (id: number) => api.post<{ success: boolean; order_id?: string }>(`/community/purchase/${id}`, {}),
  comments: (id: number, page?: number, pageSize?: number) =>
    api.get<{ comments: CommunityComment[]; total: number }>(`/community/comments/${id}?page=${page || 1}&page_size=${pageSize || 20}`).then(d => d?.comments ?? []),
  addComment: (id: number, data: { rating: number; content: string }) =>
    api.post<{ success: boolean; comment_id?: number }>(`/community/comments/${id}`, data),
}

// ── Admin ──
export const adminApi = {
  users: () => api.get<AdminUser[]>('/admin/users').then(d => d ?? []),
  user: (id: number) => api.get<AdminUser>(`/admin/users/${id}`),
  updateUser: (id: number, data: Partial<AdminUser>) => api.put<{ success: boolean }>(`/admin/users/${id}`, data),
  stats: () => api.get<AdminStats>('/admin/stats'),
  enhancedStats: () => api.get<AdminStats>('/admin/stats'),
  auditLog: (params?: { limit?: number; offset?: number }) => api.get<{ logs: AdminAuditLog[]; total: number }>('/admin/audit-log', { params }),
}

// ── Agent (admin) ──
export const agentAdminApi = {
  tokens: () => agentApi.tokens(),
  createToken: (data: Partial<AgentToken>) => agentApi.createToken(data),
  deleteToken: (id: string) => agentApi.deleteToken(id),
  auditLog: () => api.get<AdminAuditLog[]>('/agent/audit-log').then(d => d ?? []),
}

// ── Strategy Config (Martin / WallStreet) ──
export const strategyConfigApi = {
  createMartin: (config: MartinConfig) => axiosInstance.post<{ id: string; success: boolean }>('/strategies/martin', config),
  createWallStreet: (config: WallStreetConfig) => axiosInstance.post<{ id: string; success: boolean }>('/strategies/wallstreet', config),
  getMartinConfigs: () => axiosInstance.get<MartinConfig[]>('/strategies/martin'),
  getWallStreetConfigs: () => axiosInstance.get<WallStreetConfig[]>('/strategies/wallstreet'),
  updateMartin: (id: string, config: MartinConfig) => axiosInstance.put<{ success: boolean }>(`/strategies/martin/${id}`, config),
  updateWallStreet: (id: string, config: WallStreetConfig) => axiosInstance.put<{ success: boolean }>(`/strategies/wallstreet/${id}`, config),
  deleteMartin: (id: string) => axiosInstance.delete<{ success: boolean }>(`/strategies/martin/${id}`),
  deleteWallStreet: (id: string) => axiosInstance.delete<{ success: boolean }>(`/strategies/wallstreet/${id}`),
}

// ── Signal Executor ──
export const executorApi = {
  getStatus: () => axiosInstance.get<ExecutorStatus>('/executor/status'),
  getActivePositions: () => axiosInstance.get<{ positions: ExecutorPosition[] }>('/executor/positions'),
  getExecutionRecords: (params?: { bot_id?: string; limit?: number }) =>
    axiosInstance.get<{ records: ExecutionRecord[] }>('/executor/records', { params }),
  getSignalSources: () => axiosInstance.get<{ sources: SignalSource[] }>('/executor/signal-sources'),
  updateSignalSource: (id: string, data: Partial<SignalSource>) =>
    axiosInstance.put<{ success: boolean }>(`/executor/signal-sources/${id}`, data),
}

// ── AI Robot ──
export const aiRobotApi = {
  getConfig: () => axiosInstance.get<AIRobotConfig>('/ai-robot/config'),
  saveConfig: (config: AIRobotConfig) => axiosInstance.post<AIRobotConfig>('/ai-robot/config', config),
  getStatus: () => axiosInstance.get<{ success: boolean; data: AIStatus }>('/ai/status').then((r) => r.data?.data),
  getSignals: (params?: { limit?: number; symbol?: string }) =>
    axiosInstance.get<{ signals: AISignal[] }>('/ai/signals', { params }).then((r) => r.data?.signals || []),
  getModels: () => axiosInstance.get<{ models: string[] }>('/ai-robot/models'),
}

// ── Contract Trading ──
export const contractApi = {
  getLeverage: () => axiosInstance.get<{ leverage: number }>('/contract/leverage'),
  setLeverage: (leverage: number) => axiosInstance.post<{ success: boolean }>('/contract/leverage', { leverage }),
  getMarginInfo: () => axiosInstance.get<ContractMarginInfo>('/contract/margin'),
  getLiquidationPrice: (params: { entry_price: number; side: string; leverage: number }) =>
    axiosInstance.get<LiquidationPriceResult>('/contract/liquidation-price', { params }),
  saveParams: (params: ContractParams) => axiosInstance.post<{ success: boolean }>('/contract/params', params),
  getParams: () => axiosInstance.get<ContractParams>('/contract/params'),
}

// ── AI Bots Marketplace ──
export const aiBotApi = {
  // Catalog
  catalog: () => api.get<AIBotCatalogItem[]>('/ai-bots/catalog').then(d => d ?? []),
  catalogItem: (id: string) => api.get<AIBotCatalogItem>(`/ai-bots/catalog/${id}`),

  // Instances
  list: () => api.get<AIBotInstance[]>('/ai-bots/instances').then(d => d ?? []),
  get: (id: string) => api.get<AIBotInstance>(`/ai-bots/instances/${id}`),
  create: (data: AIBotCreateRequest) => api.post<AIBotInstance>('/ai-bots/instances', data),
  update: (id: string, data: Partial<AIBotInstance>) => api.put<AIBotInstance>(`/ai-bots/instances/${id}`, data),
  delete: (id: string) => api.del<{ id: string }>(`/ai-bots/instances/${id}`),
  start: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/start`),
  pause: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/pause`),
  resume: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/resume`),
  stop: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/stop`),
  clone: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/clone`),
  batchStart: (ids: string[]) => api.post<{ success: boolean; started: number }>('/ai-bots/instances/batch-start', { ids }),
  batchStop: (ids: string[]) => api.post<{ success: boolean; stopped: number }>('/ai-bots/instances/batch-stop', { ids }),
  batchDelete: (ids: string[]) => api.post<{ success: boolean; deleted: number }>('/ai-bots/instances/batch-delete', { ids }),

  // Analytics
  analytics: (id: string) => api.get<AIBotAnalytics>(`/ai-bots/instances/${id}/analytics`),
  trades: (id: string, limit = 50) => api.get<{ bot: AIBotInstance; trades: AIBotTrade[] }>(`/ai-bots/instances/${id}/trades?limit=${limit}`),

  // Subscriptions
  subscriptions: () => api.get<AIBotSubscription[]>('/ai-bots/subscriptions').then(d => d ?? []),
  subscribe: (data: Partial<AIBotSubscription>) => api.post<{ id: number }>('/ai-bots/subscriptions', data),
  cancelSubscription: (id: number) => api.post<{ id: number }>(`/ai-bots/subscriptions/${id}/cancel`),
}

export { ApiError }
