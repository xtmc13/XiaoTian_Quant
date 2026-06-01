import axios, { AxiosInstance, AxiosRequestConfig, AxiosError } from 'axios'

// ── Timeout presets (QuantDinger style) ──
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
  return config
})

// ── Response interceptor ──
let isRedirectingToLogin = false
axiosInstance.interceptors.response.use(
  (response) => {
    // If backend wraps with { data: ... }, unwrap it — always return response object
    const data = response.data
    if (data && typeof data === 'object' && 'data' in data && !('success' in data)) {
      response.data = data.data
    }
    return response
  },
  (error: AxiosError) => {
    if (error.response) {
      const status = error.response.status
      const data = error.response.data as any

      // 401 Unauthorized → redirect to login (prevent loop)
      if (status === 401) {
        if (!isRedirectingToLogin) {
          isRedirectingToLogin = true
          localStorage.removeItem('xt-token')
          window.location.href = '/#/login'
          setTimeout(() => { isRedirectingToLogin = false }, 3000)
        }
        return Promise.reject(new ApiError('登录已过期，请重新登录', 401, 'UNAUTHORIZED'))
      }

      // 403 Forbidden → show backend message
      if (status === 403) {
        const msg = data?.msg || data?.message || '权限不足'
        return Promise.reject(new ApiError(msg, 403, 'FORBIDDEN'))
      }

      const message = data?.message || data?.error || `Request failed with status ${status}`
      return Promise.reject(new ApiError(message, status, data?.code || 'HTTP_ERROR'))
    }

    if (error.request) {
      return Promise.reject(new ApiError('网络错误，请检查连接', 0, 'NETWORK_ERROR'))
    }

    return Promise.reject(new ApiError(error.message, 0, 'UNKNOWN'))
  }
)

// ── Generic request helpers ──
async function request<T>(method: string, path: string, body?: unknown, config?: AxiosRequestConfig): Promise<T> {
  const resp = await axiosInstance.request<ApiResponse<T>>({
    method: method as any,
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
  summary: () => api.get<any>('/dashboard/summary'),
}

// ── Portfolio ──
export const portfolioApi = {
  summary: () => api.get<any>('/portfolio/summary'),
  positions: () => api.get<any>('/portfolio/positions'),
  snapshots: (days?: number) => api.get<any>(`/portfolio/snapshots${days ? '?days=' + days : ''}`),
  calendar: (year?: number, month?: number) =>
    api.get<any>(`/portfolio/calendar?year=${year || new Date().getFullYear()}&month=${month || new Date().getMonth() + 1}`),
}

// ── Market ──
export const marketApi = {
  klines: (symbol: string, interval = '1h', limit = 200) =>
    api.get<any>('/market/klines', { params: { symbol, interval, limit } })
      .then((d: any) => d?.klines || d || []),
  orderBook: (symbol: string, depth = 20) =>
    api.get<any>('/market/orderbook', { params: { symbol, depth } }),
  trades: (symbol: string, limit = 50) =>
    api.get<any>('/market/trades', { params: { symbol, limit } })
      .then((d: any) => d?.trades || d || []),
  snapshot: (symbol?: string) =>
    api.get<any>(`/market/snapshot${symbol ? '?symbol=' + symbol : ''}`),
  symbolSearch: (q: string) => api.get<any[]>(`/symbols/search?q=${q}`),
  status: () => api.get<any>('/status'),
}

// ── Orders ──
export const orderApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<any[]>(`/orders${qs}`)
  },
  place: (order: any) => api.post<any>('/orders', order),
  cancel: (id: string) => api.post<any>(`/orders/${id}/cancel`),
  cancelAll: () => api.post<any>('/orders/cancel-all'),
  history: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<any[]>(`/orders/history${qs}`)
  },
}

// ── Account ──
export const accountApi = {
  balance: (symbol?: string) => api.get<any>(`/account/balance${symbol ? '?symbol=' + symbol : ''}`),
}

// ── Trades ──
export const tradesApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<any[]>(`/trades${qs}`)
  },
}

// ── Strategies ──
export const strategyApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<any[]>(`/strategies/configs${qs}`)
  },
  get: (id: string) => api.get<any>(`/strategies/configs/${id}`),
  create: (data: any) => api.post<any>('/strategies/configs', data),
  update: (id: string, data: any) => api.put<any>(`/strategies/configs/${id}`, data),
  delete: (id: string) => api.del<any>(`/strategies/configs/${id}`),
  start: (id: string) => api.post<any>(`/strategies/configs/${id}/start`),
  stop: (id: string) => api.post<any>(`/strategies/configs/${id}/stop`),
  batchStart: (ids: string[]) => api.post<any>('/strategies/configs/batch-start', { ids }),
  batchStop: (ids: string[]) => api.post<any>('/strategies/configs/batch-stop', { ids }),
  logs: (strategyId?: string) =>
    api.get<any[]>(`/strategies/logs${strategyId ? '?strategy_id=' + strategyId : ''}`),
  clearLogs: (strategyId?: string) =>
    api.del<any>(`/strategies/logs${strategyId ? '?strategy_id=' + strategyId : ''}`),
  templates: (category = 'spot') => api.get<any[]>(`/strategies/templates?category=${category}`),
  createTemplate: (data: any) => api.post<any>('/strategies/templates', data),
  deleteTemplate: (id: string) => api.del<any>(`/strategies/templates/${id}`),
  global: () => api.get<any>('/strategies/global'),
  saveGlobal: (data: any) => api.put<any>('/strategies/global', data),
  spot: () => api.get<any[]>('/strategies/spot'),
  contract: () => api.get<any[]>('/strategies/contract'),
  ranking: () => api.get<any>('/strategies/ranking'),
}

// ── Backtest ──
export const backtestApi = {
  run: (config: any) => api.post<any>('/backtest/run', config, { timeout: TIMEOUTS.backtest }),
  native: (config: any) => api.post<any>('/native/backtest', config, { timeout: TIMEOUTS.backtest }),
}

// ── AI ──
export const aiApi = {
  snapshot: (symbol?: string) =>
    api.get<any>(`/ai/snapshot${symbol ? '?symbol=' + symbol : ''}`),
  klines: (symbol: string, interval?: string) =>
    api.get<any>(`/ai/klines?symbol=${symbol}&interval=${interval || '1h'}`),
  generate: (data: any) => api.post<any>('/ai/generate', data, { timeout: TIMEOUTS.ai }),
  multiAgent: (data: any) => api.post<any>('/ai/multi-agent', data, { timeout: TIMEOUTS.ai }),
  backtest: (data: any) => api.post<any>('/ai/backtest', data, { timeout: TIMEOUTS.backtest }),
  optimize: (data: any) => api.post<any>('/ai/optimize', data, { timeout: TIMEOUTS.ai }),
  deploy: (data: any) => api.post<any>('/ai/deploy', data),
  analyze: (data: any) => api.post<any>('/ai/analyze', data, { timeout: TIMEOUTS.analysis }),
  quickScan: () => api.get<any>('/ai/quickscan'),
  chat: (message: string) => api.post<any>('/ai/chat', { message }),
  models: () => api.get<any[]>('/ai/models'),
  autoTradeGet: () => api.get<any>('/auto-trade/config'),
  autoTradeSave: (config: any) => api.put<any>('/auto-trade/config', config),
}

// ── Chat ──
export const chatApi = {
  send: (message: string) => api.post<any>('/chat/send', { message }),
}

// ── Agent ──
export const agentApi = {
  tokens: () => api.get<any[]>('/agent/tokens'),
  createToken: (data: any) => api.post<any>('/agent/tokens', data),
  deleteToken: (id: string) => api.del<any>(`/agent/tokens/${id}`),
  ccSwitchStatus: () => api.get<any>('/agent/cc-switch'),
  aiConfig: () => api.get<any>('/agent/ai-config'),
  saveAIConfig: (data: any) => api.put<any>('/agent/ai-config', data),
  chat: (message: string) => api.post<any>('/agent/chat', { message }),
}

// ── Config (raw store config) ──
export const configApi = {
  get: () => api.get<any>('/config'),
  save: (data: any) => api.put<any>('/config', data),
  exchangeTest: (data: any) => api.post<any>('/exchange/test', data),
  exchangeSave: (data: any) => api.post<any>('/exchange/save', data),
  currencyGet: () => api.get<any>('/settings/currency'),
  currencySet: (currency: string) => api.put<any>('/settings/currency', { currency }),
  aiTest: (data: any) => api.post<any>('/ai/test', data),
  aiSave: (data: any) => api.post<any>('/ai/save', data),
}

// ── Settings ──
export const settingsApi = {
  agentModels: () => api.get<any[]>('/settings/agent/models'),
  defaults: () => api.get<any>('/settings/defaults'),
  saveDefaults: (data: any) => api.post<any>('/settings/defaults', data),
  saveUI: (data: any) => api.post<any>('/settings/ui', data),
  exchangeTest: (id: string) => api.post<any>(`/settings/exchange/${id}/test`),
  exchangeSave: (id: string, data: any) => api.put<any>(`/settings/exchange/${id}`, data),
  aiTest: (id: string) => api.post<any>(`/settings/ai/${id}/test`),
  aiSave: (id: string, data: any) => api.put<any>(`/settings/ai/${id}`, data),
}

// ── Health ──
export const healthApi = {
  check: () => api.get<any>('/health'),
  components: () => api.get<any[]>('/health/components'),
}

// ── Strategy Community ──
export const strategyCommunityApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; sort_by?: string }) =>
    api.get<any>('/community/strategies', { params }),
  detail: (id: number) => api.get<any>(`/community/strategies/${id}`),
  publish: (data: any) => api.post<any>('/community/strategies/publish', data),
  comment: (id: number, content: string) => api.post<any>(`/community/strategies/${id}/comment`, { content }),
  rate: (id: number, rating: number) => api.post<any>(`/community/strategies/${id}/rate`, { rating }),
  leaderboard: (sortBy?: string, limit?: number) =>
    api.get<any>('/community/strategies/leaderboard', { params: { sort_by: sortBy, limit } }),
}

// ── ML ──
export const mlApi = {
  train: (config: any) => api.post<any>('/ml/train', config, { timeout: 120000 }),
  predict: (data: any) => api.post<any>('/ml/predict', data),
  list: () => api.get<any>('/ml/models'),
  detail: (id: string) => api.get<any>(`/ml/models/${id}`),
  deleteModel: (id: string) => api.del<any>(`/ml/models/${id}`),
  importance: (id: string) => api.get<any>(`/ml/models/${id}/importance`),
  generateFeatures: (data: any) => api.post<any>('/ml/features', data),
  health: () => api.get<any>('/ml/health'),
  deploy: (data: any) => api.post<any>('/ml/deploy', data),
  strategyModels: () => api.get<any>('/ml/strategy-models'),
}

// ── Notifications ──
export const notificationApi = {
  list: (params?: { limit?: number; offset?: number; unread?: boolean }) =>
    api.get<any>('/notifications', { params }),
  unreadCount: () => api.get<any>('/notifications/unread-count'),
  markRead: (id: number) => api.post<any>(`/notifications/${id}/read`),
  markAllRead: () => api.post<any>('/notifications/read-all'),
  clear: () => api.del<any>('/notifications'),
}

// ── Indicators ──
export const indicatorApi = {
  // --- New contract-based API ---
  parse: (code: string) => api.post<any>('/indicator/parse', { code }),
  validate: (code: string) => api.post<any>('/indicator/validate', { code }),
  save: (data: any) => api.post<any>('/indicator/save', data),
  list: () => api.get<any[]>('/indicator/list'),
  get: (id: number) => api.get<any>(`/indicator/${id}`),
  delete: (id: number) => api.del<any>(`/indicator/${id}`),
  applyParamDefaults: (code: string, indicatorParams: Record<string, any>) =>
    api.post<any>('/indicator/applyParamDefaults', { code, indicatorParams }),

  // --- Legacy API (keep for compatibility) ---
  listLegacy: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : ''
    return api.get<any[]>(`/indicator/getIndicators${qs}`)
  },
  create: (data: any) => api.post<any>('/indicator/saveIndicator', data),
  update: (id: number, data: any) => api.put<any>(`/indicator/${id}`, data),
  saveAs: (data: any) => api.post<any>('/indicator/saveIndicator', { ...data, is_new_copy: true }),
  publish: (id: number, data: any) => api.post<any>('/indicator/publish', { id, ...data }),
  decrypt: (userId: number, indicatorId: number) =>
    api.post<any>('/indicator/getDecryptKey', { user_id: userId, indicator_id: indicatorId }),
  backtest: (data: any) => api.post<any>('/indicator/backtest', data, { timeout: TIMEOUTS.backtest }),
  aiGenerate: (data: any) => api.post<any>('/indicator/aiGenerate', data, { timeout: TIMEOUTS.ai }),
  aiGenerateStream: (
    data: { prompt: string; existingCode?: string },
    handlers: {
      onCodeChunk?: (chunk: string) => void
      onStatus?: (status: string) => void
      onValidation?: (result: any) => void
      onCodeReplace?: (code: string) => void
      onDebug?: (info: any) => void
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
        } catch (e: any) {
          handlers.onError?.(e.message || 'Stream error')
          reject(e)
        }
      }).catch((err: any) => {
        handlers.onError?.(err.message || 'Network error')
        reject(err)
      })
    })
  },
  run: (data: any) => api.post<any>('/indicator/run', data),
  watchlist: () => api.get<any[]>('/watchlist'),
  addWatchlist: (data: any) => api.post<any>('/watchlist', data),
  kline: (params: Record<string, any>) => api.get<any>('/indicator/kline', { params }),
  experiment: {
    run: (data: any) => api.post<any>('/experiment/run', data, { timeout: TIMEOUTS.backtest }),
    sensitivity: (data: any) => api.post<any>('/experiment/sensitivity', data, { timeout: TIMEOUTS.backtest }),
    walkForward: (data: any) => api.post<any>('/experiment/walk-forward', data, { timeout: TIMEOUTS.backtest }),
    aiOptimize: (data: any) => api.post<any>('/experiment/ai-optimize', data, { timeout: TIMEOUTS.backtest }),
    structuredTune: (data: any) => api.post<any>('/experiment/structured-tune', data, { timeout: TIMEOUTS.backtest }),
  },
}

// ── Community ──
export const communityApi = {
  market: (params?: { page?: number; page_size?: number; keyword?: string; pricing_type?: string; sort_by?: string }) => {
    const qs = params ? '?' + new URLSearchParams(Object.entries(params).filter(([_, v]) => v !== undefined) as [string, string][]).toString() : ''
    return api.get<any>(`/community/indicators${qs}`)
  },
  publish: (data: { indicatorId: number; pricingType?: string; price?: number }) =>
    api.post<any>('/community/publish', data),
  purchase: (id: number) => api.post<any>(`/community/purchase/${id}`, {}),
  comments: (id: number, page?: number, pageSize?: number) =>
    api.get<any>(`/community/comments/${id}?page=${page || 1}&page_size=${pageSize || 20}`),
  addComment: (id: number, data: { rating: number; content: string }) =>
    api.post<any>(`/community/comments/${id}`, data),
}

// ── Admin ──
export const adminApi = {
  users: () => api.get<any[]>('/auth/admin/users'),
  user: (id: number) => api.get<any>(`/auth/admin/users/${id}`),
  updateUser: (id: number, data: any) => api.put<any>(`/auth/admin/users/${id}`, data),
  stats: () => api.get<any>('/auth/admin/stats'),
  enhancedStats: () => api.get<any>('/admin/stats'),
  auditLog: (params?: { limit?: number; offset?: number }) => api.get<any>('/admin/audit-log', { params }),
}

// ── Agent (admin) ──
export const agentAdminApi = {
  tokens: () => agentApi.tokens(),
  createToken: (data: any) => agentApi.createToken(data),
  deleteToken: (id: string) => agentApi.deleteToken(id),
  auditLog: () => api.get<any[]>('/agent/audit-log'),
}

export { ApiError }
