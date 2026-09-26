import { useState, useEffect, useCallback, useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { configApi, notifyRouteApi, riskApi } from '@/lib/api'
import { useAppStore } from '@/stores/appStore'
import { cn } from '@/lib/utils'
import type { ExchangeTestResult } from '@/types'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { DataDownloadSection } from './settings/DataDownloadSection'
import { MfaSection } from './settings/MfaSection'
import { useI18n, LANGS } from '@/i18n'
import { toast } from '@/lib/useToast'
import {
  Globe,
  KeyRound,
  BrainCircuit,
  Shield,
  Palette,
  Bell,
  Database,
  Save,
  RotateCcw,
  Wifi,
  WifiOff,
  AlertTriangle,
  Check,
  Copy,
  Eye,
  EyeOff,
  ChevronRight,
  Loader2,
  SlidersHorizontal,
  Zap,
  Route,
  Info,
  Terminal,
} from 'lucide-react'

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

interface ExchangeConfig {
  api_key?: string
  secret?: string
  passphrase?: string
  testnet?: boolean
  futures?: boolean
  enabled?: boolean
}

interface AIProviderConfig {
  api_key?: string
  model?: string
  base_url?: string
}

interface NotifyConfig {
  enabled: boolean
  address?: string
  botToken?: string
  chatId?: string
  webhook?: string
}

interface DataConfig {
  klineLimit: number
  autoCleanup: boolean
  realtime: boolean
  timezone?: string
}

interface SecurityConfig {
  twoFactor: boolean
  sessionTimeout: number
  ipWhitelist: string
}

interface RouteRule {
  id: string
  name: string
  events: string[]
  levels: string[]
  channels: string[]
  enabled: boolean
  minReturnPct?: number
}

/* ------------------------------------------------------------------ */
/*  Constants                                                          */
/* ------------------------------------------------------------------ */

const EVENTS = ['signal', 'trade', 'risk', 'protection', 'system', 'backtest', 'hyperopt']
const LEVELS = ['INFO', 'WARN', 'CRITICAL']
const CHANNELS = ['log', 'email', 'lark', 'dingtalk', 'telegram', 'discord', 'sms']

const CHANNEL_LABELS: Record<string, string> = {
  log: 'settings.routing.channel.log',
  email: 'settings.routing.channel.email',
  lark: 'settings.routing.channel.lark',
  dingtalk: 'settings.routing.channel.dingtalk',
  sms: 'settings.routing.channel.sms',
}

const EVENT_LABELS: Record<string, string> = {
  signal: 'settings.routing.event.signal',
  trade: 'settings.routing.event.trade',
  risk: 'settings.routing.event.risk',
  protection: 'settings.routing.event.protection',
  system: 'settings.routing.event.system',
  backtest: 'settings.routing.event.backtest',
  hyperopt: 'settings.routing.event.hyperopt',
  all: 'settings.routing.event.all',
}

const LEVEL_LABELS: Record<string, string> = {
  INFO: 'settings.routing.level.info',
  WARN: 'settings.routing.level.warn',
  CRITICAL: 'settings.routing.level.critical',
}

const EXCHANGES = [
  { key: 'binance', label: 'Binance', needsPassphrase: false },
  { key: 'okx', label: 'OKX', needsPassphrase: true },
  { key: 'coinbase', label: 'Coinbase', needsPassphrase: false },
  { key: 'gate', label: 'Gate.io', needsPassphrase: false },
  { key: 'mexc', label: 'MEXC', needsPassphrase: false },
  { key: 'bitget', label: 'Bitget', needsPassphrase: false },
  { key: 'bybit', label: 'Bybit', needsPassphrase: false },
  { key: 'kraken', label: 'Kraken', needsPassphrase: false },
] as const

// AI 模型目录条目（与后端 GET /config/ai-models 返回形状一致；
// 后端不可达时回落到这份内置完整目录）。
interface AIModelCatalogItem {
  key: string
  label: string
  models: string[]
  baseUrl: string
  default?: string
  configured?: boolean
  current_model?: string
}

// 内置回落目录：key 与后端 provider 注册表逐一对应（claude 不用 anthropic）。
const FALLBACK_AI_CATALOG: AIModelCatalogItem[] = [
  { key: 'openai', label: 'OpenAI', models: ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'gpt-4.1', 'o3', 'gpt-4o'], baseUrl: 'https://api.openai.com/v1', default: 'gpt-5.5' },
  { key: 'claude', label: 'Anthropic Claude', models: ['claude-opus-4-7', 'claude-opus-4-6', 'claude-sonnet-4-6', 'claude-sonnet-4-5', 'claude-haiku-4-5'], baseUrl: 'https://api.anthropic.com', default: 'claude-opus-4-7' },
  { key: 'gemini', label: 'Google Gemini', models: ['gemini-3.1-pro-preview', 'gemini-2.5-pro', 'gemini-2.5-flash', 'gemini-2.5-flash-lite'], baseUrl: 'https://generativelanguage.googleapis.com', default: 'gemini-3.1-pro-preview' },
  { key: 'deepseek', label: 'DeepSeek', models: ['deepseek-chat', 'deepseek-reasoner'], baseUrl: 'https://api.deepseek.com/v1', default: 'deepseek-chat' },
  { key: 'qwen', label: '通义千问', models: ['qwen3-max', 'qwen-plus', 'qwen-turbo', 'qwen3-coder-plus'], baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', default: 'qwen3-max' },
  { key: 'glm', label: '智谱 GLM', models: ['glm-4.7', 'glm-4.6', 'glm-4-plus', 'glm-4.5-air'], baseUrl: 'https://open.bigmodel.cn/api/paas/v4', default: 'glm-4.7' },
  { key: 'kimi', label: 'Kimi（月之暗面）', models: ['kimi-k2.5', 'kimi-k2-0905-preview', 'moonshot-v1-128k', 'moonshot-v1-32k'], baseUrl: 'https://api.moonshot.cn/v1', default: 'kimi-k2.5' },
  { key: 'doubao', label: '豆包（火山引擎）', models: ['doubao-seed-1-6', 'doubao-pro-32k', 'doubao-lite-32k'], baseUrl: 'https://ark.cn-beijing.volces.com/api/v3', default: 'doubao-seed-1-6' },
  { key: 'hunyuan', label: '腾讯混元', models: ['hunyuan-pro', 'hunyuan-standard', 'hunyuan-lite'], baseUrl: 'https://api.hunyuan.cloud.tencent.com/v1', default: 'hunyuan-pro' },
  { key: 'llama', label: 'Llama（Groq）', models: ['llama-4-maverick', 'llama-4-scout'], baseUrl: 'https://api.groq.com/openai/v1', default: 'llama-4-maverick' },
  { key: 'mistral', label: 'Mistral', models: ['mistral-large-3', 'mistral-medium', 'codestral'], baseUrl: 'https://api.mistral.ai/v1', default: 'mistral-large-3' },
  { key: 'openrouter', label: 'OpenRouter（聚合）', models: ['openai/gpt-5.5', 'anthropic/claude-opus-4.7', 'google/gemini-3.1-pro-preview', 'deepseek/deepseek-chat', 'qwen/qwen3-max'], baseUrl: 'https://openrouter.ai/api/v1', default: 'openai/gpt-5.5' },
]

const TIMEZONES = [
  'UTC',
  'Asia/Shanghai',
  'Asia/Tokyo',
  'Asia/Singapore',
  'Europe/London',
  'America/New_York',
  'America/Chicago',
  'America/Los_Angeles',
]

/* ------------------------------------------------------------------ */
/*  LocalStorage helpers                                               */
/* ------------------------------------------------------------------ */

function loadLocal<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    if (raw) return JSON.parse(raw) as T
  } catch {
    /* ignore parse error */
  }
  return fallback
}

function saveLocal<T>(key: string, value: T) {
  localStorage.setItem(key, JSON.stringify(value))
}

/* ------------------------------------------------------------------ */
/*  Reusable UI primitives                                             */
/* ------------------------------------------------------------------ */

function Toggle({ value, onChange, disabled }: { value: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button
      onClick={() => !disabled && onChange(!value)}
      disabled={disabled}
      className={cn(
        'relative h-5 w-10 rounded-full transition-colors',
        value ? 'bg-quant-gold' : 'bg-quant-border',
        disabled && 'opacity-50'
      )}
      role="switch"
      aria-checked={value}
    >
      <span
        className={cn(
          'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform',
          value ? 'left-5' : 'left-0.5'
        )}
      />
    </button>
  )
}

function PasswordInput({
  value,
  onChange,
  placeholder,
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  const [visible, setVisible] = useState(false)
  const { t } = useI18n()
  return (
    <div className="relative">
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 pr-10 text-sm text-foreground placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
      />
      <button
        type="button"
        onClick={() => setVisible(!visible)}
        aria-label={visible ? t('settings.security.hidePassword') : t('settings.security.showPassword')}
        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
      >
        {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  )
}

function TextInput({
  value,
  onChange,
  placeholder,
  type = 'text',
}: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  type?: string
}) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-foreground placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

function SelectField({
  value,
  onChange,
  options,
  label,
}: {
  value: string
  onChange: (v: string) => void
  options: Array<string | { value: string; label: string }>
  label?: string
}) {
  // options 兜底：调用方传入 null（接口异常/旧缓存页面）时不至于 .map 崩溃
  const safeOptions = Array.isArray(options) ? options : []
  return (
    <div className="relative">
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label={label}
        className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 pr-8 text-sm text-foreground outline-none transition-colors focus:border-quant-gold"
      >
        {safeOptions.map((opt) => {
          const optValue = typeof opt === 'string' ? opt : opt.value
          const optLabel = typeof opt === 'string' ? opt : opt.label
          return (
            <option key={optValue} value={optValue}>
              {optLabel}
            </option>
          )
        })}
      </select>
      <ChevronRight className="pointer-events-none absolute right-2.5 top-1/2 h-4 w-4 -translate-y-1/2 rotate-90 text-muted-foreground" />
    </div>
  )
}

function NumberInput({
  value,
  onChange,
  placeholder,
  min,
  max,
}: {
  value: number
  onChange: (v: number) => void
  placeholder?: string
  min?: number
  max?: number
}) {
  return (
    <input
      type="number"
      min={min}
      max={max}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-foreground placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

/* ------------------------------------------------------------------ */
/*  Main Component                                                     */
/* ------------------------------------------------------------------ */

export function Settings() {
  const queryClient = useQueryClient()
  const app = useAppStore()
  const { t, lang, setLang } = useI18n()

  /* ── Backend config ── */
  const { data: backendConfig, isLoading: configLoading } = useQuery({
    queryKey: ['config'],
    queryFn: () => configApi.get(),
  })

  // 交易所"已配置"状态（保险库为准；密钥本身永不下发）。
  const { data: exchangesConfigured } = useQuery({
    queryKey: ['configured-exchanges'],
    queryFn: () => configApi.exchangesConfigured(),
    staleTime: 30000,
  })

  // AI 模型目录（后端 GET /config/ai-models；失败/为空时回落内置完整目录）。
  const { data: aiModelsCatalog } = useQuery({
    queryKey: ['ai-models-catalog'],
    queryFn: () => configApi.aiModels(),
    staleTime: 300000,
    retry: 1,
  })

  // 生效目录：后端目录优先，条目字段不完整时用内置目录补齐兜底。
  const aiCatalog: AIModelCatalogItem[] = useMemo(() => {
    const remote = Array.isArray(aiModelsCatalog?.providers) ? aiModelsCatalog.providers : []
    if (remote.length === 0) return FALLBACK_AI_CATALOG
    return remote.map((p) => {
      const fb = FALLBACK_AI_CATALOG.find((f) => f.key === p.key)
      return {
        key: p.key,
        label: p.label || fb?.label || p.key,
        models: Array.isArray(p.models) && p.models.length > 0 ? p.models : fb?.models || [],
        baseUrl: p.baseUrl || fb?.baseUrl || '',
        default: p.default || fb?.default,
        configured: !!p.configured,
        current_model: p.current_model,
      }
    })
  }, [aiModelsCatalog])

  /* ── Backend-backed form state ── */
  const [defaultExchange, setDefaultExchange] = useState('binance')
  const [exchanges, setExchanges] = useState<Record<string, ExchangeConfig>>({})
  const [testErrors, setTestErrors] = useState<Record<string, string>>({})
  const [defaultAIProvider, setDefaultAIProvider] = useState('openai')
  const [aiProviders, setAiProviders] = useState<Record<string, AIProviderConfig>>({})
  const [profitProtection, setProfitProtection] = useState(false)
  const [maxOrders, setMaxOrders] = useState(5)
  const [dirty, setDirty] = useState(false)
  // P0-1：密钥草稿——输入框只进草稿，留空保存=保持现有保险库凭证不变。
  const [credDrafts, setCredDrafts] = useState<Record<string, { api_key?: string; secret?: string; passphrase?: string }>>({})

  /* ── Local settings (frontend-only) ── */
  const [notifyEmail, setNotifyEmail] = useState<NotifyConfig>(() =>
    loadLocal('xt-notify-email', { enabled: true, address: '' })
  )
  const [notifyTelegram, setNotifyTelegram] = useState<NotifyConfig>(() =>
    loadLocal('xt-notify-telegram', { enabled: false, botToken: '', chatId: '' })
  )
  const [notifyDingtalk, setNotifyDingtalk] = useState<NotifyConfig>(() =>
    loadLocal('xt-notify-dingtalk', { enabled: false, webhook: '' })
  )
  const [dataSettings, setDataSettings] = useState<DataConfig>(() =>
    loadLocal('xt-data', { klineLimit: 5000, autoCleanup: true, realtime: true })
  )
  const [securitySettings, setSecuritySettings] = useState<SecurityConfig>(() =>
    loadLocal('xt-security', { twoFactor: false, sessionTimeout: 60, ipWhitelist: '' })
  )

  /* ── Notify routing ── */
  const { data: notifyRoutesData, isLoading: notifyRoutesLoading } = useQuery({
    queryKey: ['notify-routes'],
    queryFn: () => notifyRouteApi.list(),
  })

  const [editingRule, setEditingRule] = useState<RouteRule | null>(null)
  const [isRuleFormOpen, setIsRuleFormOpen] = useState(false)

  const saveRuleMut = useMutation({
    mutationFn: (rule: RouteRule) => {
      // Adapt RouteRule to NotifyRoute shape expected by the API
      const payload: Parameters<typeof notifyRouteApi.save>[0] = {
        id: rule.id,
        channel: rule.channels[0] || 'log',
        enabled: rule.enabled,
        events: rule.events,
        config: {
          levels: rule.levels,
          minReturnPct: rule.minReturnPct,
        },
      }
      return notifyRouteApi.save(payload)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notify-routes'] })
      setIsRuleFormOpen(false)
      setEditingRule(null)
    },
  })

  const deleteRuleMut = useMutation({
    mutationFn: (id: string) => notifyRouteApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notify-routes'] })
    },
  })

  const testChannelMut = useMutation<{ success: boolean }, Error, { channel: string; message?: string }>({
    mutationFn: ({ channel, message }: { channel: string; message?: string }) => notifyRouteApi.test(channel, message),
  })

  const notifyRoutes: RouteRule[] = useMemo(() => {
    if (!Array.isArray(notifyRoutesData)) return []
    return notifyRoutesData.map((r: unknown) => {
      const raw = r as Record<string, unknown>
      return {
        id: String(raw.id || ''),
        name: String(raw.name || raw.channel || ''),
        events: Array.isArray(raw.events) ? (raw.events as string[]) : [],
        levels: Array.isArray((raw.config as Record<string, unknown>)?.levels)
          ? ((raw.config as Record<string, unknown>).levels as string[])
          : [],
        channels: [String(raw.channel || 'log')],
        enabled: Boolean(raw.enabled),
        minReturnPct: Number((raw.config as Record<string, unknown>)?.minReturnPct || 0) || undefined,
      }
    })
  }, [notifyRoutesData])

  /* ── Sync backend config into form state ── */
  useEffect(() => {
    if (!backendConfig) return
    setDefaultExchange(((backendConfig as Record<string, unknown>).default_exchange as string) || 'binance')
    setExchanges(((backendConfig as Record<string, unknown>).exchanges || {}) as Record<string, ExchangeConfig>)
    const rawAI = ((backendConfig as Record<string, unknown>).ai || {}) as Record<string, AIProviderConfig>
    // legacy 迁移：旧版前端用 anthropic 作 key，后端注册表为 claude——合并到 claude 并删除旧键。
    const migratedAI: Record<string, AIProviderConfig> = {}
    for (const [k, v] of Object.entries(rawAI)) {
      const key = k === 'anthropic' ? 'claude' : k
      migratedAI[key] = { ...(migratedAI[key] || {}), ...v }
    }
    const rawDefault = ((backendConfig as Record<string, unknown>).default_ai_provider as string) || 'openai'
    setDefaultAIProvider(rawDefault === 'anthropic' ? 'claude' : rawDefault)
    setAiProviders(migratedAI)
    const risk = ((backendConfig as Record<string, unknown>).risk as Record<string, unknown>) || {}
    setProfitProtection(!!risk.profit_protection_enabled)
    setMaxOrders(typeof risk.max_concurrent_orders === 'number' ? risk.max_concurrent_orders : 5)
    setDirty(false)
  }, [backendConfig])

  /* ── Helpers to mutate nested state ── */
  const setExchangeField = useCallback((name: string, field: keyof ExchangeConfig, val: string | number | boolean) => {
    setExchanges((prev) => {
      const next = { ...prev, [name]: { ...(prev[name] || {}), [field]: val } }
      return next
    })
    setDirty(true)
  }, [])

  const setAIField = useCallback((provider: string, field: keyof AIProviderConfig, val: string | number | boolean) => {
    setAiProviders((prev) => {
      const next = { ...prev, [provider]: { ...(prev[provider] || {}), [field]: val } }
      return next
    })
    setDirty(true)
  }, [])

  /* ── Save mutation ── */
  const saveMut = useMutation({
    mutationFn: async () => {
      // P0-1：有密钥草稿的交易所先写保险库（空字段=保留现有凭证）。
      const credWrites: Promise<unknown>[] = []
      for (const [name, draft] of Object.entries(credDrafts)) {
        if (draft.api_key?.trim() || draft.secret?.trim() || draft.passphrase?.trim()) {
          credWrites.push(
            configApi.saveExchangeCredentials({
              name,
              api_key: draft.api_key?.trim() || '',
              secret: draft.secret?.trim() || '',
              passphrase: draft.passphrase?.trim() || '',
            })
          )
        }
      }
      if (credWrites.length > 0) await Promise.all(credWrites)
      // PUT /config：非敏感字段照常；密钥字段置空（后端也会剥离，双保险）。
      const sanitizedExchanges: Record<string, Record<string, unknown>> = {}
      for (const [name, cfg] of Object.entries(exchanges)) {
        sanitizedExchanges[name] = { ...cfg, api_key: '', secret: '', passphrase: '' }
      }
      const payload = {
        ...(backendConfig || {}),
        default_exchange: defaultExchange,
        exchanges: sanitizedExchanges,
        default_ai_provider: defaultAIProvider,
        ai: aiProviders,
      }
      await configApi.save(payload)
      // P0-2：风控三参数走 /api/risk/config，当前进程即时生效（与风险中心同源）。
      const current = await riskApi.getConfig()
      await riskApi.updateConfig({
        max_concurrent_orders: maxOrders,
        position_limit_pct: current?.position_limit_pct ?? 100,
        profit_protection_enabled: profitProtection,
        indicator_fail_open: current?.indicator_fail_open ?? true,
      })
      return { ok: true }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      queryClient.invalidateQueries({ queryKey: ['configured-exchanges'] })
      queryClient.invalidateQueries({ queryKey: ['risk-config'] })
      setCredDrafts({})
      setDirty(false)
    },
  })

  /* ── Test mutations ── */
  const testExchangeMut = useMutation<ExchangeTestResult, Error, { name: string; cfg: ExchangeConfig }>({
    mutationFn: async ({ name, cfg }) => {
      // P0-3：测试时优先用草稿里的新凭证；为空回退到保险库（服务端处理）。
      const result = await configApi.exchangeTest({
        id: name,
        name,
        api_key: cfg.api_key || '',
        secret: cfg.secret || '',
        passphrase: cfg.passphrase || '',
        enabled: true,
      })
      // 后端失败分支统一为 success:false + HTTP 200，必须按 success 判定。
      if (result?.success === false) throw new Error(result?.message || t('settings.connectFailedFallback'))
      return result
    },
    onError: (err: Error, vars) => {
      setTestErrors((prev) => ({ ...prev, [vars.name]: err.message }))
    },
    onSuccess: (_data, vars) => {
      setTestErrors((prev) => ({ ...prev, [vars.name]: '' }))
    },
  })

  const testAIMut = useMutation<ExchangeTestResult, Error, { provider: string; cfg: AIProviderConfig }>({
    mutationFn: async ({ provider, cfg }: { provider: string; cfg: AIProviderConfig }) => {
      const prov = aiCatalog.find((p) => p.key === provider)
      return configApi.aiTest({
        provider,
        api_key: cfg.api_key || '',
        model: cfg.model || '',
        base_url: cfg.base_url || prov?.baseUrl || '',
      })
    },
    onSuccess: (data) => {
      if (data?.success) toast('success', data?.message || t('settings.connectOk'))
    },
    onError: (err: Error) => {
      toast('error', err.message || t('settings.connectFail'))
    },
  })

  /* ── Local settings persist ── */
  const persistLocal = useCallback(() => {
    saveLocal('xt-notify-email', notifyEmail)
    saveLocal('xt-notify-telegram', notifyTelegram)
    saveLocal('xt-notify-dingtalk', notifyDingtalk)
    saveLocal('xt-data', dataSettings)
    saveLocal('xt-security', securitySettings)
  }, [notifyEmail, notifyTelegram, notifyDingtalk, dataSettings, securitySettings])

  /* ── Restart banner ── */
  const [copied, setCopied] = useState(false)
  const handleCopyRestart = useCallback(() => {
    navigator.clipboard.writeText('docker compose restart gateway')
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [])

  /* ── System info ── */
  const [systemInfo, setSystemInfo] = useState<{ version?: string; status?: string; buildTime?: string }>({})
  const [logs, setLogs] = useState<string>('')

  useEffect(() => {
    fetch('/api/health')
      .then((r) => r.json())
      .then((data) => {
        if (data?.data) {
          setSystemInfo({
            version: data.data.version,
            status: data.data.status,
            buildTime: data.data.buildTime,
          })
        }
      })
      .catch(() => {})

    fetch('/api/logs?tail=100')
      .then((r) => (r.ok ? r.text() : ''))
      .then((text) => setLogs(text || t('settings.system.noLogs')))
      .catch(() => setLogs(t('settings.system.logsUnavailable')))
  }, [])

  /* ── Active tab ── */
  const [activeTab, setActiveTab] = useState('general')

  const tabs = [
    { key: 'general', label: t('settings.nav.general'), icon: Globe, local: true },
    { key: 'exchange', label: t('settings.nav.exchange'), icon: Globe },
    { key: 'ai', label: t('settings.nav.ai'), icon: BrainCircuit },
    { key: 'notify', label: t('settings.nav.notify'), icon: Bell, local: true },
    { key: 'notify-routing', label: t('settings.nav.notify-routing'), icon: Route },
    { key: 'appearance', label: t('settings.nav.appearance'), icon: Palette, local: true },
    { key: 'data', label: t('settings.nav.data'), icon: Database, local: true },
    { key: 'security', label: t('settings.nav.security'), icon: Shield, local: true },
    { key: 'system', label: t('settings.nav.system'), icon: Info },
  ] as const

  const isSaving = saveMut.isPending

  /* ═══════════════════════════════════════════════════════════════
     Render helpers
     ═══════════════════════════════════════════════════════════════ */

  if (configLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-quant-gold" />
        <span className="ml-2 text-sm text-muted-foreground">{t('settings.loadingConfig')}</span>
      </div>
    )
  }

  return (
    <div className="h-full flex flex-col bg-quant-bg">
      {/* Fixed header */}
      <div className="shrink-0 pl-4 pr-6 pt-2 pb-2">
        {/* Restart alert */}
        {dirty && (
          <div className="mb-3 flex items-center gap-3 rounded-lg border border-amber-500/20 bg-amber-500/10 px-4 py-3">
            <AlertTriangle className="h-5 w-5 shrink-0 text-amber-400" />
            <div className="flex-1 text-sm text-amber-200">
              {t('settings.restartBanner')}
              <code className="ml-2 rounded bg-amber-500/20 px-1.5 py-0.5 text-xs font-mono text-amber-300">
                docker compose restart gateway
              </code>
            </div>
            <button
              onClick={handleCopyRestart}
              className="flex items-center gap-1.5 rounded-md bg-amber-500/20 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/30"
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {copied ? t('settings.copied') : t('settings.copyCmd')}
            </button>
          </div>
        )}

        {/* Page header */}
        <PageHeader
          subtitle={t('settings.subtitle')}
          actions={
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  if (backendConfig) {
                    setDefaultExchange(
                      ((backendConfig as Record<string, unknown>).default_exchange as string) || 'binance'
                    )
                    setExchanges(
                      ((backendConfig as Record<string, unknown>).exchanges || {}) as Record<string, ExchangeConfig>
                    )
                    setDefaultAIProvider(
                      ((backendConfig as Record<string, unknown>).default_ai_provider as string) || 'openai'
                    )
                    setAiProviders(
                      ((backendConfig as Record<string, unknown>).ai || {}) as Record<string, AIProviderConfig>
                    )
                    const risk = ((backendConfig as Record<string, unknown>).risk as Record<string, unknown>) || {}
                    setProfitProtection(!!risk.profit_protection_enabled)
                    setMaxOrders(typeof risk.max_concurrent_orders === 'number' ? risk.max_concurrent_orders : 5)
                  }
                  setDirty(false)
                }}
                className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground"
              >
                <RotateCcw className="h-3.5 w-3.5" />
                {t('settings.reset')}
              </button>
              <button
                onClick={() => {
                  persistLocal()
                  saveMut.mutate()
                }}
                disabled={isSaving}
                className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50"
              >
                {isSaving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                {isSaving ? t('settings.saving') : t('settings.save')}
              </button>
            </div>
          }
        />
      </div>

      {/* Content area: left nav + right panel */}
      <div className="flex-1 flex gap-5 pl-4 pr-6 min-h-0">
        {/* Left nav — independently scrollable */}
        <div className="w-36 shrink-0 space-y-0.5 overflow-y-auto">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={cn(
                'flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors',
                activeTab === tab.key
                  ? 'bg-quant-gold/10 text-quant-gold'
                  : 'text-muted-foreground hover:bg-quant-card hover:text-foreground'
              )}
            >
              <tab.icon className="h-4 w-4 shrink-0" />
              <span className="flex-1">{tab.label}</span>
              {'local' in tab && tab.local && (
                <span className="rounded bg-quant-border px-1.5 py-0.5 text-[10px] text-foreground">{t('settings.localBadge')}</span>
              )}
            </button>
          ))}
        </div>

        {/* Right content — scrollable */}
        <div className="flex-1 space-y-4 overflow-y-auto min-h-0">
          {/* ── GENERAL ── */}
          {activeTab === 'general' && (
            <>
              <SectionCard title={t('settings.general.title')} bodyClassName="space-y-5">
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.general.language')}</label>
                  <SelectField
                    value={app.language}
                    onChange={(v) => app.setLanguage(v)}
                    options={['zh-CN', 'en', 'ja']}
                    label={t('settings.general.language')}
                  />
                  <p className="mt-1.5 text-[10px] text-muted-foreground">{t('settings.general.languageHint')}</p>
                </div>
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">{t('settings.general.darkTheme')}</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">{t('settings.general.darkThemeHint')}</div>
                  </div>
                  <Toggle value={app.theme === 'dark'} onChange={(v) => app.setTheme(v ? 'dark' : 'light')} />
                </label>
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.general.timezone')}</label>
                  <SelectField
                    value={dataSettings.timezone || 'Asia/Shanghai'}
                    onChange={(v) => setDataSettings((p) => ({ ...p, timezone: v }))}
                    options={TIMEZONES}
                    label={t('settings.general.timezone')}
                  />
                </div>
              </SectionCard>
              <CurrencySelector />
            </>
          )}

          {/* ── EXCHANGE ── */}
          {activeTab === 'exchange' && (
            <>
              <SectionCard title={t('settings.exchange.defaultTitle')} bodyClassName="space-y-4">
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.exchange.defaultLabel')}</label>
                    <SelectField
                      value={defaultExchange}
                      onChange={(v) => {
                        setDefaultExchange(v)
                        setDirty(true)
                      }}
                      options={EXCHANGES.map((e) => e.label)}
                      label={t('settings.exchange.defaultLabel')}
                    />
                  </div>
                </div>
              </SectionCard>

              {EXCHANGES.map((ex) => {
                const cfg = exchanges[ex.key] || {}
                const testStatus =
                  testExchangeMut.variables?.name === ex.key
                    ? testExchangeMut.isPending
                      ? 'testing'
                      : testExchangeMut.isSuccess
                        ? 'ok'
                        : testExchangeMut.isError
                          ? 'error'
                          : null
                    : null
                return (
                  <SectionCard key={ex.key} title={ex.label} bodyClassName="space-y-4">
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">API Key</label>
                        <PasswordInput
                          value={credDrafts[ex.key]?.api_key || ''}
                          onChange={(v) => {
                            setCredDrafts((prev) => ({ ...prev, [ex.key]: { ...prev[ex.key], api_key: v } }))
                            setDirty(true)
                          }}
                          placeholder={
                            exchangesConfigured?.[ex.key]?.has_credentials
                              ? t('settings.exchange.credConfiguredPlaceholder')
                              : t('settings.exchange.enterApiKeyPlaceholder')
                          }
                        />
                      </div>
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">API Secret</label>
                        <PasswordInput
                          value={credDrafts[ex.key]?.secret || ''}
                          onChange={(v) => {
                            setCredDrafts((prev) => ({ ...prev, [ex.key]: { ...prev[ex.key], secret: v } }))
                            setDirty(true)
                          }}
                          placeholder={
                            exchangesConfigured?.[ex.key]?.has_credentials
                              ? t('settings.exchange.credConfiguredPlaceholder')
                              : t('settings.exchange.enterApiSecretPlaceholder')
                          }
                        />
                      </div>
                      {ex.needsPassphrase && (
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">Passphrase</label>
                          <PasswordInput
                            value={credDrafts[ex.key]?.passphrase || ''}
                            onChange={(v) => {
                              setCredDrafts((prev) => ({ ...prev, [ex.key]: { ...prev[ex.key], passphrase: v } }))
                              setDirty(true)
                            }}
                            placeholder={
                              exchangesConfigured?.[ex.key]?.has_credentials
                                ? t('settings.exchange.credConfiguredPlaceholder')
                                : t('settings.exchange.enterPassphrasePlaceholder')
                            }
                          />
                        </div>
                      )}
                      <div className="flex items-center gap-6 md:col-span-2">
                        <label className="flex items-center gap-2 text-xs text-muted-foreground">
                          <Toggle value={!!cfg.testnet} onChange={(v) => setExchangeField(ex.key, 'testnet', v)} />
                          {t('settings.exchange.useTestnet')}
                        </label>
                        <label className="flex items-center gap-2 text-xs text-muted-foreground">
                          <Toggle value={!!cfg.futures} onChange={(v) => setExchangeField(ex.key, 'futures', v)} />
                          {t('settings.exchange.enableFutures')}
                        </label>
                        <label className="flex items-center gap-2 text-xs text-muted-foreground">
                          <Toggle value={!!cfg.enabled} onChange={(v) => setExchangeField(ex.key, 'enabled', v)} />
                          {t('settings.exchange.enableExchange')}
                        </label>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() =>
                          testExchangeMut.mutate({
                            name: ex.key,
                            // 草稿优先（用户刚输入的新凭证）；为空时后端回退保险库/提示缺凭证。
                            cfg: { ...cfg, ...(credDrafts[ex.key] || {}) } as typeof cfg,
                          })
                        }
                        disabled={testExchangeMut.isPending && testExchangeMut.variables?.name === ex.key}
                        className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground disabled:opacity-50"
                      >
                        {testStatus === 'testing' ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : testStatus === 'ok' ? (
                          <Wifi className="h-3.5 w-3.5 text-emerald-400" />
                        ) : testStatus === 'error' ? (
                          <WifiOff className="h-3.5 w-3.5 text-red-400" />
                        ) : (
                          <Wifi className="h-3.5 w-3.5" />
                        )}
                        {testStatus === 'testing'
                          ? t('settings.testing')
                          : testStatus === 'ok'
                            ? t('settings.connectOk')
                            : testStatus === 'error'
                              ? testErrors[ex.key] || t('settings.connectFail')
                              : t('settings.testConnection')}
                      </button>
                    </div>
                  </SectionCard>
                )
              })}

              {/* Currency Preference */}
              <CurrencySelector />
            </>
          )}

          {/* ── AI ── */}
          {activeTab === 'ai' && (
            <>
              <SectionCard title={t('settings.ai.defaultTitle')} bodyClassName="space-y-4">
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.ai.defaultLabel')}</label>
                    <SelectField
                      value={defaultAIProvider}
                      onChange={(v) => {
                        setDefaultAIProvider(v)
                        setDirty(true)
                      }}
                      options={aiCatalog.map((p) => ({ value: p.key, label: p.label }))}
                      label={t('settings.ai.defaultLabel')}
                    />
                  </div>
                </div>
              </SectionCard>

              {aiCatalog.map((prov) => {
                const cfg = aiProviders[prov.key] || {}
                const effectiveModel = cfg.model || prov.default || prov.models[0] || ''
                const testStatus =
                  testAIMut.variables?.provider === prov.key
                    ? testAIMut.isPending
                      ? 'testing'
                      : testAIMut.isSuccess
                        ? 'ok'
                        : testAIMut.isError
                          ? 'error'
                          : null
                    : null
                return (
                  <SectionCard
                    key={prov.key}
                    title={prov.label}
                    bodyClassName="space-y-4"
                    headerAction={
                      <div className="flex items-center gap-2">
                        {prov.configured && (
                          <Badge variant="success">{t('settings.ai.configured')}</Badge>
                        )}
                        {prov.current_model && prov.current_model !== effectiveModel && (
                          <span className="text-xs text-muted-foreground">
                            {t('settings.ai.runtimeModel')}: {prov.current_model}
                          </span>
                        )}
                      </div>
                    }
                  >
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">API Key</label>
                        <PasswordInput
                          value={cfg.api_key || ''}
                          onChange={(v) => setAIField(prov.key, 'api_key', v)}
                          placeholder={`${t('settings.ai.apiKeyPlaceholderPrefix')}${prov.label}${t('settings.ai.apiKeyPlaceholderSuffix')}`}
                        />
                      </div>
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.ai.defaultModel')}</label>
                        <input
                          list={`ai-models-${prov.key}`}
                          value={effectiveModel}
                          onChange={(e) => setAIField(prov.key, 'model', e.target.value)}
                          aria-label={`${prov.label} ${t('settings.ai.defaultModel')}`}
                          placeholder={prov.models[0] || ''}
                          className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-foreground outline-none transition-colors focus:border-quant-gold"
                        />
                        <datalist id={`ai-models-${prov.key}`}>
                          {prov.models.map((m) => (
                            <option key={m} value={m} />
                          ))}
                        </datalist>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() =>
                          testAIMut.mutate({
                            provider: prov.key,
                            cfg: { ...cfg, model: effectiveModel, base_url: cfg.base_url || prov.baseUrl },
                          })
                        }
                        disabled={testAIMut.isPending && testAIMut.variables?.provider === prov.key}
                        className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground disabled:opacity-50"
                      >
                        {testStatus === 'testing' ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : testStatus === 'ok' ? (
                          <Wifi className="h-3.5 w-3.5 text-emerald-400" />
                        ) : testStatus === 'error' ? (
                          <WifiOff className="h-3.5 w-3.5 text-red-400" />
                        ) : (
                          <Wifi className="h-3.5 w-3.5" />
                        )}
                        {testStatus === 'testing'
                          ? t('settings.testing')
                          : testStatus === 'ok'
                            ? t('settings.connectOk')
                            : testStatus === 'error'
                              ? t('settings.connectFail')
                              : t('settings.testConnection')}
                      </button>
                    </div>
                  </SectionCard>
                )
              })}
            </>
          )}

          {/* ── NOTIFY (local) ── */}
          {activeTab === 'notify' && (
            <>
              <SectionCard title={t('settings.notify.emailTitle')} bodyClassName="space-y-4">
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Toggle value={notifyEmail.enabled} onChange={(v) => setNotifyEmail((p) => ({ ...p, enabled: v }))} />
                  {t('settings.notify.enableEmail')}
                </label>
                {notifyEmail.enabled && (
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.notify.recipientEmail')}</label>
                    <TextInput
                      value={notifyEmail.address || ''}
                      onChange={(v) => setNotifyEmail((p) => ({ ...p, address: v }))}
                      placeholder="admin@example.com"
                    />
                  </div>
                )}
              </SectionCard>
              <SectionCard title={t('settings.notify.telegramTitle')} bodyClassName="space-y-4">
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Toggle
                    value={notifyTelegram.enabled}
                    onChange={(v) => setNotifyTelegram((p) => ({ ...p, enabled: v }))}
                  />
                  {t('settings.notify.enableTelegram')}
                </label>
                {notifyTelegram.enabled && (
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">Bot Token</label>
                      <PasswordInput
                        value={notifyTelegram.botToken || ''}
                        onChange={(v) => setNotifyTelegram((p) => ({ ...p, botToken: v }))}
                        placeholder={t('settings.notify.enterBotToken')}
                      />
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">Chat ID</label>
                      <TextInput
                        value={notifyTelegram.chatId || ''}
                        onChange={(v) => setNotifyTelegram((p) => ({ ...p, chatId: v }))}
                        placeholder={t('settings.notify.enterChatId')}
                      />
                    </div>
                  </div>
                )}
              </SectionCard>
              <SectionCard title={t('settings.notify.dingtalkTitle')} bodyClassName="space-y-4">
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Toggle
                    value={notifyDingtalk.enabled}
                    onChange={(v) => setNotifyDingtalk((p) => ({ ...p, enabled: v }))}
                  />
                  {t('settings.notify.enableDingtalk')}
                </label>
                {notifyDingtalk.enabled && (
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.notify.webhookAddress')}</label>
                    <PasswordInput
                      value={notifyDingtalk.webhook || ''}
                      onChange={(v) => setNotifyDingtalk((p) => ({ ...p, webhook: v }))}
                      placeholder={t('settings.notify.enterDingtalkWebhook')}
                    />
                  </div>
                )}
              </SectionCard>

              <SectionCard title={t('settings.notify.tvTitle')}>
                <div className="space-y-3">
                  <p className="text-xs text-muted-foreground">
                    {t('settings.notify.tvDesc')}
                  </p>
                  <div>
                    <label className="mb-1 block text-[10px] text-muted-foreground">Webhook URL</label>
                    <div className="flex items-center gap-2">
                      <code className="flex-1 rounded-lg border border-quant-gold/30 bg-quant-bg px-3 py-2 text-xs text-quant-gold break-all">
                        {window.location.origin}/api/webhook/tv
                      </code>
                      <button
                        onClick={() => {
                          navigator.clipboard.writeText(window.location.origin + '/api/webhook/tv')
                        }}
                        className="px-3 py-2 rounded-lg border border-quant-border text-xs hover:bg-white/5 shrink-0"
                      >
                        {t('settings.copy')}
                      </button>
                    </div>
                  </div>
                  <div className="p-3 rounded-lg bg-quant-bg-secondary space-y-1.5">
                    <p className="text-[10px] font-medium text-foreground">{t('settings.notify.tvStepsTitle')}</p>
                    <ol className="text-[10px] text-muted-foreground space-y-0.5 list-decimal list-inside">
                      <li>{t('settings.notify.tvStep1')}</li>
                      <li>{t('settings.notify.tvStep2')}</li>
                      <li>{t('settings.notify.tvStep3')}</li>
                    </ol>
                  </div>
                  <div className="p-3 rounded-lg bg-quant-bg-tertiary">
                    <p className="text-[10px] text-muted-foreground mb-1">{t('settings.notify.tvMessageExample')}</p>
                    <pre className="text-[10px] font-mono text-foreground/80 whitespace-pre-wrap">{`{"symbol":"BTCUSDT","action":"buy","price":"50000","quantity":"0.1","strategy":"TV_MA_Cross"}`}</pre>
                  </div>
                  <div className="text-[10px] text-muted-foreground">
                    <span className="font-medium">{t('settings.notify.tvActionLabel')}</span> {t('settings.notify.tvActionValues')}
                  </div>
                </div>
              </SectionCard>
            </>
          )}

          {/* ── NOTIFY ROUTING ── */}
          {activeTab === 'notify-routing' && (
            <>
              <SectionCard title={t('settings.routing.title')} bodyClassName="space-y-4">
                <div className="flex items-center justify-between">
                  <p className="text-xs text-muted-foreground">{t('settings.routing.desc')}</p>
                  <button
                    onClick={() => {
                      setEditingRule({
                        id: '',
                        name: '',
                        events: [],
                        levels: ['INFO'],
                        channels: ['log'],
                        enabled: true,
                      })
                      setIsRuleFormOpen(true)
                    }}
                    className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90"
                  >
                    <Zap className="h-3.5 w-3.5" />
                    {t('settings.routing.addRule')}
                  </button>
                </div>

                {isRuleFormOpen && editingRule && (
                  <div className="rounded-lg border border-quant-border bg-quant-bg p-4 space-y-4">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.routing.ruleName')}</label>
                      <TextInput
                        value={editingRule.name}
                        onChange={(v) => setEditingRule((p) => (p ? { ...p, name: v } : p))}
                        placeholder={t('settings.routing.ruleNamePlaceholder')}
                      />
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.routing.eventTypes')}</label>
                      <div className="flex flex-wrap gap-2">
                        {EVENTS.map((ev) => (
                          <label
                            key={ev}
                            className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-2.5 py-1.5 text-xs text-muted-foreground cursor-pointer hover:border-quant-gold/30"
                          >
                            <input
                              type="checkbox"
                              className="accent-quant-gold"
                              checked={editingRule.events.includes(ev)}
                              onChange={(e) => {
                                setEditingRule((p) => {
                                  if (!p) return p
                                  const next = e.target.checked ? [...p.events, ev] : p.events.filter((x) => x !== ev)
                                  return { ...p, events: next }
                                })
                              }}
                            />
                            {t(EVENT_LABELS[ev] || ev)}
                          </label>
                        ))}
                      </div>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.routing.levels')}</label>
                      <div className="flex flex-wrap gap-2">
                        {LEVELS.map((lv) => (
                          <label
                            key={lv}
                            className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-2.5 py-1.5 text-xs text-muted-foreground cursor-pointer hover:border-quant-gold/30"
                          >
                            <input
                              type="checkbox"
                              className="accent-quant-gold"
                              checked={editingRule.levels.includes(lv)}
                              onChange={(e) => {
                                setEditingRule((p) => {
                                  if (!p) return p
                                  const next = e.target.checked ? [...p.levels, lv] : p.levels.filter((x) => x !== lv)
                                  return { ...p, levels: next }
                                })
                              }}
                            />
                            {t(LEVEL_LABELS[lv] || lv)}
                          </label>
                        ))}
                      </div>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.routing.channels')}</label>
                      <div className="flex flex-wrap gap-2">
                        {CHANNELS.map((ch) => (
                          <label
                            key={ch}
                            className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-2.5 py-1.5 text-xs text-muted-foreground cursor-pointer hover:border-quant-gold/30"
                          >
                            <input
                              type="checkbox"
                              className="accent-quant-gold"
                              checked={editingRule.channels.includes(ch)}
                              onChange={(e) => {
                                setEditingRule((p) => {
                                  if (!p) return p
                                  const next = e.target.checked
                                    ? [...p.channels, ch]
                                    : p.channels.filter((x) => x !== ch)
                                  return { ...p, channels: next }
                                })
                              }}
                            />
                            {t(CHANNEL_LABELS[ch] || ch)}
                          </label>
                        ))}
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <label className="flex items-center gap-2 text-xs text-muted-foreground">
                        <Toggle
                          value={editingRule.enabled}
                          onChange={(v) => setEditingRule((p) => (p ? { ...p, enabled: v } : p))}
                        />
                        {t('settings.routing.enableRule')}
                      </label>
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => saveRuleMut.mutate(editingRule)}
                        disabled={saveRuleMut.isPending || !editingRule.name.trim()}
                        className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black transition-opacity hover:opacity-90 disabled:opacity-50"
                      >
                        {saveRuleMut.isPending ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Save className="h-3.5 w-3.5" />
                        )}
                        {saveRuleMut.isPending ? t('settings.saving') : t('settings.save')}
                      </button>
                      <button
                        onClick={() => {
                          setIsRuleFormOpen(false)
                          setEditingRule(null)
                        }}
                        className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground"
                      >
                        {t('settings.cancel')}
                      </button>
                    </div>
                  </div>
                )}

                {notifyRoutesLoading ? (
                  <div className="flex items-center gap-2 py-4">
                    <Loader2 className="h-4 w-4 animate-spin text-quant-gold" />
                    <span className="text-xs text-muted-foreground">{t('settings.loading')}</span>
                  </div>
                ) : notifyRoutes.length === 0 ? (
                  <div className="py-6 text-center text-xs text-muted-foreground">{t('settings.routing.empty')}</div>
                ) : (
                  <div className="space-y-3">
                    {notifyRoutes.map((rule: RouteRule) => (
                      <div key={rule.id} className="rounded-lg border border-quant-border bg-quant-bg p-4 space-y-3">
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-3">
                            <span className="text-sm font-medium text-foreground">{rule.name}</span>
                            <span
                              className={cn(
                                'rounded px-1.5 py-0.5 text-[10px]',
                                rule.enabled
                                  ? 'bg-emerald-500/10 text-emerald-400'
                                  : 'bg-quant-border text-muted-foreground'
                              )}
                            >
                              {rule.enabled ? t('settings.enabled') : t('settings.disabled')}
                            </span>
                          </div>
                          <div className="flex items-center gap-2">
                            <button
                              onClick={() => {
                                setEditingRule({ ...rule })
                                setIsRuleFormOpen(true)
                              }}
                              className="rounded-md border border-quant-border bg-quant-card px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground"
                            >
                              {t('settings.edit')}
                            </button>
                            <button
                              onClick={() => {
                                if (confirm(t('settings.routing.deleteConfirm'))) {
                                  deleteRuleMut.mutate(rule.id)
                                }
                              }}
                              disabled={deleteRuleMut.isPending && deleteRuleMut.variables === rule.id}
                              className="rounded-md border border-quant-border bg-quant-card px-2.5 py-1 text-xs text-red-400 transition-colors hover:border-red-400/30 hover:bg-red-400/10 disabled:opacity-50"
                            >
                              {t('settings.delete')}
                            </button>
                          </div>
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                          {(rule.events.length === 0 ? ['all'] : rule.events).map((ev) => (
                            <span key={ev} className="rounded bg-quant-border px-1.5 py-0.5 text-[10px] text-foreground">
                              {t(EVENT_LABELS[ev] || ev)}
                            </span>
                          ))}
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                          {rule.levels.map((lv) => (
                            <span
                              key={lv}
                              className={cn(
                                'rounded px-1.5 py-0.5 text-[10px]',
                                lv === 'CRITICAL'
                                  ? 'bg-red-500/10 text-red-400'
                                  : lv === 'WARN'
                                    ? 'bg-amber-500/10 text-amber-400'
                                    : 'bg-blue-500/10 text-blue-400'
                              )}
                            >
                              {t(LEVEL_LABELS[lv] || lv)}
                            </span>
                          ))}
                        </div>
                        <div className="flex flex-wrap gap-1.5">
                          {rule.channels.map((ch) => (
                            <span
                              key={ch}
                              className="rounded bg-quant-gold/10 px-1.5 py-0.5 text-[10px] text-quant-gold"
                            >
                              {t(CHANNEL_LABELS[ch] || ch)}
                            </span>
                          ))}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </SectionCard>

              <SectionCard title={t('settings.routing.channelTest')} bodyClassName="space-y-4">
                <p className="text-xs text-muted-foreground">{t('settings.routing.channelTestDesc')}</p>
                <div className="flex flex-wrap gap-2">
                  {CHANNELS.map((ch) => {
                    const testStatus =
                      testChannelMut.variables?.channel === ch
                        ? testChannelMut.isPending
                          ? 'testing'
                          : testChannelMut.isSuccess
                            ? 'ok'
                            : testChannelMut.isError
                              ? 'error'
                              : null
                        : null
                    return (
                      <button
                        key={ch}
                        onClick={() => testChannelMut.mutate({ channel: ch })}
                        disabled={testChannelMut.isPending && testChannelMut.variables?.channel === ch}
                        className={cn(
                          'flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs font-medium transition-colors',
                          testStatus === 'ok'
                            ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-400'
                            : testStatus === 'error'
                              ? 'border-red-500/30 bg-red-500/10 text-red-400'
                              : 'border-quant-border bg-quant-card text-muted-foreground hover:border-quant-gold/30 hover:text-foreground',
                          'disabled:opacity-50'
                        )}
                      >
                        {testStatus === 'testing' ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : testStatus === 'ok' ? (
                          <Check className="h-3.5 w-3.5" />
                        ) : testStatus === 'error' ? (
                          <WifiOff className="h-3.5 w-3.5" />
                        ) : (
                          <Wifi className="h-3.5 w-3.5" />
                        )}
                        {t(CHANNEL_LABELS[ch] || ch)}
                        {testStatus === 'testing'
                          ? t('settings.testing')
                          : testStatus === 'ok'
                            ? t('settings.success')
                            : testStatus === 'error'
                              ? t('settings.fail')
                              : ''}
                      </button>
                    )
                  })}
                </div>
              </SectionCard>
            </>
          )}

          {/* ── APPEARANCE (local) ── */}
          {activeTab === 'appearance' && (
            <SectionCard title={t('settings.appearance.title')} bodyClassName="space-y-5">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.appearance.uiScale')}</label>
                <SelectField
                  value={`${Math.round(app.uiScale * 100)}%`}
                  onChange={(v) => app.setUiScale(Number(v.replace('%', '')) / 100)}
                  options={['90%', '100%', '110%']}
                  label={t('settings.appearance.uiScale')}
                />
                <p className="mt-1.5 text-[10px] text-muted-foreground">{t('settings.appearance.uiScaleHint')}</p>
              </div>
              <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                <div>
                  <div className="text-sm font-medium text-foreground">{t('settings.appearance.sidebarHover')}</div>
                  <div className="mt-0.5 text-xs text-muted-foreground">
                    {app.sidebarBehavior === 'hover'
                      ? t('settings.appearance.sidebarHoverOn')
                      : t('settings.appearance.sidebarHoverOff')}
                  </div>
                </div>
                <Toggle
                  value={app.sidebarBehavior === 'hover'}
                  onChange={(v) => app.setSidebarBehavior(v ? 'hover' : 'click')}
                />
              </label>
            </SectionCard>
          )}

          {/* ── DATA (local) ── */}
          {activeTab === 'data' && (
            <SectionCard title={t('settings.data.title')} bodyClassName="space-y-5">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.data.klineLimit')}</label>
                <NumberInput
                  value={dataSettings.klineLimit}
                  onChange={(v) => setDataSettings((p) => ({ ...p, klineLimit: v }))}
                  min={100}
                  max={50000}
                />
              </div>
              <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                <div>
                  <div className="text-sm font-medium text-foreground">{t('settings.data.autoCleanup')}</div>
                  <div className="mt-0.5 text-xs text-muted-foreground">{t('settings.data.autoCleanupHint')}</div>
                </div>
                <Toggle
                  value={dataSettings.autoCleanup}
                  onChange={(v) => setDataSettings((p) => ({ ...p, autoCleanup: v }))}
                />
              </label>
              <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                <div>
                  <div className="text-sm font-medium text-foreground">{t('settings.data.realtime')}</div>
                  <div className="mt-0.5 text-xs text-muted-foreground">{t('settings.data.realtimeHint')}</div>
                </div>
                <Toggle
                  value={dataSettings.realtime}
                  onChange={(v) => setDataSettings((p) => ({ ...p, realtime: v }))}
                />
              </label>
            </SectionCard>
          )}

          {/* ── SECURITY (MFA + local) ── */}
          {activeTab === 'security' && (
            <>
              <MfaSection />
              <SectionCard title={t('settings.security.title')} bodyClassName="space-y-5">
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.security.sessionTimeout')}</label>
                  <NumberInput
                    value={securitySettings.sessionTimeout}
                    onChange={(v) => setSecuritySettings((p) => ({ ...p, sessionTimeout: v }))}
                    min={5}
                    max={1440}
                  />
                </div>
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">{t('settings.security.ipWhitelist')}</label>
                  <TextInput
                    value={securitySettings.ipWhitelist}
                    onChange={(v) => setSecuritySettings((p) => ({ ...p, ipWhitelist: v }))}
                    placeholder="192.168.1.0/24, 10.0.0.1"
                  />
                  <p className="mt-1 text-[11px] text-muted-foreground">{t('settings.security.ipWhitelistHint')}</p>
                </div>
              </SectionCard>
            </>
          )}

          {/* ── SYSTEM ── */}
          {activeTab === 'system' && (
            <>
              <DataDownloadSection />

              <SectionCard title={t('settings.system.title')} bodyClassName="space-y-4">
                <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
                  <div className="rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div className="text-xs text-muted-foreground">{t('settings.system.frontendVersion')}</div>
                    <div className="mt-1 text-sm font-medium text-foreground">
                      {import.meta.env.VITE_APP_VERSION || '2.0.0'}
                    </div>
                  </div>
                  <div className="rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div className="text-xs text-muted-foreground">{t('settings.system.backendVersion')}</div>
                    <div className="mt-1 text-sm font-medium text-foreground">{systemInfo.version || '-'}</div>
                  </div>
                  <div className="rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div className="text-xs text-muted-foreground">{t('settings.system.healthStatus')}</div>
                    <div
                      className={cn(
                        'mt-1 flex items-center gap-1.5 text-sm font-medium',
                        systemInfo.status === 'healthy' ? 'text-green-400' : 'text-red-400'
                      )}
                    >
                      {systemInfo.status === 'healthy' ? (
                        <Wifi className="h-3.5 w-3.5" />
                      ) : (
                        <WifiOff className="h-3.5 w-3.5" />
                      )}
                      {systemInfo.status === 'healthy' ? t('settings.system.healthy') : systemInfo.status || t('settings.system.unknown')}
                    </div>
                  </div>
                </div>
                {systemInfo.buildTime && (
                  <div className="rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div className="text-xs text-muted-foreground">{t('settings.system.buildTime')}</div>
                    <div className="mt-1 text-sm font-medium text-foreground">{systemInfo.buildTime}</div>
                  </div>
                )}
              </SectionCard>

              <SectionCard title={t('settings.system.logsTitle')} bodyClassName="space-y-4">
                <div className="flex items-center justify-between">
                  <div className="text-xs text-muted-foreground">{t('settings.system.logsDesc')}</div>
                  <button
                    onClick={() => {
                      fetch('/api/logs?tail=100')
                        .then((r) => (r.ok ? r.text() : t('settings.system.logsDisabled')))
                        .then(setLogs)
                        .catch(() => setLogs(t('settings.system.logsUnavailable')))
                    }}
                    className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground"
                  >
                    <Terminal className="h-3.5 w-3.5" /> {t('settings.system.refreshLogs')}
                  </button>
                </div>
                <pre className="max-h-96 overflow-auto rounded-lg border border-quant-border bg-black/40 p-3 text-[11px] leading-relaxed text-muted-foreground">
                  {logs || t('settings.system.clickToRefresh')}
                </pre>
              </SectionCard>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

/* ── Currency Selector Component ── */
const CURRENCIES = [
  { key: 'CNY', label: '🇨🇳 CNY ¥', symbol: '¥' },
  { key: 'USD', label: '🇺🇸 USD $', symbol: '$' },
  { key: 'EUR', label: '🇪🇺 EUR €', symbol: '€' },
  { key: 'HKD', label: '🇭🇰 HKD HK$', symbol: 'HK$' },
  { key: 'JPY', label: '🇯🇵 JPY ¥', symbol: '¥' },
  { key: 'GBP', label: '🇬🇧 GBP £', symbol: '£' },
]

function CurrencySelector() {
  const [currency, setCurrency] = useState('CNY')
  const [rates, setRates] = useState<Record<string, number>>({})
  const [saving, setSaving] = useState(false)
  const { t } = useI18n()

  useEffect(() => {
    configApi
      .currencyGet()
      .then((data: { currency?: string; rates?: Record<string, number> }) => {
        if (data?.currency) setCurrency(data.currency)
        if (data?.rates) setRates(data.rates)
      })
      .catch(() => {})
  }, [])

  const handleChange = async (cur: string) => {
    setCurrency(cur)
    setSaving(true)
    try {
      await configApi.currencySet(cur)
    } catch {
      /* ignore save error */
    }
    setSaving(false)
  }

  return (
    <SectionCard title={t('settings.currency.title')} bodyClassName="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t('settings.currency.desc')}
      </p>
      <div className="flex flex-wrap gap-2">
        {CURRENCIES.map((c) => (
          <button
            key={c.key}
            onClick={() => handleChange(c.key)}
            disabled={saving}
            className={cn(
              'rounded-lg border px-4 py-2 text-sm font-medium transition-all',
              currency === c.key
                ? 'border-quant-gold bg-quant-gold/10 text-quant-gold'
                : 'border-quant-border bg-quant-card text-muted-foreground hover:border-quant-gold/30 hover:text-foreground'
            )}
          >
            {c.label}
            {rates[c.key] && (
              <span className="ml-1.5 text-xs opacity-60">
                {rates[c.key] < 10 ? rates[c.key].toFixed(4) : rates[c.key].toFixed(2)}
              </span>
            )}
          </button>
        ))}
      </div>
      {saving && (
        <p className="text-xs text-quant-gold flex items-center gap-1">
          <Loader2 className="h-3 w-3 animate-spin" /> {t('settings.saving')}
        </p>
      )}
    </SectionCard>
  )
}
