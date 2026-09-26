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
  type HyperoptEpoch,
  type HyperoptEpochApplyResult,
  type MLModelInfo,
  type MLTrainResult,
  type IndicatorItem,
  type IndicatorComment,
  type IndicatorDetail,
  type BillingPlan,
  type ChainInfo,
  type BillingOrder,
  type BillingSubscription,
  type BillingVerificationResponse,
  type StripeConfig,
  type BacktestResult,
  type BacktestReport,
  type BotConfig,
  type NotificationItem,
  type StrategyItem,
  type StrategyLog,
  type StrategyTemplate,
  type StrategyParamDefs,
  type StrategyRanking,
  type StrategyRuntimeResponse,
  type StrategyBatchResult,
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
  type LadderOrder,
  type LadderCreateRequest,
  type LadderAmendRequest,
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
  type ExecutorStats,
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
  type DataCoverageResponse,
  type DataInfoResponse,
  type DownloadConfig,
  type DownloadJobStatus,
  type BarDataResponse,
  type HealthResponse,
  type ComponentHealthResponse,
  type StatusResponse,
  type TradingSafetyResponse,
  type ExchangeStatusResponse,
  type ProviderResult,
  type SentimentResponse,
  type MacroData,
  type NewsData,
  type HeatmapData,
  type CalendarData,
  type SourceHealth,
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
const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api'

const axiosInstance: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
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
          // Avoid full reload when the user is already on the login page
          // (e.g. wrong password should only show an error toast).
          if (window.location.pathname !== '/login') {
            window.location.href = '/login'
          }
          setTimeout(() => {
            isRedirectingToLogin = false
          }, 3000)
        }
        return Promise.reject(new ApiError('登录已过期，请重新登录', 401, 'UNAUTHORIZED'))
      }

      // 403 Forbidden → show backend message
      if (status === 403) {
        const msg = (data?.msg as string) || (data?.message as string) || '权限不足'
        // Show toast for 403
        try {
          useToastStore.getState().addToast({ type: 'warning', message: msg, duration: 5000 })
        } catch {
          /* ignore */
        }
        return Promise.reject(new ApiError(msg, 403, 'FORBIDDEN'))
      }

      // 429 Rate limit
      if (status === 429) {
        const msg = '请求过于频繁，请稍后再试'
        try {
          useToastStore.getState().addToast({ type: 'warning', message: msg, duration: 6000 })
        } catch {
          /* ignore */
        }
        return Promise.reject(new ApiError(msg, 429, 'RATE_LIMIT'))
      }

      // 5xx Server error
      if (status >= 500) {
        // Try wrapped error format first: { error: { message: ... } }
        const wrappedError = data?.error as Record<string, unknown> | undefined
        const msg =
          (wrappedError?.message as string) ||
          (data?.message as string) ||
          (data?.detail as string) ||
          (data?.error as string) ||
          '服务器错误，请稍后重试'
        try {
          useToastStore.getState().addToast({ type: 'error', message: msg, duration: 6000 })
        } catch {
          /* ignore */
        }
        return Promise.reject(
          new ApiError(msg, status, (wrappedError?.code as string) || (data?.code as string) || 'SERVER_ERROR')
        )
      }

      // Backend wraps errors as { error: { code, message } }
      const wrappedError = data?.error as Record<string, unknown> | undefined
      const message =
        (wrappedError?.message as string) ||
        (data?.message as string) ||
        (data?.detail as string) ||
        `请求失败 (${status})`
      const code = (wrappedError?.code as string) || (data?.code as string) || 'HTTP_ERROR'
      // Show toast for client errors (4xx except 401/403/429)
      if (status >= 400 && status !== 401 && status !== 403 && status !== 429) {
        try {
          useToastStore.getState().addToast({ type: 'error', message, duration: 5000 })
        } catch {
          /* ignore */
        }
      }
      return Promise.reject(new ApiError(message, status, code))
    }

    if (error.request) {
      const msg = '网络错误，请检查连接'
      try {
        useToastStore.getState().addToast({ type: 'error', message: msg, duration: 5000 })
      } catch {
        /* ignore */
      }
      return Promise.reject(new ApiError(msg, 0, 'NETWORK_ERROR'))
    }

    return Promise.reject(new ApiError(error.message, 0, 'UNKNOWN'))
  }
)

// ── Generic request helpers ──
/** 部分后端接口包一层 { success, data }（响应拦截器只自动解含 meta 的包），此处兜底解包。 */
function unwrapEnvelope<T>(payload: T | { success?: boolean; data?: T }): T {
  if (payload && typeof payload === 'object' && 'success' in payload && 'data' in payload) {
    return (payload as { data?: T }).data as T
  }
  return payload as T
}

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
  subscription: () => api.get<BillingSubscription>('/billing/subscription'),
  orders: () => api.get<{ orders: BillingOrder[] }>('/billing/orders'),
  order: (id: string) => api.get<BillingOrder>(`/billing/orders/${id}`),
  createOrder: (data: { plan_id: string; chain: string; tx_hash?: string }) =>
    api.post<BillingOrder>('/billing/orders', data),
  submitTx: (id: string, tx_hash: string) =>
    api.post<BillingOrder>(`/billing/orders/${id}/tx`, { tx_hash }),
  verification: (id: string) =>
    api.get<BillingVerificationResponse>(`/billing/orders/${id}/verification`),
  stripeConfig: () => api.get<StripeConfig>('/billing/stripe/config'),
  stripeCheckout: (data: { plan_id?: string; order_id?: string; success_url: string; cancel_url: string }) =>
    api.post<{ checkout_url: string }>('/billing/stripe/checkout', data),
}

// ── Auth ──
export interface AuthUser {
  id: number
  username: string
  role: string
  nickname?: string
  email?: string
}

// 登录/注册/验证码登录的统一响应：未开 MFA 时直接给 access_token；
// 开 MFA 时进入第二步（mfa_required + mfa_token，换 /auth/mfa/verify）。
export interface AuthResult {
  access_token?: string
  token_type?: string
  mfa_required?: boolean
  mfa_token?: string
  must_change_password?: boolean
  user?: AuthUser
}

export const authApi = {
  login: (username: string, password: string, turnstileToken?: string) =>
    api.post<AuthResult>('/auth/login', { username, password, turnstile_token: turnstileToken }),

  loginCode: (email: string, code: string, turnstileToken?: string) =>
    api.post<AuthResult>('/auth/login-code', { email, code, turnstile_token: turnstileToken }),

  register: (
    data: { username: string; password: string; email: string; code: string; nickname?: string; referral_code?: string },
    turnstileToken?: string
  ) => api.post<AuthResult>('/auth/register', { ...data, turnstile_token: turnstileToken }),

  sendCode: (email: string, code_type: string) =>
    api.post<{ detail: string; email: string }>('/auth/send-code', { email, code_type }),

  resetPassword: (email: string, code: string, password: string) =>
    api.post<{ detail: string }>('/auth/reset-password', { email, code, password }),

  me: () =>
    api.get<{ id: number; username: string; role: string; totp_enabled?: boolean; must_change_password?: boolean }>(
      '/auth/me'
    ),

  changePassword: (oldPassword: string, newPassword: string) =>
    api.post<{ detail: string }>('/auth/change-password', { old_password: oldPassword, new_password: newPassword }),

  logout: () => api.post<{ detail: string }>('/auth/logout'),
}

// ── MFA / TOTP 两步验证（A3.1） ──
export const mfaApi = {
  setup: () => api.post<{ secret: string; otpauth_uri: string }>('/auth/mfa/setup'),
  enable: (code: string) => api.post<{ detail: string; backup_codes: string[] }>('/auth/mfa/enable', { code }),
  disable: (code: string) => api.post<{ detail: string }>('/auth/mfa/disable', { code }),
  verify: (mfaToken: string, code: string) =>
    api.post<AuthResult>('/auth/mfa/verify', { mfa_token: mfaToken, code }),
}

// ── User Profile ──
export const userApi = {
  profile: () =>
    api.get<{
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
    }>('/user/profile'),

  updateProfile: (data: { nickname?: string; email?: string }) => api.put<{ detail: string }>('/user/profile', data),

  changePassword: (oldPassword: string, newPassword: string) =>
    api.post<{ detail: string }>('/user/change-password', { old_password: oldPassword, new_password: newPassword }),

  notificationSettings: () => api.get<{ channels: Record<string, boolean> }>('/user/notification-settings'),

  saveNotificationSettings: (channels: Record<string, boolean>) =>
    api.put<{ detail: string }>('/user/notification-settings', { channels }),
}

// ── Dashboard ──
export const dashboardApi = {
  summary: () => api.get<DashboardSummary>('/dashboard/summary'),
}

// ── Portfolio ──
// ── 已平仓持仓（GET /positions/closed，分页）──
export interface ClosedPosition {
  id: string
  symbol: string
  side: string
  quantity: number
  avg_entry_price: number
  exit_price: number
  realized_pnl: number
  cost_basis: number
  pnl_pct: number
  exchange: string
  opened_at: number
  closed_at: number
}

export const portfolioApi = {
  summary: () => api.get<PortfolioSummary>('/portfolio/summary'),
  positions: () => api.get<{ positions: PortfolioPosition[] }>('/portfolio/positions'),
  closedPositions: (params?: { limit?: number; offset?: number }) => {
    const qs = params ? '?' + new URLSearchParams(
      Object.fromEntries(Object.entries(params).map(([k, v]) => [k, String(v)]))
    ).toString() : ''
    return api.get<{ positions: ClosedPosition[]; limit: number; offset: number; has_more: boolean }>(
      `/positions/closed${qs}`
    )
  },
  snapshots: (days?: number) =>
    api.get<{ snapshots: EquitySnapshot[] }>(`/portfolio/snapshots${days ? '?days=' + days : ''}`),
  calendar: (year?: number, month?: number) =>
    api.get<{ months: CalendarMonth[] }>(
      `/portfolio/calendar?year=${year || new Date().getFullYear()}&month=${month || new Date().getMonth() + 1}`
    ),
}

// ── Market ──
export const marketApi = {
  klines: (symbol: string, interval = '1h', limit = 200, from?: number, to?: number) =>
    api
      .get<
        { klines: KlineBar[] } & Record<string, unknown>
      >('/market/klines', { params: { symbol, interval, limit, from, to } })
      .then((d) => {
        const klines = d?.klines ?? (d?.data as Record<string, unknown>)?.klines ?? (Array.isArray(d) ? d : [])
        return Array.isArray(klines) ? klines : []
      }),
  orderBook: (symbol: string, depth = 20) => api.get<OrderBook>('/market/orderbook', { params: { symbol, depth } }),
  trades: (symbol: string, limit = 50) =>
    api
      .get<{ trades: Trade[] }>('/market/trades', { params: { symbol, limit } })
      .then((d) => (Array.isArray(d?.trades) ? d.trades : Array.isArray(d) ? d : [])),
  snapshot: (symbol?: string) =>
    api.get<MarketSnapshotResponse>(`/market/snapshot${symbol ? '?symbol=' + symbol : ''}`),
  symbolSearch: (q: string) => api.get<{ symbols: string[] }>(`/symbols/search?q=${q}`),
  status: () => api.get<{ status: string }>('/status'),
  // 资金费率/标记价走后端薄代理 /market/funding（P1：不再前端直连
  // fapi.binance.com；拿不到时返回 null，绝不静默 0——0 是假数据）。
  fundingRate: async (symbol: string) => {
    try {
      const d = await api.get<{ funding_rate?: number | null; mark_price?: number | null; next_funding_time?: number }>(
        '/market/funding',
        { params: { symbol }, timeout: 12000 }
      )
      if (d == null || d.funding_rate == null) return null
      return {
        fundingRate: d.funding_rate,
        markPrice: d.mark_price ?? null,
        nextFundingTime: d.next_funding_time ?? 0,
      }
    } catch {
      return null
    }
  },
  markPrice: async (symbol: string) => {
    const d = await marketApi.fundingRate(symbol)
    return d?.markPrice ?? null
  },
}

// ── Orders ──
export const orderApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<{ orders: Order[] }>(`/orders${qs}`).then((d) => d?.orders ?? [])
  },
  place: (order: Record<string, unknown>) => api.post<Order>('/orders', order),
  cancel: (id: string) => api.post<{ success: boolean }>(`/orders/${id}/cancel`),
  cancelAll: () => api.post<{ success: boolean }>('/orders/cancel-all'),
  history: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<{ orders: Order[] }>(`/orders/history${qs}`).then((d) => d?.orders ?? [])
  },
}

// ── Account ──
export const accountApi = {
  balance: (symbol?: string) =>
    api.get<{
      balances: { asset: string; free: number; locked: number; total: number }[]
      currencies?: { currency: string; available: number; total: number }[]
    }>(`/account/balance${symbol ? '?symbol=' + symbol : ''}`),
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
    return api.get<{ trades: Trade[] }>(`/trades${qs}`).then((d) => d?.trades ?? [])
  },
}

// ── 策略版本快照 ──
export interface StrategyVersionItem {
  id: string
  version: number
  note: string
  created_at: number
}
export interface StrategyVersionDetail extends StrategyVersionItem {
  strategy_id: string
  payload: Record<string, unknown>
}
export const strategyVersionApi = {
  list: (strategyId: string) =>
    api
      .get<{ versions: StrategyVersionItem[] }>(`/strategies/configs/${strategyId}/versions`)
      .then((d) => d?.versions ?? []),
  create: (strategyId: string, note?: string) =>
    api.post<StrategyVersionItem>(`/strategies/configs/${strategyId}/versions`, note ? { note } : {}),
  get: (strategyId: string, version: number) =>
    api.get<StrategyVersionDetail>(`/strategies/configs/${strategyId}/versions/${version}`),
  restore: (strategyId: string, version: number) =>
    api.post<{ status: string; restored_version: number }>(
      `/strategies/configs/${strategyId}/versions/${version}/restore`
    ),
}

// ── AI 策略复盘报告 ──
export type AIReviewScopeType = 'strategy' | 'bot'
export interface AIReviewReport {
  id: string
  user_id: number
  scope_type: AIReviewScopeType
  scope_id: string
  period_start: number
  period_end: number
  trades_count: number
  total_pnl: number
  win_rate: number
  max_drawdown: number
  report_text: string
  model: string
  status: 'pending' | 'done' | 'failed'
  error: string
  created_at: number
}
export const aiReviewApi = {
  generate: (data: { scope_type: AIReviewScopeType; scope_id: string; days?: number }) =>
    api.post<{ status: string; msg?: string; report: AIReviewReport }>('/ai/review', data, { timeout: TIMEOUTS.ai }),
  listReports: (params?: { scope_type?: string; scope_id?: string; limit?: number }) =>
    api.get<{ reports: AIReviewReport[] }>('/ai/review/reports', { params }).then((d) => d?.reports ?? []),
  getReport: (id: string) =>
    api.get<{ report: AIReviewReport }>(`/ai/review/reports/${id}`).then((d) => d?.report),
}

// ── AI 交易决策门（对标 QuantDinger JEV 决策门） ──
export type AIGateDecisionKind = 'approve' | 'reject' | 'abstain' | 'bypassed_exit' | 'skipped' | 'fail_open'
export interface AIGateDecision {
  id: string
  user_id: number
  source: string
  symbol: string
  side: string
  order_type: string
  market_type: string
  position_side: string
  quantity: number
  ref_price: number
  notional: number
  decision: AIGateDecisionKind
  allowed: boolean
  confidence: number
  reasons: string[]
  provider: string
  model: string
  latency_ms: number
  fail_open: boolean
  degrade_reason: string
  request_hash: string
  context_json: string
  order_id: string
  executed: boolean
  created_at: number
  updated_at: number
}
export interface AIGateConfig {
  enabled: boolean
  min_confidence: number
  abstain_action: 'allow' | 'block'
  paper_only: boolean
  timeout_seconds: number
  provider: string
  context_bars: number
  excluded_sources: string[]
}
export interface AIGateStats {
  total: number
  evaluated: number
  approved: number
  blocked: number
  abstained: number
  fail_open: number
  bypassed_exit: number
  skipped: number
}
export interface AIGateStatsResponse {
  stats: AIGateStats
  block_rate: number
  fail_open_rate: number
}
export const aiGateApi = {
  listDecisions: (params?: {
    page?: number
    page_size?: number
    decision?: string
    symbol?: string
    source?: string
    fail_open?: string
    days?: number
  }) =>
    api
      .get<{ decisions: AIGateDecision[]; total: number; page: number; page_size: number }>('/ai/gate/decisions', {
        params,
      })
      .then((d) => d ?? { decisions: [], total: 0, page: 1, page_size: 20 }),
  getDecision: (id: string) =>
    api.get<{ decision: AIGateDecision }>(`/ai/gate/decisions/${id}`).then((d) => d?.decision),
  stats: (days?: number) => api.get<AIGateStatsResponse>('/ai/gate/stats', { params: days ? { days } : {} }),
  getConfig: () => api.get<{ config: AIGateConfig }>('/ai/gate/config').then((d) => d?.config ?? null),
  putConfig: (data: Partial<AIGateConfig>) =>
    api.put<{ status: string; config: AIGateConfig }>('/ai/gate/config', data).then((d) => d?.config),
}

// ── 社交市场：开放信号入驻 + 利润分成 + 提现 ──
export interface SocialProviderApply {
  id: number
  user_id: number
  name: string
  description: string
  monthly_fee: number
  profit_share_pct?: number | null
  fee_mode: 'monthly' | 'profit_share' | 'hybrid'
  apply_status: 'pending' | 'approved' | 'rejected'
  apply_note?: string
  approved_at?: number
  created_at: number
}
export interface SocialEarnings {
  today: number
  total: number
  payable: number
  withdrawn: number
  pending_withdrawals: number
  available: number
}
export interface SocialWithdrawal {
  id: string
  amount: number
  chain: string
  address: string
  tx_hash: string
  status: 'pending' | 'paid' | 'rejected'
  admin_note: string
  created_at: number
  processed_at?: number
}
export const socialMarketApi = {
  apply: (data: { name: string; description: string; monthly_fee: number; profit_share_pct?: number | null }) =>
    api.post<{ provider: SocialProviderApply }>('/social/providers/apply', data),
  myProvider: () =>
    api.get<{ provider: SocialProviderApply | null }>('/social/providers/my').then((d) => d ?? { provider: null }),
  earnings: () => api.get<{ earnings: SocialEarnings }>('/social/earnings'),
  withdraw: (data: { amount: number; chain: string; address: string }) =>
    api.post<{ status: string }>('/social/earnings/withdraw', data),
  withdrawals: () => api.get<{ withdrawals: SocialWithdrawal[] }>('/social/earnings/withdrawals'),
  adminWithdrawals: (status?: string) =>
    api
      .get<{ withdrawals: SocialWithdrawal[] }>('/social/admin/withdrawals', { params: { status } })
      .then((d) => d?.withdrawals ?? []),
  adminPayWithdrawal: (id: string, txHash: string) =>
    api.post<{ status: string }>(`/social/admin/withdrawals/${id}/pay`, { tx_hash: txHash }),
  adminRejectWithdrawal: (id: string, note: string) =>
    api.post<{ status: string }>(`/social/admin/withdrawals/${id}/reject`, { note }),
}

// ── Strategies ──
export const strategyApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<StrategyItem[]>(`/strategies/configs${qs}`)
  },
  get: (id: string) => api.get<StrategyItem>(`/strategies/configs/${id}`),
  create: (data: Partial<StrategyItem>) =>
    api.post<{ id: string; success: boolean; forced_paper?: boolean }>('/strategies/configs', data),
  update: (id: string, data: Partial<StrategyItem>) =>
    api.put<{ success: boolean; forced_paper?: boolean }>(`/strategies/configs/${id}`, data),
  delete: (id: string) => api.del<{ success: boolean }>(`/strategies/configs/${id}`),
  start: (id: string) => api.post<{ success: boolean }>(`/strategies/configs/${id}/start`),
  stop: (id: string) => api.post<{ success: boolean }>(`/strategies/configs/${id}/stop`),
  runtime: (id: string) => api.get<StrategyRuntimeResponse>(`/strategies/configs/${id}/runtime`),
  batchStart: (ids: string[]) =>
    api.post<StrategyBatchResult>('/strategies/configs/batch-start', { ids }),
  batchStop: (ids: string[]) =>
    api.post<StrategyBatchResult>('/strategies/configs/batch-stop', { ids }),
  batchClose: (ids: string[]) =>
    api.post<{ success: boolean; closed: number }>('/strategies/configs/batch-close', { ids }),
  batchDelete: (ids: string[]) =>
    api.post<{ success: boolean; deleted: number }>('/strategies/configs/batch-delete', { ids }),
  logs: (strategyId?: string) =>
    api.get<StrategyLog[]>(`/strategies/logs${strategyId ? '?strategy_id=' + strategyId : ''}`),
  clearLogs: (strategyId?: string) =>
    api.del<{ success: boolean }>(`/strategies/logs${strategyId ? '?strategy_id=' + strategyId : ''}`),
  templates: (category = 'spot') => api.get<StrategyTemplate[]>(`/strategies/templates?category=${category}`),
  createTemplate: (data: Partial<StrategyTemplate>) =>
    api.post<{ id: string; success: boolean }>('/strategies/templates', data),
  deleteTemplate: (id: string) => api.del<{ success: boolean }>(`/strategies/templates/${id}`),
  global: () => api.get<{ config: StrategyGlobalConfig }>('/strategies/global').then((d) => d?.config ?? {}),
  saveGlobal: (data: StrategyGlobalConfig) => api.put<{ success: boolean }>('/strategies/global', data),
  spot: () => api.get<StrategyItem[]>('/strategies/spot'),
  contract: () => api.get<StrategyItem[]>('/strategies/contract'),
  ranking: () => api.get<StrategyRanking[]>('/strategies/ranking'),
  paramDefs: (type: string) => api.get<StrategyParamDefs>(`/strategies/param-defs?type=${type}`),
}

// ── Backtest ──
export const backtestApi = {
  run: (config: BacktestRequest) => api.post<BacktestResult>('/backtest/run', config, { timeout: TIMEOUTS.backtest }),
  native: (config: BacktestRequest) =>
    api.post<BacktestResult>('/native/backtest', config, { timeout: TIMEOUTS.backtest }),
}

// ── 因子研究（A6.1） ──
export interface FactorParamSchema {
  name: string
  type: string
  default?: unknown
  min?: number
  max?: number
  description?: string
}

export interface FactorMeta {
  name: string
  version: number
  versions?: number[]
  category: string
  description?: string
  params?: FactorParamSchema[]
  default_params?: Record<string, unknown>
}

export interface FactorValue {
  time: number
  value: number
}

export interface FactorEvaluation {
  factor_name: string
  version: number
  symbol: string
  tf: string
  forward_bars: number
  samples: number
  overall_ic: number
  overall_rank_ic: number
  ic_mean: number
  ic_std: number
  icir: number
  rank_ic_mean: number
  rank_ic_std: number
  rank_icir: number
  ic_positive_pct: number
  ic_series: { time: number; ic: number; rank_ic: number }[]
}

export interface FactorLayer {
  layer: number
  count: number
  avg_forward_ret: number
  total_return: number
  annualized_ret: number
}

export interface FactorLayersResult {
  factor_name: string
  version: number
  symbol: string
  tf: string
  forward_bars: number
  layer_count: number
  lookback: number
  layers: FactorLayer[]
  long_short_total_return: number
  monotonicity: number
  samples: number
  equity_curves: { time: number; equity: number }[][]
}

export interface FactorEvaluationRecord {
  id: number
  user_id: number
  kind: string
  factor_name: string
  factor_version: number
  category: string
  symbol: string
  tf: string
  forward_bars: number
  params_json: string
  samples: number
  result_json: string
  created_at: number
}

export interface FactorEvaluateRequest {
  name: string
  symbol: string
  tf?: string
  from?: string
  to?: string
  limit?: number
  forward_bars?: number
  ic_window?: number
  layer_count?: number
  lookback?: number
  params?: Record<string, unknown>
  version?: number
  save?: boolean
}

export const factorApi = {
  list: () => api.get<{ factors: FactorMeta[]; categories: string[]; count: number }>('/factors'),
  values: (name: string, query: { symbol: string; tf?: string; limit?: number; from?: string; to?: string; params?: string }) =>
    api.get<{
      factor: string
      version: number
      category: string
      symbol: string
      tf: string
      bars_used: number
      source: string
      values: FactorValue[]
      closes: { time: number; close: number }[]
      default_params?: Record<string, unknown>
    }>(`/factors/${encodeURIComponent(name)}/values`, { params: query }),
  evaluate: (req: FactorEvaluateRequest) =>
    api.post<{ evaluation: FactorEvaluation; bars_used: number; source: string }>('/factors/evaluate', req, { timeout: TIMEOUTS.backtest }),
  layers: (req: FactorEvaluateRequest) =>
    api.post<{ result: FactorLayersResult; bars_used: number; source: string }>('/factors/layers', req, { timeout: TIMEOUTS.backtest }),
  evaluations: (factor?: string, limit = 100) =>
    api.get<{ evaluations: FactorEvaluationRecord[] }>(
      `/factors/evaluations${factor ? '?factor=' + encodeURIComponent(factor) + '&' : '?'}limit=${limit}`
    ),
}

// ── 组合回测（A6.2） ──
export interface PortfolioLegConfig {
  strategy_type: string
  symbol: string
  weight: number
  params?: Record<string, unknown>
}

export interface PortfolioBacktestRequest {
  name: string
  timeframe?: string
  start?: string
  end?: string
  initial_capital?: number
  rebalance?: 'none' | 'daily' | 'weekly' | 'monthly'
  legs: PortfolioLegConfig[]
}

export interface PortfolioLegResult {
  strategy_type: string
  symbol: string
  weight: number
  initial_allocation: number
  final_value: number
  total_return_pct: number
  contribution_pct: number
  trades: number
  win_rate: number
  sharpe_ratio: number
  max_drawdown_pct: number
}

export interface PortfolioDriftRecord {
  time: number
  trigger: string
  weights_before: Record<string, number>
  weights_after: Record<string, number>
}

export interface PortfolioBacktestResult {
  id: string
  name: string
  report: BacktestReport & { equity_sampled?: { timestamp: number; equity: number }[] }
  equity_curve: { timestamp: number; equity: number }[]
  drift: PortfolioDriftRecord[]
  legs: PortfolioLegResult[]
  duration_ms: number
}

export interface PortfolioBacktestRecord {
  id: string
  user_id: number
  name: string
  timeframe: string
  rebalance: string
  start_time: number
  end_time: number
  initial_capital: number
  final_equity: number
  total_return_pct: number
  max_drawdown_pct: number
  sharpe_ratio: number
  sortino_ratio: number
  calmar_ratio: number
  win_rate: number
  profit_factor: number
  total_trades: number
  legs_json: string
  result_json: string
  duration_ms: number
  created_at: number
}

export const portfolioBacktestApi = {
  run: (req: PortfolioBacktestRequest) =>
    api.post<PortfolioBacktestResult>('/backtests/portfolio', req, { timeout: TIMEOUTS.backtest }),
  list: (limit = 50) => api.get<{ backtests: PortfolioBacktestRecord[] }>(`/backtests/portfolio?limit=${limit}`),
  get: (id: string) =>
    api.get<{
      id: string
      name: string
      timeframe: string
      rebalance: string
      start_time: number
      end_time: number
      initial_capital: number
      final_equity: number
      metrics: Record<string, number>
      legs: PortfolioLegConfig[]
      result: {
        report?: BacktestReport
        equity_curve?: { timestamp: number; equity: number }[]
        drift?: PortfolioDriftRecord[]
        legs?: PortfolioLegResult[]
      }
      duration_ms: number
      created_at: number
    }>(`/backtests/portfolio/${encodeURIComponent(id)}`),
  delete: (id: string) => api.del<{ status: string }>(`/backtests/portfolio/${encodeURIComponent(id)}`),
}

// ── AI ──
export const aiApi = {
  snapshot: (symbol?: string) => api.get<AISnapshot>(`/ai/snapshot${symbol ? '?symbol=' + symbol : ''}`),
  klines: (symbol: string, interval?: string) =>
    api.get<{ klines: KlineBar[] }>(`/ai/klines?symbol=${symbol}&interval=${interval || '1h'}`),
  generate: (data: AIGenerateRequest) => api.post<AIGenerateResponse>('/ai/generate', data, { timeout: TIMEOUTS.ai }),
  multiAgent: (data: AIMultiAgentRequest) =>
    api.post<AIMultiAgentResponse>('/ai/multi-agent', data, { timeout: TIMEOUTS.ai }),
  backtest: (data: BacktestRequest) => api.post<BacktestResult>('/ai/backtest', data, { timeout: TIMEOUTS.backtest }),
  optimize: (data: Record<string, unknown>) =>
    api.post<{
      iteration_history: {
        iteration: number
        sharpe: number
        return: number
        max_drawdown?: number
        win_rate?: number
        total_trades?: number
        params?: Record<string, unknown>
      }[]
      best_sharpe?: number
      best_return?: number
      symbol?: string
      strategy_type?: string
    }>('/ai/optimize', data, { timeout: TIMEOUTS.ai }),
  deploy: (data: Record<string, unknown>) => api.post<{ success: boolean; strategy_id: string }>('/ai/deploy', data),
  analyze: (data: Record<string, unknown>) =>
    api.post<AIAnalysisResult>('/ai/analyze', data, { timeout: TIMEOUTS.analysis }),
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

// ── Agent Chat (SSE streaming, POST /agent/chat) ──
// 与 indicatorApi.aiGenerateStream 同样的 fetch + ReadableStream 模式：
// axios 拦截器管不到 SSE，401/错误码在下方手工处理。
export interface AgentChatMessage {
  role: 'user' | 'assistant' | 'system'
  content: string
}

export interface AgentToolCall {
  name: string
  args_summary?: string
  status: 'running' | 'done'
  result_summary?: string
}

export interface AgentChatDoneMessage {
  content: string
  tool_calls?: AgentToolCall[]
}

export interface AgentChatHandlers {
  onDelta?: (delta: string) => void
  onToolCall?: (toolCall: AgentToolCall) => void
  onDone?: (message: AgentChatDoneMessage) => void
  onError?: (message: string) => void
}

export const agentChatApi = {
  chat: (
    messages: AgentChatMessage[],
    handlers: AgentChatHandlers
  ): { abort: () => void; promise: Promise<void> } => {
    const controller = new AbortController()
    const token = localStorage.getItem('xt-token') || ''
    const url = `${API_BASE_URL}/agent/chat`

    const promise = (async (): Promise<void> => {
      let response: Response
      try {
        response = await fetch(url, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${token}`,
            'Access-Token': token,
          },
          body: JSON.stringify({ messages, stream: true }),
          signal: controller.signal,
        })
      } catch (e) {
        // 用户主动停止（abort）不算错误，静默结束
        if (controller.signal.aborted) return
        handlers.onError?.('网络错误，请检查连接')
        throw e instanceof Error ? e : new Error(String(e))
      }

      if (!response.ok) {
        const text = await response.text()
        if (response.status === 401) {
          // 与 axios 401 拦截器行为保持一致：清 token 跳登录
          if (!isRedirectingToLogin) {
            isRedirectingToLogin = true
            localStorage.removeItem('xt-token')
            localStorage.removeItem('xt-auth')
            if (window.location.pathname !== '/login') {
              window.location.href = '/login'
            }
            setTimeout(() => {
              isRedirectingToLogin = false
            }, 3000)
          }
          handlers.onError?.('登录已过期，请重新登录')
          throw new ApiError('登录已过期，请重新登录', 401, 'UNAUTHORIZED')
        }
        handlers.onError?.(`HTTP ${response.status}: ${text || '请求失败'}`)
        throw new ApiError(text || `请求失败 (${response.status})`, response.status)
      }

      const reader = response.body?.getReader()
      if (!reader) {
        handlers.onError?.('No response body')
        throw new Error('No response body')
      }

      const decoder = new TextDecoder()
      let buffer = ''
      let currentEvent = ''
      let currentData = ''

      const flushEvent = () => {
        if (!currentEvent) {
          currentEvent = 'message'
        }
        if (currentData === '') {
          currentEvent = ''
          return
        }
        // SSE 结束标记，不是消息内容
        if (currentData === '[DONE]') {
          currentEvent = ''
          currentData = ''
          return
        }
        try {
          if (currentEvent === 'message') {
            // 默认事件 = 文本分片，直接追加
            handlers.onDelta?.(currentData)
          } else if (currentEvent === 'tool_call') {
            handlers.onToolCall?.(JSON.parse(currentData))
          } else if (currentEvent === 'done') {
            handlers.onDone?.(JSON.parse(currentData))
          } else if (currentEvent === 'error') {
            let msg = currentData
            try {
              const parsed = JSON.parse(currentData)
              msg = parsed?.message || parsed?.error || msg
            } catch {
              /* 纯文本错误 */
            }
            handlers.onError?.(msg)
          }
        } catch (e) {
          if (currentEvent === 'message') handlers.onDelta?.(currentData)
          else if (currentEvent === 'done') handlers.onDone?.({ content: currentData })
        }
        currentEvent = ''
        currentData = ''
      }

      const feedLine = (line: string) => {
        if (line.startsWith('event:')) {
          currentEvent = line.slice(6).trim()
        } else if (line.startsWith('data:')) {
          if (currentData !== '') currentData += '\n'
          currentData += line.slice(5).trim()
        } else if (line.trim() === '') {
          flushEvent()
        }
      }

      try {
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() || ''
          for (const line of lines) feedLine(line)
        }
        // 处理残留 buffer（服务端可能不以空行结尾）
        if (buffer) {
          for (const line of buffer.split('\n')) feedLine(line)
        }
        flushEvent()
        // done 事件缺省时以流结束为完成——promise resolve 即代表流正常结束
      } catch (e) {
        if (controller.signal.aborted) return
        const msg = e instanceof Error ? e.message : 'Stream error'
        handlers.onError?.(msg)
        throw e instanceof Error ? e : new Error(msg)
      }
    })()

    return {
      abort: () => controller.abort(),
      promise,
    }
  },
}

// ── Config (raw store config) ──
export const configApi = {
  get: () => api.get<RawConfig>('/config'),
  save: (data: RawConfig) => api.put<RawConfig>('/config', data),
  exchangeTest: (data: ExchangeSettings) => api.post<ExchangeTestResult>('/exchange/test', data),
  saveExchangeCredentials: (data: { name: string; api_key?: string; secret?: string; passphrase?: string }) =>
    api.put<{ success: boolean; name: string; has_credentials: boolean }>('/config/exchanges/credentials', data),
  exchangeSave: (data: ExchangeSettings) => api.post<ExchangeSaveResult>('/exchange/save', data),
  currencyGet: () => api.get<{ currency: string }>('/settings/currency'),
  currencySet: (currency: string) => api.put<{ currency: string }>('/settings/currency', { currency }),
  aiTest: (data: Record<string, unknown>) => api.post<{ success: boolean }>('/ai/test', data),
  aiSave: (data: Record<string, unknown>) => api.post<{ success: boolean }>('/ai/save', data),
  // Dynamic config endpoints (backend-driven)
  getMarkets: () =>
    api.get<{
      symbols: Array<{ symbol: string; base: string; quote: string; precision: { price: number; quantity: number } }>
    }>('/config/markets'),
  getIndices: () =>
    api.get<{
      heatmap: Record<string, string[]>
      global_indices: Array<{ symbol: string; name: string; region: string }>
    }>('/config/indices'),
  getExchanges: () =>
    api.get<{ exchanges: Array<{ key: string; label: string; status: string; supports: string[] }> }>(
      '/config/exchanges'
    ),
  getAIModels: () =>
    api.get<{ providers: Array<{ key: string; label: string; models: string[]; baseUrl: string }> }>(
      '/config/ai-models'
    ),
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
  publish: (data: Partial<StrategyCommunityItem>) =>
    api.post<{ success: boolean }>('/community/strategies/publish', data),
  comment: (id: number, content: string) =>
    api.post<{ success: boolean; comment_id?: number }>(`/community/strategies/${id}/comment`, { content }),
  rate: (id: number, rating: number) => api.post<{ success: boolean }>(`/community/strategies/${id}/rate`, { rating }),
  leaderboard: (sortBy?: string, limit?: number) =>
    api.get<StrategyCommunityItem[]>('/community/strategies/leaderboard', { params: { sort_by: sortBy, limit } }),
  trending: (limit?: number) =>
    api.get<StrategyCommunityItem[]>('/community/strategies/trending', { params: { limit } }),
  overfit: (id: number) => api.get<OverfitResult>(`/community/strategies/${id}/overfit`),
}

// ── ML ──
export const mlApi = {
  train: (config: Record<string, unknown>) => api.post<MLTrainResult>('/ml/train', config, { timeout: 120000 }),
  predict: (data: Record<string, unknown>) => api.post<{ prediction: number; confidence: number }>('/ml/predict', data),
  list: () => api.get<{ models: MLModelInfo[] }>('/ml/models').then((d) => d?.models ?? []),
  detail: (id: string) => api.get<MLModelInfo>(`/ml/models/${id}`),
  deleteModel: (id: string) => api.del<{ success: boolean }>(`/ml/models/${id}`),
  importance: (id: string) =>
    api.get<{ importance: { feature: string; score: number }[] }>(`/ml/models/${id}/importance`),
  generateFeatures: (data: Record<string, unknown>) => api.post<{ features: string[] }>('/ml/features', data),
  health: () => api.get<{ status: string }>('/ml/health'),
  deploy: (data: Record<string, unknown>) => api.post<{ success: boolean; strategy_id: string }>('/ml/deploy', data),
  strategyModels: () => api.get<{ models: MLModelInfo[] }>('/ml/strategy-models').then((d) => d?.models ?? []),
  // ── 训练闭环（自动重训任务 + 运行历史 + 状态汇总）──
  retrainJobs: () => api.get<{ jobs: MLRetrainJob[] }>('/ml/retrain-jobs').then((d) => d?.jobs ?? []),
  createRetrainJob: (data: { model_name: string; feature_set?: Record<string, unknown>; interval_minutes?: number }) =>
    api.post<MLRetrainJob>('/ml/retrain-jobs', data),
  updateRetrainJob: (id: number, data: { interval_minutes?: number; active?: boolean }) =>
    api.put<MLRetrainJob>(`/ml/retrain-jobs/${id}`, data),
  runRetrainJob: (id: number) =>
    api.post<{ run: { status: string; error: string } }>(`/ml/retrain-jobs/${id}/run`, {}, { timeout: 300000 }),
  trainingRuns: (limit = 20) =>
    api.get<{ runs: MLTrainingRun[] }>('/ml/training-runs', { params: { limit } }).then((d) => d?.runs ?? []),
  loopStatus: () => api.get<MLLoopStatus>('/ml/loop-status'),
}

// ── ML 训练闭环类型 ──
export interface MLRetrainJob {
  id: number
  user_id: number
  model_name: string
  feature_set: string
  interval_minutes: number
  last_run_at: number
  last_status: string
  last_error: string
  created_at: number
  active: boolean
}

export interface MLTrainingRun {
  id: number
  job_id: number
  model_name: string
  trigger: string
  trainer: string
  status: string
  error: string
  symbol: string
  interval: string
  bars_loaded: number
  train_samples: number
  test_samples: number
  feature_count: number
  metrics_json: string
  model_version: string
  duration_ms: number
  created_at: number
}

export interface MLLoopStatus {
  ml_server: {
    managed: boolean
    running?: boolean
    adopted?: boolean
    pid?: number
    restarts?: number
    healthy?: boolean
    reachable?: boolean
    url: string
    last_error?: string
  }
  retrainer: {
    running: boolean
    enabled: boolean
    check_interval_sec?: number
    fallback_mode?: string
  }
  jobs: {
    id: number
    model_name: string
    active: boolean
    interval_minutes: number
    last_run_at: number
    last_status: string
    last_error: string
    next_run_at: number
  }[]
  models_loaded: {
    model_id: string
    version: string
    model_type: string
    features: number
    trees: number
    loaded_at: number
  }[]
  last_run?: MLTrainingRun | null
  drift: {
    model_id: string
    has_baseline: boolean
    checked_at: number
    drifted: boolean
    overall_psi?: number
    drifted_features?: string[]
  }[]
}

// ── RL (Reinforcement Learning) ──
export const rlApi = {
  train: (config: Record<string, unknown>) =>
    api.post<RLTrainResult | { job_id: string; status: string; message: string }>('/rl/train', config, {
      timeout: 300000,
    }),
  predict: (data: Record<string, unknown>) => api.post<RLPredictResult>('/rl/predict', data),
  evaluate: (data: Record<string, unknown>) => api.post<RLEvalResult>('/rl/evaluate', data),
  list: () => api.get<{ models: RLModelInfo[] }>('/rl/models').then((d) => d?.models ?? []),
  deleteModel: (id: string) => api.del<{ success: boolean }>(`/rl/models/${id}`),
  getJob: (id: string) => api.get<RLJob>(`/rl/jobs/${id}`),
  cancelJob: (id: string) => api.post<{ success: boolean }>(`/rl/jobs/${id}/cancel`),
  getWorkerStatus: () => api.get<RLWorkerStatus>('/rl/worker/status'),
  startWorker: (config: Record<string, unknown>) =>
    api.post<{ success: boolean; message: string; worker_pid?: number; command?: string; error?: string }>(
      '/rl/worker/start',
      config
    ),
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
  config: (data: { protections: ProtectionConfigItem[] }) => api.post<{ success: boolean }>('/protection/config', data),
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
  configure: (data: PairlistConfig) => api.post<PairlistConfig>('/pairlist/config', data),
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

// ── Ladder Smart Orders（阶梯智能单）──
export const ladderApi = {
  create: (data: LadderCreateRequest) =>
    api.post<{ status: string; ladder: LadderOrder }>('/orders/ladder', data).then((d) => d?.ladder),
  list: (all = false) =>
    api
      .get<{ orders: LadderOrder[]; count: number }>(`/orders/ladder${all ? '?all=1' : ''}`)
      .then((d) => d?.orders ?? []),
  get: (id: string) => api.get<LadderOrder>(`/orders/ladder/${id}`),
  amend: (id: string, data: LadderAmendRequest) =>
    api.put<{ status: string; ladder: LadderOrder }>(`/orders/ladder/${id}`, data).then((d) => d?.ladder),
  cancel: (id: string) =>
    api.post<{ status: string; ladder: LadderOrder }>(`/orders/ladder/${id}/cancel`).then((d) => d?.ladder),
  flatten: (id: string) =>
    api.post<{ status: string; ladder: LadderOrder }>(`/orders/ladder/${id}/flatten`).then((d) => d?.ladder),
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
    api
      .get<{ opportunity: ArbitrageOpportunity | null }>('/arbitrage/opportunity')
      .then((r) => (r.opportunity ? [r.opportunity] : [])),

  positions: () => api.get<{ positions: ArbitragePosition[] }>('/arbitrage/positions').then((r) => r.positions ?? []),

  history: (limit?: number) =>
    api
      .get<{ history: ArbitrageHistoryItem[] }>('/arbitrage/history', { params: { limit } })
      .then((r) => r.history ?? []),

  exchanges: () => api.get<{ registered_count: number; exchanges: string[] }>('/arbitrage/exchanges'),

  registerExchange: (data: Partial<ArbitrageExchange>) =>
    api.post<{ status: string; exchange: string }>('/arbitrage/exchanges', data),

  unregisterExchange: (name: string) =>
    api.del<{ status: string; exchange: string }>(`/arbitrage/exchanges/${name}`),

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

  failPosition: (id: string) => api.post<{ status: string }>(`/arbitrage/positions/${id}/fail`),
}

// ── Triangular Arbitrage ──

export const triangularApi = {
  config: () => api.get<{ config: TriangularConfig }>('/triangular/config').then((r) => r?.config ?? null),

  updateConfig: (data: TriangularConfig) =>
    api.post<{ config: TriangularConfig }>('/triangular/config', data).then((r) => r.config),

  start: () => api.post<{ status: string }>('/triangular/start'),
  stop: () => api.post<{ status: string }>('/triangular/stop'),
  status: () => api.get<{ running: boolean; stats: Record<string, unknown> }>('/triangular/status'),
  performance: () => api.get<TriangularPerformance>('/triangular/performance'),

  opportunity: () =>
    api
      .get<{ opportunity: TriangularOpportunity | null }>('/triangular/opportunity')
      .then((r) => (r.opportunity ? [r.opportunity] : [])),

  positions: () => api.get<{ positions: TriangularTrade[] }>('/triangular/positions').then((r) => r.positions ?? []),

  history: (limit?: number) =>
    api.get<{ history: TriangularTrade[] }>('/triangular/history', { params: { limit } }).then((r) => r.history ?? []),

  execute: (data: { exchange: string; cycle: string[]; start_qty: number }) =>
    api.post<{ status: string; opportunity: TriangularOpportunity }>('/triangular/execute', data),

  closePosition: (id: string) => api.post<{ status: string }>(`/triangular/positions/${id}/close`),

  failPosition: (id: string) => api.post<{ status: string }>(`/triangular/positions/${id}/fail`),
}

// ── Hyperopt ──
export const hyperoptApi = {
  start: (data: Record<string, unknown>) => api.post<{ job_id: string }>('/hyperopt/start', data, { timeout: 600000 }),
  jobs: () => api.get<{ jobs: HyperoptJob[] }>('/hyperopt/jobs').then((d) => d?.jobs ?? []),
  job: (id: string) => api.get<HyperoptJob>(`/hyperopt/jobs/${id}`),
  cancel: (id: string) => api.post<{ success: boolean }>(`/hyperopt/jobs/${id}/cancel`),
  delete: (id: string) => api.del<{ success: boolean }>(`/hyperopt/jobs/${id}`),
  spaces: (strategy?: string) =>
    api.get<{ spaces: HyperoptSpace[] }>('/hyperopt/spaces', { params: { strategy } }).then((d) => d?.spaces ?? []),
  // protection 空间维度（freqtrade --spaces protection 对标）
  protectionSpaces: (protections?: string[]) =>
    api
      .get<{ spaces: HyperoptSpace[] }>('/hyperopt/spaces', {
        params: { space: 'protection', protections: protections?.join(',') },
      })
      .then((d) => d?.spaces ?? []),
  epochs: (params?: Record<string, string>) =>
    api
      .get<{ epochs: HyperoptEpoch[]; count: number }>('/hyperopt/epochs', { params })
      .then((d) => d?.epochs ?? []),
  epoch: (id: string) => api.get<HyperoptEpoch>(`/hyperopt/epochs/${id}`),
  applyEpoch: (id: string) => api.post<HyperoptEpochApplyResult>(`/hyperopt/epochs/${id}/apply`),
}

// ── Notifications ──
export const notificationApi = {
  list: (params?: { limit?: number; offset?: number; unread?: boolean }) =>
    api
      .get<{ notifications: NotificationItem[]; total: number }>('/notifications', { params })
      .then((d) => d?.notifications ?? []),
  unreadCount: () => api.get<{ count: number }>('/notifications/unread-count').then((d) => d?.count ?? 0),
  markRead: (id: number) => api.post<{ success: boolean }>(`/notifications/${id}/read`),
  markAllRead: () => api.post<{ success: boolean }>('/notifications/read-all'),
  clear: () => api.del<{ success: boolean }>('/notifications'),
}

// ── Notify Routes ──
export const notifyRouteApi = {
  list: () => api.get<{ rules: NotifyRoute[] }>('/notify/routes').then((d) => d?.rules ?? []),
  save: (rule: Partial<NotifyRoute>) => api.post<{ id: string }>('/notify/routes', rule),
  delete: (id: string) => api.del<{ success: boolean }>(`/notify/routes/${id}`),
  test: (channel: string, message?: string) =>
    api.post<{ success: boolean }>('/notify/test', { channel, message: message || '测试消息' }),
}

// ── Indicators ──
export const indicatorApi = {
  // --- New contract-based API ---
  parse: (code: string) =>
    api.post<{ success: boolean; params?: Record<string, unknown>; error?: string }>('/indicator/parse', { code }),
  validate: (code: string) =>
    api.post<{ success: boolean; error?: string; params?: Record<string, unknown> }>('/indicator/validate', { code }),
  save: (data: Record<string, unknown>) => api.post<{ id: number; success: boolean }>('/indicator/save', data),
  list: () => api.get<{ items: IndicatorItem[]; total: number }>('/indicator/list').then((d) => d?.items ?? []),
  get: (id: number) => api.get<IndicatorDetail>(`/indicator/${id}`),
  delete: (id: number) => api.del<{ success: boolean }>(`/indicator/${id}`),
  applyParamDefaults: (code: string, indicatorParams: Record<string, unknown>) =>
    api
      .post<{ params: Record<string, unknown> }>('/indicator/applyParamDefaults', { code, indicatorParams })
      .then((d) => d?.params ?? {}),

  // --- Legacy API (keep for compatibility) ---
  listLegacy: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<IndicatorItem[]>(`/indicator/getIndicators${qs}`).then((d) => d ?? [])
  },
  create: (data: Partial<IndicatorDetail>) => api.post<IndicatorDetail>('/indicator/saveIndicator', data),
  update: (id: number, data: Partial<IndicatorDetail>) => api.put<IndicatorDetail>(`/indicator/${id}`, data),
  saveAs: (data: Partial<IndicatorDetail>) =>
    api.post<IndicatorDetail>('/indicator/saveIndicator', { ...data, is_new_copy: true }),
  publish: (id: number, data: Record<string, unknown>) =>
    api.post<{ success: boolean }>('/indicator/publish', { id, ...data }),
  decrypt: (userId: number, indicatorId: number) =>
    api.post<{ key: string; success: boolean }>('/indicator/getDecryptKey', {
      user_id: userId,
      indicator_id: indicatorId,
    }),
  backtest: (data: Record<string, unknown>) =>
    api.post<IndicatorBacktestResult>('/indicator/backtest', data, { timeout: TIMEOUTS.backtest }),
  aiGenerate: (data: Record<string, unknown>) =>
    api.post<IndicatorAIGenerateResult>('/indicator/ai-generate', data, { timeout: TIMEOUTS.ai }),
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
          Authorization: `Bearer ${token}`,
          'Access-Token': token,
        },
        body: JSON.stringify(data),
      })
        .then(async (response) => {
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
            if (!currentEvent) {
              currentEvent = 'message'
            }
            if (currentData === '') {
              currentEvent = ''
              return
            }
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
        })
        .catch((err: unknown) => {
          const msg = err instanceof Error ? err.message : 'Network error'
          handlers.onError?.(msg)
          reject(err)
        })
    })
  },
  run: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/indicator/execute', data),
  execute: (data: Record<string, unknown>) => api.post<IndicatorRunResult>('/indicator/execute', data),
  watchlist: () => api.get<{ items: IndicatorItem[] }>('/watchlist').then((d) => d?.items ?? []),
  addWatchlist: (data: Record<string, unknown>) => api.post<{ success: boolean }>('/watchlist', data),
  kline: (params: Record<string, string | number>) =>
    api.get<{ klines: KlineBar[] }>('/indicator/kline', { params }).then((d) => d?.klines ?? []),
  experiment: {
    run: (data: Record<string, unknown>) =>
      api.post<IndicatorRunResult>('/experiment/run', data, { timeout: TIMEOUTS.backtest }),
    sensitivity: (data: Record<string, unknown>) =>
      api.post<IndicatorRunResult>('/experiment/sensitivity', data, { timeout: TIMEOUTS.backtest }),
    walkForward: (data: Record<string, unknown>) =>
      api.post<IndicatorRunResult>('/experiment/walk-forward', data, { timeout: TIMEOUTS.backtest }),
    aiOptimize: (data: Record<string, unknown>) =>
      api.post<IndicatorRunResult>('/experiment/ai-optimize', data, { timeout: TIMEOUTS.backtest }),
    structuredTune: (data: Record<string, unknown>) =>
      api.post<IndicatorRunResult>('/experiment/structured-tune', data, { timeout: TIMEOUTS.backtest }),
  },
}

// ── Social Trading ──
export const socialApi = {
  providers: () => api.get<{ providers: any[] }>('/social/providers').then((d) => d?.providers ?? []),
  follow: (providerId: number, followerId: number) =>
    api.post<{ success: boolean }>(`/social/providers/${providerId}/follow`, undefined, {
      params: { follower_id: followerId },
    }),
  unfollow: (providerId: number, followerId: number) =>
    api.post<{ success: boolean }>(`/social/providers/${providerId}/unfollow`, undefined, {
      params: { follower_id: followerId },
    }),
  signals: (providerId?: number, limit?: number) =>
    api
      .get<{ signals: any[] }>('/social/signals', { params: { provider_id: providerId, limit } })
      .then((d) => d?.signals ?? []),
  publishSignal: (data: Record<string, unknown>) => api.post<{ signal: any }>('/social/signals', data),
  followerConfigs: (followerId: number) =>
    api
      .get<{ configs: any[] }>('/social/followers/configs', { params: { follower_id: followerId } })
      .then((d) => d?.configs ?? []),
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
  whaleAlerts: (minUSD?: number) =>
    api.get<{ alerts: any[] }>('/onchain/whale-alerts', { params: { min_usd: minUSD } }).then((d) => d?.alerts ?? []),
  btcSignal: () => api.get<any>('/onchain/signal/btc'),
  ethSignal: () => api.get<any>('/onchain/signal/eth'),
}

// ── Data Providers（外部数据生态：情绪/宏观/新闻/热力图/经济日历）──
export const dataProviderApi = {
  sentiment: () => api.get<SentimentResponse>('/dataproviders/sentiment'),
  macro: () => api.get<ProviderResult<MacroData>>('/dataproviders/macro'),
  news: (symbol?: string) =>
    api.get<ProviderResult<NewsData>>('/dataproviders/news', { params: symbol ? { symbol } : {} }),
  heatmap: () => api.get<ProviderResult<HeatmapData>>('/dataproviders/heatmap'),
  calendar: () => api.get<ProviderResult<CalendarData>>('/dataproviders/calendar'),
  sources: () => api.get<{ sources: SourceHealth[] }>('/dataproviders/sources').then((d) => d?.sources ?? []),
}

// ── Community ──
export const communityApi = {
  market: (params?: {
    page?: number
    page_size?: number
    keyword?: string
    pricing_type?: string
    sort_by?: string
  }) => {
    const qs = params
      ? '?' +
        new URLSearchParams(Object.entries(params).filter(([_, v]) => v !== undefined) as [string, string][]).toString()
      : ''
    return api.get<{ items: IndicatorItem[]; total: number }>(`/community/indicators${qs}`).then((d) => d?.items ?? [])
  },
  publish: (data: { indicatorId: number; pricingType?: string; price?: number }) =>
    api.post<{ success: boolean }>('/community/publish', data),
  purchase: (id: number) => api.post<{ success: boolean; order_id?: string }>(`/community/purchase/${id}`, {}),
  comments: (id: number, page?: number, pageSize?: number) =>
    api
      .get<{
        comments: CommunityComment[]
        total: number
      }>(`/community/comments/${id}?page=${page || 1}&page_size=${pageSize || 20}`)
      .then((d) => d?.comments ?? []),
  addComment: (id: number, data: { rating: number; content: string }) =>
    api.post<{ success: boolean; comment_id?: number }>(`/community/comments/${id}`, data),
}

// ── Admin ──
export const adminApi = {
  users: () => api.get<AdminUser[]>('/admin/users').then((d) => d ?? []),
  user: (id: string) => api.get<AdminUser>(`/admin/users/${id}`),
  updateUser: (id: string, data: Partial<AdminUser>) => api.put<{ success: boolean }>(`/admin/users/${id}`, data),
  stats: () => api.get<AdminStats>('/admin/stats'),
  enhancedStats: () => api.get<AdminStats>('/admin/stats'),
  auditLog: (params?: { limit?: number; offset?: number }) =>
    api.get<{ logs: AdminAuditLog[]; total: number }>('/admin/audit-log', { params }),
}

// ── Agent (admin) ──
export const agentAdminApi = {
  tokens: () => agentApi.tokens(),
  createToken: (data: Partial<AgentToken>) => agentApi.createToken(data),
  deleteToken: (id: string) => agentApi.deleteToken(id),
  auditLog: () => api.get<AdminAuditLog[]>('/agent/audit-log').then((d) => d ?? []),
}

// ── Strategy Config (Martin / WallStreet) ──
export const strategyConfigApi = {
  createMartin: (config: MartinConfig) =>
    axiosInstance.post<{ id: string; success: boolean }>('/strategies/martin', config),
  createWallStreet: (config: WallStreetConfig) =>
    axiosInstance.post<{ id: string; success: boolean }>('/strategies/wallstreet', config),
  getMartinConfigs: () => axiosInstance.get<MartinConfig[]>('/strategies/martin'),
  getWallStreetConfigs: () => axiosInstance.get<WallStreetConfig[]>('/strategies/wallstreet'),
  updateMartin: (id: string, config: MartinConfig) =>
    axiosInstance.put<{ success: boolean }>(`/strategies/martin/${id}`, config),
  updateWallStreet: (id: string, config: WallStreetConfig) =>
    axiosInstance.put<{ success: boolean }>(`/strategies/wallstreet/${id}`, config),
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
  // GET /executor/stats — 信号统计：KPI、近 30 日盈亏曲线、按交易对汇总
  getStats: () =>
    axiosInstance
      .get<ExecutorStats | { success: boolean; data: ExecutorStats }>('/executor/stats')
      .then((r) => unwrapEnvelope(r.data)),
}

// ── AI Robot ──
// ai-robot 配置/模型路由已接入（见 handler/ai_robot.go）；状态与信号走 /ai/*。
export const aiRobotApi = {
  getConfig: () =>
    axiosInstance
      .get<AIRobotConfig | { success: boolean; data: AIRobotConfig }>('/ai-robot/config')
      .then((r) => unwrapEnvelope(r.data)),
  saveConfig: (config: AIRobotConfig) =>
    axiosInstance.post<AIRobotConfig | { success: boolean; data: AIRobotConfig }>('/ai-robot/config', config),
  getStatus: () =>
    axiosInstance
      .get<AIStatus | { success: boolean; data: AIStatus }>('/ai/status')
      .then((r) => unwrapEnvelope(r.data)),
  getSignals: (params?: { limit?: number; symbol?: string }) =>
    axiosInstance
      .get<{ signals: AISignal[] } | { success: boolean; data: { signals: AISignal[] } }>('/ai/signals', { params })
      .then((r) => unwrapEnvelope(r.data)?.signals || []),
  getModels: () =>
    axiosInstance
      .get<{ models: string[] } | { success: boolean; data: { models: string[] } }>('/ai-robot/models')
      .then((r) => unwrapEnvelope(r.data)?.models || []),
}

// ── Contract Trading ──
export const contractApi = {
  // 杠杆无持久化/账户数据源：可能为 null（显示 "--"）
  getLeverage: () => axiosInstance.get<{ leverage: number | null }>('/contract/leverage'),
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
  catalog: () => api.get<AIBotCatalogItem[]>('/ai-bots/catalog').then((d) => d ?? []),
  catalogItem: (id: string) => api.get<AIBotCatalogItem>(`/ai-bots/catalog/${id}`),

  // Instances
  list: () => api.get<AIBotInstance[]>('/ai-bots/instances').then((d) => d ?? []),
  get: (id: string) => api.get<AIBotInstance>(`/ai-bots/instances/${id}`),
  create: (data: AIBotCreateRequest) => api.post<AIBotInstance>('/ai-bots/instances', data),
  update: (id: string, data: Partial<AIBotInstance>) => api.put<AIBotInstance>(`/ai-bots/instances/${id}`, data),
  delete: (id: string) => api.del<{ id: string }>(`/ai-bots/instances/${id}`),
  start: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/start`),
  pause: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/pause`),
  resume: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/resume`),
  stop: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/stop`),
  clone: (id: string) => api.post<AIBotInstance>(`/ai-bots/instances/${id}/clone`),
  batchStart: (ids: string[]) =>
    api.post<{ success: boolean; started: number }>('/ai-bots/instances/batch-start', { ids }),
  batchStop: (ids: string[]) =>
    api.post<{ success: boolean; stopped: number }>('/ai-bots/instances/batch-stop', { ids }),
  batchDelete: (ids: string[]) =>
    api.post<{ success: boolean; deleted: number }>('/ai-bots/instances/batch-delete', { ids }),

  // Analytics
  analytics: (id: string) => api.get<AIBotAnalytics>(`/ai-bots/instances/${id}/analytics`),
  trades: (id: string, limit = 50) =>
    api.get<{ bot: AIBotInstance; trades: AIBotTrade[] }>(`/ai-bots/instances/${id}/trades?limit=${limit}`),

  // Subscriptions
  subscriptions: () => api.get<AIBotSubscription[]>('/ai-bots/subscriptions').then((d) => d ?? []),
  subscribe: (data: Partial<AIBotSubscription>) => api.post<{ id: number }>('/ai-bots/subscriptions', data),
  cancelSubscription: (id: number) => api.post<{ id: number }>(`/ai-bots/subscriptions/${id}/cancel`),
}

// ── Market Listings（机器人/信号市场上架准入，对标 CryptoRobotics 创作者市场）──
export type MarketListingStatus = 'draft' | 'probation' | 'pending_review' | 'listed' | 'rejected' | 'delisted'

export interface MarketListingStats {
  listing_id: string
  date: string
  total_return_pct: number
  annualized_return_pct: number
  max_drawdown_pct: number
  win_rate: number
  profit_factor: number
  sharpe_ratio: number
  total_trades: number
  monthly_return_pct: number
  followers: number
  running_days: number
}

export interface MarketProbationProgress {
  days_elapsed: number
  min_days: number
  remaining_days: number
  trades_in_window: number
  min_trades: number
  remaining_trades: number
  max_drawdown_pct: number
  max_drawdown_limit: number
  days_ok: boolean
  trades_ok: boolean
  drawdown_ok: boolean
  passed: boolean
}

export interface MarketListing {
  id: string
  author_user_id: number
  bot_instance_id: string
  kind: string
  name: string
  description: string
  fee_model: string
  fee_percent: number
  monthly_fee: number
  status: MarketListingStatus
  listed_at?: number
  probation_passed: boolean
  stats?: MarketListingStats
  // 作者/管理视图扩展字段
  reject_reason?: string
  delist_reason?: string
  probation_started_at?: number
  progress?: MarketProbationProgress
  rule_min_days?: number
  rule_min_trades?: number
  rule_max_drawdown_pct?: number
}

export interface MarketRules {
  min_days: number
  min_trades: number
  max_drawdown_pct: number
}

export interface MarketListingCreateRequest {
  bot_instance_id: string
  kind?: string
  name?: string
  description?: string
  fee_model?: string
  fee_percent?: number
  monthly_fee?: number
  submit?: boolean
}

export const marketListingApi = {
  // 作者侧
  create: (data: MarketListingCreateRequest) => api.post<MarketListing>('/market/listings', data),
  myListings: () => api.get<{ listings: MarketListing[] }>('/market/my-listings').then((d) => d?.listings ?? []),
  submit: (id: string) => api.post<MarketListing>(`/market/listings/${id}/submit`),
  cancel: (id: string) => api.post<MarketListing>(`/market/listings/${id}/cancel`),
  // 公开侧
  list: (params?: { sort?: string; order?: string; page?: number; page_size?: number }) =>
    api.get<{ listings: MarketListing[]; total: number; page: number; page_size: number }>('/market/listings', { params }),
  stats: (id: string, limit = 90) =>
    api.get<{ listing: MarketListing; series: MarketListingStats[] }>(`/market/listings/${id}/stats?limit=${limit}`),
  rules: () => api.get<MarketRules>('/market/rules'),
}

// ── Admin Market（上架审核队列）──
export const adminMarketApi = {
  listings: (status = 'pending_review') =>
    api.get<{ listings: MarketListing[] }>('/admin/market/listings', { params: { status } }).then((d) => d?.listings ?? []),
  approve: (id: string) => api.post<MarketListing>(`/admin/market/listings/${id}/approve`),
  reject: (id: string, reason: string) => api.post<MarketListing>(`/admin/market/listings/${id}/reject`, { reason }),
  delist: (id: string, reason: string) => api.post<MarketListing>(`/admin/market/listings/${id}/delist`, { reason }),
  saveRules: (rules: MarketRules) => api.put<MarketRules>('/admin/market/rules', rules),
}

// ── Logs ──
export const logsApi = {
  tail: (lines?: number) => api.get<string>(`/logs?tail=${lines || 100}`),
}

// ── Data Download ──
export const dataApi = {
  coverage: () =>
    api
      .get<{
        coverage?: { symbol: string; interval: string; bar_count?: number; start_time?: number; end_time: number }[] | null
        symbols?: string[] | null
      }>('/data/coverage')
      .then((d): DataCoverageResponse => {
        // 后端实际返回 {coverage:[{symbol,interval,...}], symbols:[...]}（可为 null），
        // 归一化为按交易对聚合的 DataCoverageResponse，两个消费组件共用。
        const grouped = new Map<string, { symbol: string; intervals: string[]; from?: number; to?: number }>()
        for (const c of d?.coverage ?? []) {
          const g = grouped.get(c.symbol) ?? { symbol: c.symbol, intervals: [] as string[] }
          if (c.interval && !g.intervals.includes(c.interval)) g.intervals.push(c.interval)
          if (c.start_time && (!g.from || c.start_time < g.from)) g.from = c.start_time
          if (c.end_time && (!g.to || c.end_time > g.to)) g.to = c.end_time
          grouped.set(c.symbol, g)
        }
        for (const s of d?.symbols ?? []) {
          if (!grouped.has(s)) grouped.set(s, { symbol: s, intervals: [] })
        }
        const symbols = [...grouped.values()]
        return { symbols, total_symbols: symbols.length }
      }),
  info: (symbol: string, interval: string) =>
    api.get<DataInfoResponse>(`/data/info?symbol=${symbol}&interval=${interval}`),
  download: (config: DownloadConfig) => api.post<{ job_id: string }>('/data/download', config),
  jobStatus: (id: string) => api.get<DownloadJobStatus>(`/data/download/${id}`),
  bars: (symbol: string, interval: string, from: number, to: number) =>
    api.get<BarDataResponse>(`/data/bars?symbol=${symbol}&interval=${interval}&from=${from}&to=${to}`),
}

// ── Health / Status ──
export const healthApi = {
  health: () => api.get<HealthResponse>('/health'),
  components: () => api.get<ComponentHealthResponse[]>('/health/components'),
  status: () => api.get<StatusResponse>('/status'),
}

// ── Alertmanager 告警事件（Prometheus → Alertmanager → gateway webhook 落库）──
export interface AlertEvent {
  fingerprint: string
  alertname: string
  status: 'firing' | 'resolved' | string
  severity: 'critical' | 'warning' | 'info' | string
  summary: string
  description: string
  labels: Record<string, string>
  starts_at: number
  ends_at: number
  first_seen: number
  last_seen: number
}

export const alertApi = {
  active: () => api.get<{ alerts: AlertEvent[] }>('/alerts/active'),
  history: (limit?: number) => api.get<{ alerts: AlertEvent[] }>(`/alerts/history?limit=${limit || 50}`),
}

// ── Integrations Precheck（外部集成预检：连通性 + 凭证格式，密钥不回显）──
export interface IntegrationStatus {
  name: string
  display_name: string
  category: string
  configured: boolean
  /** null = 未探测 */
  reachable: boolean | null
  last_verified_at?: number
  notes?: string
}

export const integrationsApi = {
  status: () => api.get<{ integrations: IntegrationStatus[] }>('/integrations/status'),
  check: (name: string) => api.post<IntegrationStatus>(`/integrations/${name}/check`),
}

// ── Exchange Health Check（交易所体检，对标 freqtrade check_exchange）──
export type ExchangeHealthCheckStatus = 'pass' | 'fail' | 'skip'
export type ExchangeHealthOverall = 'healthy' | 'degraded' | 'unhealthy' | 'not_configured'

export interface ExchangeHealthCheckItem {
  name: string
  status: ExchangeHealthCheckStatus
  duration_ms: number
  detail?: string
  error?: string
}

export interface ExchangeHealthLevel {
  level: number
  name: string
  status: ExchangeHealthCheckStatus
  duration_ms: number
  items: ExchangeHealthCheckItem[]
}

export interface ExchangeHealthReport {
  exchange: string
  configured: boolean
  overall: ExchangeHealthOverall
  levels: ExchangeHealthLevel[]
  duration_ms: number
  checked_at: number
}

export interface ExchangeHealthRecord {
  id: string
  exchange: string
  job_id: string
  overall: ExchangeHealthOverall
  configured: boolean
  duration_ms: number
  checked_at: number
  report: ExchangeHealthReport
}

export interface ExchangeHealthJob {
  id: string
  user_id: number
  exchanges: string[]
  status: 'running' | 'completed' | 'failed'
  results: Record<string, ExchangeHealthReport>
  error?: string
  created_at: number
  finished_at?: number
}

export const exchangeHealthApi = {
  start: (exchange?: string) =>
    api.post<{ job_id: string; status: string; exchanges: string[] }>('/exchanges/health-check', {
      exchange: exchange || 'all',
    }),
  job: (id: string) => api.get<ExchangeHealthJob>(`/exchanges/health-check/jobs/${id}`),
  latest: () => api.get<{ results: ExchangeHealthRecord[] }>('/exchanges/health-check/latest'),
  history: (exchange?: string, limit?: number) =>
    api.get<{ results: ExchangeHealthRecord[] }>(
      `/exchanges/health-check/history?limit=${limit || 50}${exchange ? `&exchange=${exchange}` : ''}`,
    ),
}

// ── 回测偏差检测（对标 freqtrade lookahead-analysis / recursive-analysis）──
export interface AnalysisJob {
  id: string
  user_id: number
  kind: 'lookahead' | 'recursive'
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  symbol: string
  interval: string
  strategy_type: string
  params: string
  result?: string
  error?: string
  created_at: number
  updated_at: number
  completed_at?: number
}

export interface AnalysisSignalEvent {
  time: number
  index: number
  direction: string
}

export interface AnalysisFalseSignal {
  signal: AnalysisSignalEvent
  variant: string
  kind: 'missing' | 'displaced'
  detail: string
}

export interface AnalysisVariantReport {
  name: string
  kind: string
  n: number
  variant_bars: number
  total_entries: number
  false_entries: AnalysisFalseSignal[]
  missing_count: number
  displaced_count: number
}

export interface LookaheadResult {
  conclusion: 'biased' | 'unbiased' | 'inconclusive'
  biased: boolean
  confidence: string
  total_entries: number
  checked_entries: number
  false_entry_count: number
  variant_count: number
  baseline_entries: AnalysisSignalEvent[]
  variants: AnalysisVariantReport[]
  summary: string
}

export interface AnalysisPrefixLevel {
  name: string
  bars: number
  entries: number
  exits: number
}

export interface AnalysisUnstablePoint {
  index: number
  time: number
  by_lens: Record<string, string>
}

export interface AnalysisVariantGroup {
  kind: 'prefix' | 'start_offset'
  levels: AnalysisPrefixLevel[]
  compared_points: number
  unstable_points: AnalysisUnstablePoint[]
  unstable_count: number
}

export interface RecursiveResult {
  conclusion: 'recursive' | 'stable' | 'inconclusive'
  recursive: boolean
  confidence: string
  levels: AnalysisPrefixLevel[]
  unstable_points: AnalysisUnstablePoint[]
  unstable_count: number
  compared_points: number
  // groups 按组承载全部维度明细：prefix（同起点前缀递增）+ start_offset
  // （固定终点起点右移，捕捉 warmup 不收敛类递归偏差）。
  groups?: AnalysisVariantGroup[]
  summary: string
}

export interface AnalysisJobDetail {
  id: string
  kind: 'lookahead' | 'recursive'
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  symbol: string
  interval: string
  strategy_type: string
  config?: Record<string, unknown>
  result?: LookaheadResult | RecursiveResult
  error?: string
  created_at: number
  updated_at: number
  completed_at?: number
}

export const analysisApi = {
  startLookahead: (data: Record<string, unknown>) =>
    api.post<{ job_id: string; status: string }>('/analysis/lookahead', data),
  startRecursive: (data: Record<string, unknown>) =>
    api.post<{ job_id: string; status: string }>('/analysis/recursive', data),
  jobs: (kind?: string) =>
    api
      .get<{ jobs: AnalysisJob[] }>('/analysis/jobs', { params: kind ? { kind } : {} })
      .then((d) => d?.jobs ?? []),
  job: (id: string) => api.get<AnalysisJobDetail>(`/analysis/jobs/${id}`),
  cancel: (id: string) => api.post<{ status: string }>(`/analysis/jobs/${id}/cancel`),
  remove: (id: string) => api.del<{ status: string }>(`/analysis/jobs/${id}`),
}

// ── Paper / Live Trading Safety ──
export const paperApi = {
  safety: () => api.get<TradingSafetyResponse>('/trading/safety'),
  unlock: () => api.post<{ success: boolean }>('/trading/unlock'),
  lock: () => api.post<{ success: boolean }>('/trading/lock'),
}

// ── Exchange Status ──
export const exchangeStatusApi = {
  status: () => api.get<ExchangeStatusResponse>('/exchange/status'),
  setDefault: (id: string) => api.post<{ success: boolean }>('/exchange/default', { id }),
}

// ── Grid Trading ──
export interface GridBot {
  id: string
  name: string
  symbol: string
  lower_price: number
  upper_price: number
  grid_count: number
  investment: number
  fee_rate: number
  status: 'stopped' | 'running'
  exchange?: string
  /** A1.3：long=现货做多网格 / short=合约做空网格 / neutral=中性对冲双网格。 */
  mode?: 'long' | 'short' | 'neutral'
  leverage?: number
  margin_mode?: string
  realized_pnl: number
  total_trades: number
  base_qty: number
  quote_balance: number
  initial_equity: number
  created_at: number
  updated_at: number
  started_at?: number
  stopped_at?: number
  is_running?: boolean
}

/** A1.3 网格单腿视图（neutral 双腿各自独立核算）。 */
export interface GridLegOrder {
  level: number
  side: string
  price: number
  qty: number
}

export interface GridLegView {
  leg: 'long' | 'short'
  base_qty: number
  quote_balance: number
  realized_pnl: number
  total_pnl: number
  open_orders: number
  orders: GridLegOrder[]
}

export interface GridTrade {
  id: number
  bot_id: string
  level_index: number
  side: string
  price: number
  quantity: number
  quote_qty: number
  fee: number
  pnl: number
  /** A1.3：成交所属腿（long/short）。 */
  leg?: string
  ts: number
}

export interface GridSnapshot {
  id: number
  bot_id: string
  equity: number
  price: number
  realized_pnl: number
  open_orders: number
  ts: number
}

export interface GridBotDetail extends GridBot {
  open_orders: number
  legs?: GridLegView[]
  /** A1.3 整体净头寸（多头腿为正、空头腿为负）。 */
  net_position?: number
  price?: number
  trades: GridTrade[]
  snapshots: GridSnapshot[]
}

export interface GridBotPayload {
  name: string
  symbol: string
  lower_price: number
  upper_price: number
  grid_count: number
  investment: number
  fee_rate?: number
  mode?: 'long' | 'short' | 'neutral'
  exchange?: string
  leverage?: number
  margin_mode?: string
}

// ── Risk config（风控参数：UI 可调，PUT 仅 admin） ──
export interface RiskConfig {
  max_concurrent_orders: number
  position_limit_pct: number
  profit_protection_enabled: boolean
  indicator_fail_open: boolean
}

export const riskApi = {
  getConfig: () => api.get<RiskConfig>('/risk/config'),
  updateConfig: (data: RiskConfig) => api.put<RiskConfig>('/risk/config', data),
}

export const gridApi = {
  list: () => api.get<{ bots: GridBot[] }>('/grid/bots').then((d) => d?.bots ?? []),
  create: (data: GridBotPayload) => api.post<GridBot>('/grid/bots', data),
  get: (id: string) => api.get<GridBotDetail>(`/grid/bots/${id}`),
  update: (id: string, data: GridBotPayload) => api.put<GridBot>(`/grid/bots/${id}`, data),
  remove: (id: string) => api.del<{ deleted: boolean; id: string }>(`/grid/bots/${id}`),
  start: (id: string) => api.post<{ started: boolean; id: string; price: number }>(`/grid/bots/${id}/start`),
  stop: (id: string) => api.post<{ stopped: boolean; id: string }>(`/grid/bots/${id}/stop`),
}

// ── DCA 定投机器人（A1.2）──
export interface DCABot {
  id: string
  name: string
  symbol: string
  exchange: string
  quote_amount: number
  interval_minutes: number
  max_orders: number
  period_budget: number
  take_profit_pct: number
  stop_loss_pct: number
  trailing_enabled: boolean
  status: 'stopped' | 'running' | 'finished'
  filled_orders: number
  total_invested: number
  base_qty: number
  avg_price: number
  realized_pnl: number
  last_buy_at: number
  highest_price: number
  created_at: number
  updated_at: number
  started_at?: number
  stopped_at?: number
  is_running?: boolean
}

export interface DCAOrder {
  id: number
  bot_id: string
  side: string
  price: number
  quantity: number
  quote_qty: number
  reason: string
  ts: number
}

export interface DCABotDetail extends DCABot {
  orders: DCAOrder[]
}

export interface DCABotPayload {
  name: string
  symbol: string
  exchange?: string
  quote_amount: number
  interval_minutes: number
  max_orders?: number
  period_budget?: number
  take_profit_pct?: number
  stop_loss_pct?: number
  trailing_enabled?: boolean
}

export const dcaBotApi = {
  list: () => api.get<{ bots: DCABot[] }>('/dca-bots/').then((d) => d?.bots ?? []),
  create: (data: DCABotPayload) => api.post<DCABot>('/dca-bots/', data),
  get: (id: string) => api.get<DCABotDetail>(`/dca-bots/${id}`),
  update: (id: string, data: DCABotPayload) => api.put<DCABot>(`/dca-bots/${id}`, data),
  remove: (id: string) => api.del<{ deleted: boolean; id: string }>(`/dca-bots/${id}`),
  start: (id: string) => api.post<{ started: boolean; id: string; price: number }>(`/dca-bots/${id}/start`),
  stop: (id: string) => api.post<{ stopped: boolean; id: string }>(`/dca-bots/${id}/stop`),
}

// ── 分层马丁格尔机器人（A1.3）──
export interface LayeredMartinGroup {
  id?: number
  bot_id?: string
  group_index?: number
  quote_amount: number
  multiplier: number
  max_layers: number
  budget_cap: number
  layer?: number
  total_invested?: number
  base_qty?: number
  avg_price?: number
  entry_price?: number
  highest_price?: number
  pending_layer?: number
  status?: string
  created_at?: number
  updated_at?: number
}

export interface LayeredMartinBot {
  id: string
  name: string
  symbol: string
  exchange: string
  price_deviation_pct: number
  take_profit_pct: number
  stop_loss_pct: number
  trailing_enabled: boolean
  status: 'stopped' | 'running' | 'finished'
  realized_pnl: number
  total_trades: number
  groups: LayeredMartinGroup[]
  created_at: number
  updated_at: number
  started_at?: number
  stopped_at?: number
  is_running?: boolean
}

export interface LayeredMartinOrder {
  id: number
  bot_id: string
  group_index: number
  side: string
  layer: number
  price: number
  quantity: number
  quote_qty: number
  reason: string
  ts: number
}

export interface LayeredMartinBotDetail extends LayeredMartinBot {
  orders: LayeredMartinOrder[]
}

export interface LayeredMartinBotPayload {
  name: string
  symbol: string
  exchange?: string
  price_deviation_pct: number
  take_profit_pct?: number
  stop_loss_pct?: number
  trailing_enabled?: boolean
  groups: { quote_amount: number; multiplier: number; max_layers: number; budget_cap: number }[]
}

export const layeredMartinApi = {
  list: () => api.get<{ bots: LayeredMartinBot[] }>('/layered-martin-bots/').then((d) => d?.bots ?? []),
  create: (data: LayeredMartinBotPayload) => api.post<LayeredMartinBot>('/layered-martin-bots/', data),
  get: (id: string) => api.get<LayeredMartinBotDetail>(`/layered-martin-bots/${id}`),
  update: (id: string, data: LayeredMartinBotPayload) => api.put<LayeredMartinBot>(`/layered-martin-bots/${id}`, data),
  remove: (id: string) => api.del<{ deleted: boolean; id: string }>(`/layered-martin-bots/${id}`),
  start: (id: string) => api.post<{ started: boolean; id: string; price: number }>(`/layered-martin-bots/${id}/start`),
  stop: (id: string) => api.post<{ stopped: boolean; id: string }>(`/layered-martin-bots/${id}/stop`),
}

export { ApiError }

// ── Share Card（收益分享卡：已平仓交易 / 回测报告，属主+admin 可取）──
export interface ShareCardBase {
  kind: string
  id: string
  nickname: string
  amount_mode: 'pct' | 'abs'
  share_url: string
  generated_at: number
}

export interface ShareBacktestCard extends ShareCardBase {
  kind: 'backtest'
  name: string
  strategy: string
  symbol?: string
  timeframe?: string
  total_return_pct: number
  max_drawdown_pct: number
  sharpe_ratio: number
  sortino_ratio: number
  win_rate: number
  profit_factor: number
  total_trades: number
  initial_capital: number
  final_equity: number
  start_time: number
  end_time: number
  created_at: number
}

export interface ShareTradeCard extends ShareCardBase {
  kind: 'trade'
  symbol: string
  side: string
  exchange: string
  entry_price: number
  exit_price: number
  quantity: number
  cost_basis: number
  pnl: number
  pnl_pct: number
  opened_at: number
  closed_at: number
  hold_ms: number
}

export const shareApi = {
  backtestCard: (id: string) => api.get<ShareBacktestCard>(`/share/backtest/${id}/card`),
  tradeCard: (id: string) => api.get<ShareTradeCard>(`/share/trade/${id}/card`),
}
