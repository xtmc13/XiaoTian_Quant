// ── 用户 Python 策略 API client（契约化运行时 v1）──
// 对应后端 /api/pystrategies 路由组（见 docs/PYTHON_STRATEGY_API.md）。
// 独立成文件而非并入 lib/api.ts，便于本轮以片段形式集成。
import { api } from '@/lib/api'

export interface PyStrategy {
  id: string
  user_id: number
  name: string
  symbol: string
  interval: string
  direction: 'long' | 'short' | 'both'
  params_json: string
  code: string
  version: number
  status: 'draft' | 'active' | 'paused' | 'error'
  error: string
  paper: boolean
  bot_id: string
  created_at: number
  updated_at: number
  is_running?: boolean
}

export interface PyStrategyValidationIssue {
  line: number
  code: string
  message: string
}

export interface PyStrategyValidateResult {
  valid: boolean
  issues: PyStrategyValidationIssue[]
  sandbox_error?: string
  checked_at: number
}

export interface PyStrategyLogEntry {
  ts: number
  level: 'info' | 'error' | 'action' | string
  message: string
}

export interface PyStrategyRuntimeStatus {
  running: boolean
  consec_errors: number
  last_error: string
  restarts: number
  sandbox_alive: boolean
}

export interface PyStrategyStatusResult {
  id: string
  status: string
  error: string
  paper: boolean
  version: number
  is_running: boolean
  runtime: PyStrategyRuntimeStatus
}

export interface PyStrategyPayload {
  name: string
  symbol: string
  interval: string
  direction: 'long' | 'short' | 'both'
  params_json: string
  code: string
  paper?: boolean
}

export const pyStrategyApi = {
  list: () => api.get<{ strategies: PyStrategy[] }>('/pystrategies').then((d) => d?.strategies ?? []),
  create: (data: PyStrategyPayload) => api.post<PyStrategy>('/pystrategies', data),
  get: (id: string) => api.get<PyStrategy>(`/pystrategies/${id}`),
  update: (id: string, data: PyStrategyPayload) => api.put<PyStrategy>(`/pystrategies/${id}`, data),
  remove: (id: string) => api.del<{ deleted: boolean; id: string }>(`/pystrategies/${id}`),
  validate: (id: string) => api.post<PyStrategyValidateResult>(`/pystrategies/${id}/validate`),
  start: (id: string) => api.post<{ started: boolean; id: string; status: string }>(`/pystrategies/${id}/start`),
  stop: (id: string) => api.post<{ stopped: boolean; id: string }>(`/pystrategies/${id}/stop`),
  logs: (id: string) =>
    api.get<{ logs: PyStrategyLogEntry[]; is_running: boolean }>(`/pystrategies/${id}/logs`),
  status: (id: string) => api.get<PyStrategyStatusResult>(`/pystrategies/${id}/status`),
}
