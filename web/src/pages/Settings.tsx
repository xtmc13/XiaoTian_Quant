import { useState, useEffect, useCallback } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { configApi } from '@/lib/api'
import { useAppStore } from '@/stores/appStore'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
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

/* ------------------------------------------------------------------ */
/*  Constants                                                          */
/* ------------------------------------------------------------------ */

const EXCHANGES = [
  { key: 'binance', label: 'Binance', needsPassphrase: false },
  { key: 'okx', label: 'OKX', needsPassphrase: true },
  { key: 'coinbase', label: 'Coinbase', needsPassphrase: false },
  { key: 'gate', label: 'Gate.io', needsPassphrase: false },
  { key: 'mexc', label: 'MEXC', needsPassphrase: false },
  { key: 'bitget', label: 'Bitget', needsPassphrase: false },
] as const

const AI_PROVIDERS = [
  {
    key: 'openai',
    label: 'OpenAI',
    models: ['gpt-4o', 'gpt-4-turbo', 'o1-preview'],
    baseUrl: 'https://api.openai.com/v1',
  },
  {
    key: 'anthropic',
    label: 'Anthropic',
    models: ['claude-sonnet-4-6', 'claude-opus-4-7'],
    baseUrl: 'https://api.anthropic.com/v1',
  },
  {
    key: 'deepseek',
    label: 'DeepSeek',
    models: ['deepseek-chat', 'deepseek-coder', 'deepseek-r1'],
    baseUrl: 'https://api.deepseek.com/v1',
  },
] as const

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
  } catch {}
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
      className={cn('relative h-5 w-10 rounded-full transition-colors', value ? 'bg-quant-gold' : 'bg-quant-border', disabled && 'opacity-50')}
      role="switch"
      aria-checked={value}
    >
      <span className={cn('absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform', value ? 'left-5' : 'left-0.5')} />
    </button>
  )
}

function PasswordInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative">
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 pr-10 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
      />
      <button type="button" onClick={() => setVisible(!visible)} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground">
        {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  )
}

function TextInput({ value, onChange, placeholder, type = 'text' }: { value: string; onChange: (v: string) => void; placeholder?: string; type?: string }) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

function SelectField({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: string[] }) {
  return (
    <div className="relative">
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 pr-8 text-sm text-white outline-none transition-colors focus:border-quant-gold"
      >
        {options.map((opt) => (
          <option key={opt} value={opt}>
            {opt}
          </option>
        ))}
      </select>
      <ChevronRight className="pointer-events-none absolute right-2.5 top-1/2 h-4 w-4 -translate-y-1/2 rotate-90 text-muted-foreground" />
    </div>
  )
}

function NumberInput({ value, onChange, placeholder, min, max }: { value: number; onChange: (v: number) => void; placeholder?: string; min?: number; max?: number }) {
  return (
    <input
      type="number"
      min={min}
      max={max}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
      placeholder={placeholder}
      className="w-full rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white placeholder-muted-foreground outline-none transition-colors focus:border-quant-gold"
    />
  )
}

/* ------------------------------------------------------------------ */
/*  Main Component                                                     */
/* ------------------------------------------------------------------ */

export function Settings() {
  const queryClient = useQueryClient()
  const app = useAppStore()

  /* ── Backend config ── */
  const { data: backendConfig, isLoading: configLoading } = useQuery({
    queryKey: ['config'],
    queryFn: () => configApi.get(),
  })

  /* ── Backend-backed form state ── */
  const [defaultExchange, setDefaultExchange] = useState('binance')
  const [exchanges, setExchanges] = useState<Record<string, ExchangeConfig>>({})
const [testErrors, setTestErrors] = useState<Record<string, string>>({})
  const [defaultAIProvider, setDefaultAIProvider] = useState('openai')
  const [aiProviders, setAiProviders] = useState<Record<string, AIProviderConfig>>({})
  const [profitProtection, setProfitProtection] = useState(false)
  const [maxOrders, setMaxOrders] = useState(5)
  const [dirty, setDirty] = useState(false)

  /* ── Local settings (frontend-only) ── */
  const [notifyEmail, setNotifyEmail] = useState<NotifyConfig>(() => loadLocal('xt-notify-email', { enabled: true, address: '' }))
  const [notifyTelegram, setNotifyTelegram] = useState<NotifyConfig>(() => loadLocal('xt-notify-telegram', { enabled: false, botToken: '', chatId: '' }))
  const [notifyDingtalk, setNotifyDingtalk] = useState<NotifyConfig>(() => loadLocal('xt-notify-dingtalk', { enabled: false, webhook: '' }))
  const [dataSettings, setDataSettings] = useState<DataConfig>(() => loadLocal('xt-data', { klineLimit: 5000, autoCleanup: true, realtime: true }))
  const [securitySettings, setSecuritySettings] = useState<SecurityConfig>(() => loadLocal('xt-security', { twoFactor: false, sessionTimeout: 60, ipWhitelist: '' }))

  /* ── Sync backend config into form state ── */
  useEffect(() => {
    if (!backendConfig) return
    setDefaultExchange(backendConfig.default_exchange || 'binance')
    setExchanges((backendConfig.exchanges || {}) as Record<string, ExchangeConfig>)
    setDefaultAIProvider(backendConfig.default_ai_provider || 'openai')
    setAiProviders((backendConfig.ai || {}) as Record<string, AIProviderConfig>)
    const risk = backendConfig.risk || {}
    setProfitProtection(!!risk.profit_protection_enabled)
    setMaxOrders(typeof risk.max_concurrent_orders === 'number' ? risk.max_concurrent_orders : 5)
    setDirty(false)
  }, [backendConfig])

  /* ── Helpers to mutate nested state ── */
  const setExchangeField = useCallback((name: string, field: keyof ExchangeConfig, val: any) => {
    setExchanges((prev) => {
      const next = { ...prev, [name]: { ...(prev[name] || {}), [field]: val } }
      return next
    })
    setDirty(true)
  }, [])

  const setAIField = useCallback((provider: string, field: keyof AIProviderConfig, val: any) => {
    setAiProviders((prev) => {
      const next = { ...prev, [provider]: { ...(prev[provider] || {}), [field]: val } }
      return next
    })
    setDirty(true)
  }, [])

  /* ── Save mutation ── */
  const saveMut = useMutation({
    mutationFn: async () => {
      const payload = {
        ...(backendConfig || {}),
        default_exchange: defaultExchange,
        exchanges,
        default_ai_provider: defaultAIProvider,
        ai: aiProviders,
        risk: {
          ...(backendConfig?.risk || {}),
          profit_protection_enabled: profitProtection,
          max_concurrent_orders: maxOrders,
        },
      }
      return configApi.save(payload)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      setDirty(false)
    },
  })

  /* ── Test mutations ── */
  const testExchangeMut = useMutation({
    mutationFn: async ({ name, cfg }: { name: string; cfg: ExchangeConfig }) => {
      const result = await configApi.exchangeTest({ name, api_key: cfg.api_key || '', secret: cfg.secret || '', passphrase: cfg.passphrase || '' })
      if (result?.status === 'error') throw new Error(result?.detail || '连接失败')
      return result
    },
    onError: (err: Error, vars) => {
      // Store error per exchange for display
      setTestErrors(prev => ({ ...prev, [vars.name]: err.message }))
    },
    onSuccess: (_data, vars) => {
      setTestErrors(prev => ({ ...prev, [vars.name]: '' }))
    },
  })

  const testAIMut = useMutation({
    mutationFn: async ({ provider, cfg }: { provider: string; cfg: AIProviderConfig }) => {
      const prov = AI_PROVIDERS.find((p) => p.key === provider)
      return configApi.aiTest({ provider, api_key: cfg.api_key || '', base_url: cfg.base_url || prov?.baseUrl || '' })
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

  /* ── Active tab ── */
  const [activeTab, setActiveTab] = useState('exchange')

  const tabs = [
    { key: 'exchange', label: '交易所', icon: Globe },
    { key: 'ai', label: 'AI 模型', icon: BrainCircuit },
    { key: 'strategy', label: '全局策略', icon: SlidersHorizontal },
    { key: 'notify', label: '通知', icon: Bell, local: true },
    { key: 'appearance', label: '外观', icon: Palette, local: true },
    { key: 'data', label: '数据', icon: Database, local: true },
    { key: 'security', label: '安全', icon: Shield, local: true },
  ] as const

  const isSaving = saveMut.isPending

  /* ═══════════════════════════════════════════════════════════════
     Render helpers
     ═══════════════════════════════════════════════════════════════ */

  if (configLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-quant-gold" />
        <span className="ml-2 text-sm text-muted-foreground">加载配置中...</span>
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
                后端配置已修改，需要重启 Gateway 才能完全生效。
                <code className="ml-2 rounded bg-amber-500/20 px-1.5 py-0.5 text-xs font-mono text-amber-300">docker compose restart gateway</code>
              </div>
              <button onClick={handleCopyRestart} className="flex items-center gap-1.5 rounded-md bg-amber-500/20 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/30">
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                {copied ? '已复制' : '复制命令'}
              </button>
            </div>
          )}

          {/* Page header */}
          <PageHeader
            subtitle="管理交易所连接、AI 模型、通知通道与界面偏好"
            actions={
              <div className="flex items-center gap-2">
                <button
                  onClick={() => {
                    if (backendConfig) {
                      setDefaultExchange(backendConfig.default_exchange || 'binance')
                      setExchanges((backendConfig.exchanges || {}) as Record<string, ExchangeConfig>)
                      setDefaultAIProvider(backendConfig.default_ai_provider || 'openai')
                      setAiProviders((backendConfig.ai || {}) as Record<string, AIProviderConfig>)
                      const risk = backendConfig.risk || {}
                      setProfitProtection(!!risk.profit_protection_enabled)
                      setMaxOrders(typeof risk.max_concurrent_orders === 'number' ? risk.max_concurrent_orders : 5)
                    }
                    setDirty(false)
                  }}
                  className="flex items-center gap-1.5 rounded-md border border-quant-border bg-quant-card px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground"
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  重置
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
                  {isSaving ? '保存中...' : '保存'}
                </button>
              </div>
            }
          />
        </div>

        {/* Content area: left nav + right panel */}
        <div className="flex-1 flex gap-5 pl-4 pr-6 min-h-0">
          {/* Left nav — independently scrollable */}
          <div className="w-36 shrink-0 space-y-0.5 overflow-y-auto">
            {tabs.map((t) => (
              <button
                key={t.key}
                onClick={() => setActiveTab(t.key)}
                className={cn(
                  'flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors',
                  activeTab === t.key ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:bg-quant-card hover:text-foreground'
                )}
              >
                <t.icon className="h-4 w-4 shrink-0" />
                <span className="flex-1">{t.label}</span>
                {'local' in t && t.local && (
                  <span className="rounded bg-quant-border px-1.5 py-0.5 text-[10px] text-muted-foreground">本地</span>
                )}
              </button>
            ))}
          </div>

          {/* Right content — scrollable */}
          <div className="flex-1 space-y-4 overflow-y-auto min-h-0">
            {/* ── EXCHANGE ── */}
            {activeTab === 'exchange' && (
              <>
                <SectionCard title="默认交易所" bodyClassName="space-y-4">
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">默认交易所</label>
                      <SelectField
                        value={defaultExchange}
                        onChange={(v) => {
                          setDefaultExchange(v)
                          setDirty(true)
                        }}
                        options={EXCHANGES.map((e) => e.label)}
                      />
                    </div>
                  </div>
                </SectionCard>

                {EXCHANGES.map((ex) => {
                  const cfg = exchanges[ex.key] || {}
                  const testStatus = testExchangeMut.variables?.name === ex.key ? (testExchangeMut.isPending ? 'testing' : testExchangeMut.isSuccess ? 'ok' : testExchangeMut.isError ? 'error' : null) : null
                  return (
                    <SectionCard key={ex.key} title={ex.label} bodyClassName="space-y-4">
                      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">API Key</label>
                          <PasswordInput value={cfg.api_key || ''} onChange={(v) => setExchangeField(ex.key, 'api_key', v)} placeholder="输入 API Key" />
                        </div>
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">API Secret</label>
                          <PasswordInput value={cfg.secret || ''} onChange={(v) => setExchangeField(ex.key, 'secret', v)} placeholder="输入 API Secret" />
                        </div>
                        {ex.needsPassphrase && (
                          <div>
                            <label className="mb-1.5 block text-xs text-muted-foreground">Passphrase</label>
                            <PasswordInput value={cfg.passphrase || ''} onChange={(v) => setExchangeField(ex.key, 'passphrase', v)} placeholder="输入 Passphrase" />
                          </div>
                        )}
                        <div className="flex items-center gap-6 md:col-span-2">
                          <label className="flex items-center gap-2 text-xs text-muted-foreground">
                            <Toggle value={!!cfg.testnet} onChange={(v) => setExchangeField(ex.key, 'testnet', v)} />
                            使用测试网
                          </label>
                          <label className="flex items-center gap-2 text-xs text-muted-foreground">
                            <Toggle value={!!cfg.futures} onChange={(v) => setExchangeField(ex.key, 'futures', v)} />
                            启用合约
                          </label>
                          <label className="flex items-center gap-2 text-xs text-muted-foreground">
                            <Toggle value={!!cfg.enabled} onChange={(v) => setExchangeField(ex.key, 'enabled', v)} />
                            启用交易所
                          </label>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => testExchangeMut.mutate({ name: ex.key, cfg })}
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
                          {testStatus === 'testing' ? '测试中...' : testStatus === 'ok' ? '✅ 连接成功' : testStatus === 'error' ? (testErrors[ex.key] || '❌ 连接失败') : '测试连接'}
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
                <SectionCard title="默认 AI 提供商" bodyClassName="space-y-4">
                  <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">默认提供商</label>
                      <SelectField
                        value={defaultAIProvider}
                        onChange={(v) => {
                          setDefaultAIProvider(v)
                          setDirty(true)
                        }}
                        options={AI_PROVIDERS.map((p) => p.label)}
                      />
                    </div>
                  </div>
                </SectionCard>

                {AI_PROVIDERS.map((prov) => {
                  const cfg = aiProviders[prov.key] || {}
                  const testStatus = testAIMut.variables?.provider === prov.key ? (testAIMut.isPending ? 'testing' : testAIMut.isSuccess ? 'ok' : testAIMut.isError ? 'error' : null) : null
                  return (
                    <SectionCard key={prov.key} title={prov.label} bodyClassName="space-y-4">
                      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">API Key</label>
                          <PasswordInput value={cfg.api_key || ''} onChange={(v) => setAIField(prov.key, 'api_key', v)} placeholder={`输入 ${prov.label} API Key`} />
                        </div>
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">默认模型</label>
                          <SelectField
                            value={cfg.model || prov.models[0]}
                            onChange={(v) => setAIField(prov.key, 'model', v)}
                            options={[...prov.models]}
                          />
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => testAIMut.mutate({ provider: prov.key, cfg: { ...cfg, base_url: cfg.base_url || prov.baseUrl } })}
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
                          {testStatus === 'testing' ? '测试中...' : testStatus === 'ok' ? '✅ 连接成功' : testStatus === 'error' ? '❌ 连接失败' : '测试连接'}
                        </button>
                      </div>
                    </SectionCard>
                  )
                })}
              </>
            )}

            {/* ── STRATEGY GLOBAL ── */}
            {activeTab === 'strategy' && (
              <>
                {/* 基本风控 */}
                <SectionCard title="风控与全局参数" bodyClassName="space-y-5">
                  <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div>
                      <div className="text-sm font-medium text-foreground">盈利保护</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">盈利后实时从U本位合约账户划转至资金账户，需开启万向划转</div>
                    </div>
                    <Toggle value={profitProtection} onChange={(v) => { setProfitProtection(v); setDirty(true) }} />
                  </div>
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">最大并发订单数</label>
                    <NumberInput value={maxOrders} onChange={(v) => { setMaxOrders(v); setDirty(true) }} min={1} max={50} />
                  </div>
                </SectionCard>

                {/* CRA 首单与补仓参数 */}
                <SectionCard title="首单与补仓参数" bodyClassName="space-y-5">
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">做单数量（一般5-7单）</label>
                      <NumberInput value={loadLocal('cra-order-count', 7)} onChange={(v) => { saveLocal('cra-order-count', v); setDirty(true) }} min={1} max={20} />
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">首单仓位 (10-10000 USDT)</label>
                      <NumberInput value={loadLocal('cra-first-amount', 100)} onChange={(v) => { saveLocal('cra-first-amount', v); setDirty(true) }} min={10} max={10000} />
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">补仓价差 (0.5-50%)</label>
                      <NumberInput value={loadLocal('cra-spread', 3)} onChange={(v) => { saveLocal('cra-spread', v); setDirty(true) }} min={0.5} max={50} />
                      <p className="text-[10px] text-muted-foreground mt-1">每下跌达到设定百分比自动买入下一单</p>
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">补仓回调 (0.01-0.5%)</label>
                      <NumberInput value={loadLocal('cra-callback', 0.1)} onChange={(v) => { saveLocal('cra-callback', v); setDirty(true) }} min={0.01} max={0.5} />
                      <p className="text-[10px] text-muted-foreground mt-1">下跌到低点又上涨达到设定值才买入</p>
                    </div>
                  </div>
                  <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div>
                      <div className="text-sm font-medium text-foreground">开仓加倍</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">首单金额x2，补仓倍数仍按首单金额倍投或等比</div>
                    </div>
                    <Toggle value={loadLocal('cra-open-double', false)} onChange={(v) => { saveLocal('cra-open-double', v); setDirty(true) }} />
                  </div>
                  <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div>
                      <div className="text-sm font-medium text-foreground">关闭补仓</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">不执行补仓策略，但会正常止盈</div>
                    </div>
                    <Toggle value={loadLocal('cra-close-add', false)} onChange={(v) => { saveLocal('cra-close-add', v); setDirty(true) }} />
                  </div>
                </SectionCard>

                {/* CRA 止盈参数 */}
                <SectionCard title="止盈与止损参数" bodyClassName="space-y-5">
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">止盈比例 (%)</label>
                      <NumberInput value={loadLocal('cra-tp-ratio', 1.3)} onChange={(v) => { saveLocal('cra-tp-ratio', v); setDirty(true) }} min={0.1} max={50} />
                    </div>
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">盈利回调 (0.01-0.5%)</label>
                      <NumberInput value={loadLocal('cra-profit-cb', 0.1)} onChange={(v) => { saveLocal('cra-profit-cb', v); setDirty(true) }} min={0.01} max={0.5} />
                    </div>
                  </div>
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">止盈方式</label>
                    <div className="grid grid-cols-2 gap-3">
                      {[
                        { key: 'full', label: '全仓止盈', desc: '全仓盈利后卖出' },
                        { key: 'tail', label: '尾单止盈', desc: '最后一单盈利后卖出减仓' },
                        { key: 'head_tail', label: '首尾止盈', desc: '首单+尾单盈利后先行出仓' },
                        { key: 'moving', label: '移动止盈', desc: '动态分档止盈' },
                      ].map((m) => (
                        <button key={m.key} onClick={() => { saveLocal('cra-tp-method', m.key); setDirty(true) }}
                          className={cn('p-3 rounded-lg border text-left transition-colors',
                            loadLocal('cra-tp-method', 'full') === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                          <div className="text-xs font-medium">{m.label}</div>
                          <div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
                        </button>
                      ))}
                    </div>
                  </div>
                  {/* 移动止盈配置 */}
                  <div className="rounded-lg border border-quant-border bg-quant-bg p-4 space-y-3">
                    <div className="text-xs font-semibold">移动止盈档位配置</div>
                    <div className="grid grid-cols-3 gap-3">
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">第一档止盈比例 (%)</label>
                        <NumberInput value={loadLocal('cra-mv-tp1', 1.5)} onChange={(v) => { saveLocal('cra-mv-tp1', v); setDirty(true) }} min={0.1} max={10} />
                      </div>
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">第一档回撤 (%)</label>
                        <NumberInput value={loadLocal('cra-mv-dbk1', 30)} onChange={(v) => { saveLocal('cra-mv-dbk1', v); setDirty(true) }} min={5} max={100} />
                      </div>
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">第二档回撤 (%)</label>
                        <NumberInput value={loadLocal('cra-mv-dbk2', 20)} onChange={(v) => { saveLocal('cra-mv-dbk2', v); setDirty(true) }} min={5} max={100} />
                      </div>
                    </div>
                    <p className="text-[10px] text-muted-foreground">计算公式: 止盈比例 ± (止盈比例 × 回撤比例)。移动止盈开启后分仓/首尾止盈失效</p>
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">反向止盈</div>
                        <div className="mt-0.5 text-xs text-muted-foreground">MACD反向信号时清仓（适合大周期订单）</div>
                      </div>
                      <Toggle value={loadLocal('cra-reverse-tp', false)} onChange={(v) => { saveLocal('cra-reverse-tp', v); setDirty(true) }} />
                    </div>
                    <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">反向止损</div>
                        <div className="mt-0.5 text-xs text-muted-foreground">MACD判断错误直接止损</div>
                      </div>
                      <Toggle value={loadLocal('cra-reverse-sl', false)} onChange={(v) => { saveLocal('cra-reverse-sl', v); setDirty(true) }} />
                    </div>
                  </div>
                </SectionCard>

                {/* CRA 开仓与补仓指标 */}
                <SectionCard title="开仓与补仓指标" bodyClassName="space-y-5">
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">开仓指标策略</label>
                    <select value={loadLocal('cra-open-ind', 'macd_golden')}
                      onChange={(e) => { saveLocal('cra-open-ind', e.target.value); setDirty(true) }}
                      className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold">
                      <option value="macd_golden">MACD金叉开多</option>
                      <option value="macd_death">MACD死叉开空</option>
                      <option value="ema">EMA拐点开仓</option>
                      <option value="close">关闭（执行无脑买入）</option>
                    </select>
                  </div>
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">补仓指标（EMA和MACD补仓）</label>
                    <select value={loadLocal('cra-add-ind', 'macd')}
                      onChange={(e) => { saveLocal('cra-add-ind', e.target.value); setDirty(true) }}
                      className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold">
                      <option value="macd">MACD金叉/死叉补仓</option>
                      <option value="ema">EMA4上下拐点补仓</option>
                      <option value="close">关闭（仅按跌幅补仓）</option>
                    </select>
                    <p className="text-[10px] text-muted-foreground mt-1">开启后需同时满足跌幅条件和指标条件才补仓，大行情时非常抗跌</p>
                  </div>
                  <div className="space-y-3">
                    <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">趋势指标 (EMA4)</div>
                        <div className="mt-0.5 text-xs text-muted-foreground">监控EMA指数平滑移动平均线，可选5/15/30/60分钟</div>
                      </div>
                      <Toggle value={loadLocal('cra-trend-ind', false)} onChange={(v) => { saveLocal('cra-trend-ind', v); setDirty(true) }} />
                    </div>
                    {loadLocal('cra-trend-ind', false) && (
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">EMA4 时间周期</label>
                        <select value={loadLocal('cra-trend-tf', '15m')}
                          onChange={(e) => { saveLocal('cra-trend-tf', e.target.value); setDirty(true) }}
                          className="w-full appearance-none rounded-md border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none transition-colors focus:border-quant-gold">
                          <option value="5m">5分钟</option>
                          <option value="15m">15分钟</option>
                          <option value="30m">30分钟</option>
                          <option value="60m">60分钟</option>
                        </select>
                        <p className="text-[10px] text-muted-foreground mt-1">时间越长准确性越高，但也越容易错过行情</p>
                      </div>
                    )}
                  </div>
                </SectionCard>

                {/* CRA 防瀑布与振幅 */}
                <SectionCard title="防瀑布与振幅" bodyClassName="space-y-5">
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">防瀑布设定 (%)</label>
                    <NumberInput value={loadLocal('cra-waterfall', 2)} onChange={(v) => { saveLocal('cra-waterfall', v); setDirty(true) }} min={0.5} max={20} />
                    <p className="text-[10px] text-muted-foreground mt-1">1分钟内单一币种涨跌超过设定值自动暂停补仓，默认2%</p>
                  </div>
                  <div className="text-xs font-semibold">振幅建议设置</div>
                  <div className="grid grid-cols-4 gap-3">
                    {[
                      { key: 'cra-amp-5m', label: '5分钟', suggest: 2 },
                      { key: 'cra-amp-15m', label: '15分钟', suggest: 4 },
                      { key: 'cra-amp-30m', label: '30分钟', suggest: 7 },
                      { key: 'cra-amp-1h', label: '1小时', suggest: 10 },
                    ].map((a) => (
                      <div key={a.key}>
                        <label className="mb-1.5 block text-xs text-muted-foreground">{a.label} (建议{a.suggest}%)</label>
                        <NumberInput value={loadLocal(a.key, a.suggest)} onChange={(v) => { saveLocal(a.key, v); setDirty(true) }} min={0.1} max={50} />
                      </div>
                    ))}
                  </div>
                  <p className="text-[10px] text-muted-foreground">连续几根或一根K线连续上涨/下跌产生的价差幅度</p>
                </SectionCard>

                {/* CRA 斩仓燃烧与顺势而为 */}
                <SectionCard title="斩仓燃烧与顺势而为" bodyClassName="space-y-5">
                  <div className="space-y-3">
                    <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">斩仓和燃烧</div>
                        <div className="mt-0.5 text-xs text-muted-foreground">用顺势单盈利消耗逆势单浮亏，顺势单不占用在线单数</div>
                      </div>
                      <Toggle value={loadLocal('cra-burn', false)} onChange={(v) => { saveLocal('cra-burn', v); setDirty(true) }} />
                    </div>
                    {loadLocal('cra-burn', false) && (
                      <div className="grid grid-cols-2 gap-4">
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">双向燃烧起始仓（默认第3仓）</label>
                          <NumberInput value={loadLocal('cra-burn-dual', 3)} onChange={(v) => { saveLocal('cra-burn-dual', v); setDirty(true) }} min={1} max={10} />
                        </div>
                        <div>
                          <label className="mb-1.5 block text-xs text-muted-foreground">全局燃烧起始仓（默认第5仓）</label>
                          <NumberInput value={loadLocal('cra-burn-global', 5)} onChange={(v) => { saveLocal('cra-burn-global', v); setDirty(true) }} min={1} max={10} />
                        </div>
                      </div>
                    )}
                  </div>
                  <div className="space-y-3">
                    <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                      <div>
                        <div className="text-sm font-medium text-foreground">顺势而为</div>
                        <div className="mt-0.5 text-xs text-muted-foreground">逆势单补仓后顺势单倍投开仓，最高放大5倍</div>
                      </div>
                      <Toggle value={loadLocal('cra-follow', false)} onChange={(v) => { saveLocal('cra-follow', v); setDirty(true) }} />
                    </div>
                    {loadLocal('cra-follow', false) && (
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">顺势最大倍数（逆势补仓次数+首单，最高5倍）</label>
                        <NumberInput value={loadLocal('cra-follow-max', 5)} onChange={(v) => { saveLocal('cra-follow-max', v); setDirty(true) }} min={1} max={5} />
                        <p className="text-[10px] text-muted-foreground mt-1">顺势首单金额 = 逆势单补仓次数 + 首单倍率 × 首单金额</p>
                      </div>
                    )}
                  </div>
                  <div className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                    <div>
                      <div className="text-sm font-medium text-foreground">自定义减仓</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">极端行情下手动止损部分仓位，最后一仓占比50%，倒数第二25%，依次类推</div>
                    </div>
                    <Toggle value={loadLocal('cra-reduce', false)} onChange={(v) => { saveLocal('cra-reduce', v); setDirty(true) }} />
                  </div>
                </SectionCard>

                {/* CRA 交易次数与限制 */}
                <SectionCard title="交易次数与在线限制" bodyClassName="space-y-5">
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">交易次数模式</label>
                    <div className="flex gap-3">
                      {[
                        { key: 'single', label: '单次循环', desc: '止盈后不再买入，但补仓还会正常进行' },
                        { key: 'cycle', label: '策略循环', desc: '卖出后持续买入，直到循环次数用尽' },
                      ].map((m) => (
                        <button key={m.key} onClick={() => { saveLocal('cra-trade-count', m.key); setDirty(true) }}
                          className={cn('flex-1 p-3 rounded-lg border text-left transition-colors',
                            loadLocal('cra-trade-count', 'cycle') === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                          <div className="text-xs font-medium">{m.label}</div>
                          <div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
                        </button>
                      ))}
                    </div>
                  </div>
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">限制在线单量</label>
                    <NumberInput value={loadLocal('cra-online-limit', 10)} onChange={(v) => { saveLocal('cra-online-limit', v); setDirty(true) }} min={1} max={50} />
                    <p className="text-[10px] text-muted-foreground mt-1">控制趋势开仓后进场的交易对过多，包括多单和空单数量</p>
                  </div>
                  <div>
                    <label className="mb-1.5 block text-xs text-muted-foreground">首单挂单价格 (0=实时市价)</label>
                    <NumberInput value={loadLocal('cra-first-price', 0)} onChange={(v) => { saveLocal('cra-first-price', v); setDirty(true) }} min={0} max={1000000} />
                    <p className="text-[10px] text-muted-foreground mt-1">输入固定价格后，只有最新价格达到设定值系统才会市价买入</p>
                  </div>
                </SectionCard>

                {/* CRA 监控K线 */}
                <SectionCard title="监控K线配置" bodyClassName="space-y-5">
                  <div className="grid grid-cols-3 gap-3">
                    {[
                      { key: 'cra-kline-5m', label: '5分钟', desc: '短线' },
                      { key: 'cra-kline-15m', label: '15分钟', desc: '中短线' },
                      { key: 'cra-kline-30m', label: '30分钟', desc: '中线' },
                      { key: 'cra-kline-1h', label: '1小时', desc: '中长线' },
                      { key: 'cra-kline-4h', label: '4小时', desc: '长线' },
                      { key: 'cra-kline-8h', label: '8小时', desc: '超长线' },
                    ].map((k) => (
                      <button key={k.key} onClick={() => { saveLocal(k.key, !loadLocal(k.key, false)); setDirty(true) }}
                        className={cn('p-3 rounded-lg border text-center transition-colors',
                          loadLocal(k.key, false) ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                        <div className="text-xs font-medium">{k.label}</div>
                        <div className="text-[10px] text-muted-foreground">{k.desc}</div>
                      </button>
                    ))}
                  </div>
                  <p className="text-[10px] text-muted-foreground">监控币安交易所MACD和EMA指标，建议开启适合交易周期的K线</p>
                </SectionCard>
              </>
            )}

            {/* ── NOTIFY (local) ── */}
            {activeTab === 'notify' && (
              <>
                <SectionCard title="邮件通知" bodyClassName="space-y-4">
                  <label className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Toggle value={notifyEmail.enabled} onChange={(v) => setNotifyEmail((p) => ({ ...p, enabled: v }))} />
                    启用邮件通知
                  </label>
                  {notifyEmail.enabled && (
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">收件邮箱</label>
                      <TextInput value={notifyEmail.address || ''} onChange={(v) => setNotifyEmail((p) => ({ ...p, address: v }))} placeholder="admin@example.com" />
                    </div>
                  )}
                </SectionCard>
                <SectionCard title="Telegram 通知" bodyClassName="space-y-4">
                  <label className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Toggle value={notifyTelegram.enabled} onChange={(v) => setNotifyTelegram((p) => ({ ...p, enabled: v }))} />
                    启用 Telegram 通知
                  </label>
                  {notifyTelegram.enabled && (
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">Bot Token</label>
                        <PasswordInput value={notifyTelegram.botToken || ''} onChange={(v) => setNotifyTelegram((p) => ({ ...p, botToken: v }))} placeholder="输入 Bot Token" />
                      </div>
                      <div>
                        <label className="mb-1.5 block text-xs text-muted-foreground">Chat ID</label>
                        <TextInput value={notifyTelegram.chatId || ''} onChange={(v) => setNotifyTelegram((p) => ({ ...p, chatId: v }))} placeholder="输入 Chat ID" />
                      </div>
                    </div>
                  )}
                </SectionCard>
                <SectionCard title="钉钉通知" bodyClassName="space-y-4">
                  <label className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Toggle value={notifyDingtalk.enabled} onChange={(v) => setNotifyDingtalk((p) => ({ ...p, enabled: v }))} />
                    启用钉钉通知
                  </label>
                  {notifyDingtalk.enabled && (
                    <div>
                      <label className="mb-1.5 block text-xs text-muted-foreground">Webhook 地址</label>
                      <PasswordInput value={notifyDingtalk.webhook || ''} onChange={(v) => setNotifyDingtalk((p) => ({ ...p, webhook: v }))} placeholder="输入钉钉 Webhook" />
                    </div>
                  )}
                </SectionCard>

                <SectionCard title="TradingView 信号接入">
                  <div className="space-y-3">
                    <p className="text-xs text-muted-foreground">
                      将 TradingView 的 Pine Script 指标信号通过 Webhook 推送到本系统自动下单
                    </p>
                    <div>
                      <label className="mb-1 block text-[10px] text-muted-foreground">Webhook URL</label>
                      <div className="flex items-center gap-2">
                        <code className="flex-1 rounded-lg border border-quant-gold/30 bg-quant-bg px-3 py-2 text-xs text-quant-gold break-all">
                          {window.location.origin}/api/webhook/tv
                        </code>
                        <button
                          onClick={() => { navigator.clipboard.writeText(window.location.origin + '/api/webhook/tv') }}
                          className="px-3 py-2 rounded-lg border border-quant-border text-xs hover:bg-white/5 shrink-0"
                        >
                          复制
                        </button>
                      </div>
                    </div>
                    <div className="p-3 rounded-lg bg-quant-bg-secondary space-y-1.5">
                      <p className="text-[10px] font-medium text-foreground">TradingView 设置步骤</p>
                      <ol className="text-[10px] text-muted-foreground space-y-0.5 list-decimal list-inside">
                        <li>打开 TradingView 图表 → 创建闹钟 (Alert)</li>
                        <li>Webhook URL 填入上方地址</li>
                        <li>消息体填入 JSON 格式信号</li>
                      </ol>
                    </div>
                    <div className="p-3 rounded-lg bg-quant-bg-tertiary">
                      <p className="text-[10px] text-muted-foreground mb-1">消息体示例</p>
                      <pre className="text-[10px] font-mono text-foreground/80 whitespace-pre-wrap">{`{"symbol":"BTCUSDT","action":"buy","price":"50000","quantity":"0.1","strategy":"TV_MA_Cross"}`}</pre>
                    </div>
                    <div className="text-[10px] text-muted-foreground">
                      <span className="font-medium">action 取值:</span> buy(做多) / sell(做空) / exit(平仓)
                    </div>
                  </div>
                </SectionCard>
              </>
            )}

            {/* ── APPEARANCE (local) ── */}
            {activeTab === 'appearance' && (
              <SectionCard title="界面偏好" bodyClassName="space-y-5">
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">暗色主题</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">切换深色/浅色界面主题</div>
                  </div>
                  <Toggle value={app.theme === 'dark'} onChange={(v) => app.setTheme(v ? 'dark' : 'light')} />
                </label>
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">紧凑模式</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">减小间距以显示更多内容</div>
                  </div>
                  <Toggle value={app.layout === 'top'} onChange={(v) => app.setLayout(v ? 'top' : 'sidebar')} />
                </label>
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">固定顶部导航</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">滚动时保持顶部栏固定</div>
                  </div>
                  <Toggle value={app.fixedHeader} onChange={app.setFixedHeader} />
                </label>
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">侧边栏悬停展开</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">
                      {app.sidebarBehavior === 'hover'
                        ? '鼠标靠近自动展开，移开收起'
                        : '点击侧边栏手动展开/收起，有子菜单的项点击向下展开'}
                    </div>
                  </div>
                  <Toggle
                    value={app.sidebarBehavior === 'hover'}
                    onChange={(v) => app.setSidebarBehavior(v ? 'hover' : 'click')}
                  />
                </label>
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">界面语言</label>
                  <SelectField value={app.language} onChange={app.setLanguage} options={['zh-CN', 'en', 'ja']} />
                </div>
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">时区</label>
                  <SelectField value={dataSettings.timezone || 'Asia/Shanghai'} onChange={(v) => setDataSettings((p) => ({ ...p, timezone: v }))} options={TIMEZONES} />
                </div>
              </SectionCard>
            )}

            {/* ── DATA (local) ── */}
            {activeTab === 'data' && (
              <SectionCard title="数据管理" bodyClassName="space-y-5">
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">K 线保留条数</label>
                  <NumberInput value={dataSettings.klineLimit} onChange={(v) => setDataSettings((p) => ({ ...p, klineLimit: v }))} min={100} max={50000} />
                </div>
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">自动清理过期数据</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">定期清理超过保留条数的旧 K 线数据</div>
                  </div>
                  <Toggle value={dataSettings.autoCleanup} onChange={(v) => setDataSettings((p) => ({ ...p, autoCleanup: v }))} />
                </label>
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">实时推送</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">通过 WebSocket 接收实时行情更新</div>
                  </div>
                  <Toggle value={dataSettings.realtime} onChange={(v) => setDataSettings((p) => ({ ...p, realtime: v }))} />
                </label>
              </SectionCard>
            )}

            {/* ── SECURITY (local) ── */}
            {activeTab === 'security' && (
              <SectionCard title="安全设置" bodyClassName="space-y-5">
                <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-4">
                  <div>
                    <div className="text-sm font-medium text-foreground">两步验证 (2FA)</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">为账户登录增加额外的安全验证层</div>
                  </div>
                  <Toggle value={securitySettings.twoFactor} onChange={(v) => setSecuritySettings((p) => ({ ...p, twoFactor: v }))} />
                </label>
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">会话超时 (分钟)</label>
                  <NumberInput value={securitySettings.sessionTimeout} onChange={(v) => setSecuritySettings((p) => ({ ...p, sessionTimeout: v }))} min={5} max={1440} />
                </div>
                <div>
                  <label className="mb-1.5 block text-xs text-muted-foreground">IP 白名单</label>
                  <TextInput value={securitySettings.ipWhitelist} onChange={(v) => setSecuritySettings((p) => ({ ...p, ipWhitelist: v }))} placeholder="192.168.1.0/24, 10.0.0.1" />
                  <p className="mt-1 text-[11px] text-muted-foreground">逗号分隔，留空表示不限制</p>
                </div>
              </SectionCard>
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

  useEffect(() => {
    configApi.currencyGet().then((data: any) => {
      if (data?.currency) setCurrency(data.currency)
      if (data?.rates) setRates(data.rates)
    }).catch(() => {})
  }, [])

  const handleChange = async (cur: string) => {
    setCurrency(cur)
    setSaving(true)
    try {
      await configApi.currencySet(cur)
    } catch {}
    setSaving(false)
  }

  return (
    <SectionCard title="显示币种" bodyClassName="space-y-4">
      <p className="text-xs text-muted-foreground">
        选择资产估值和换算的显示币种。当前汇率从 open.er-api.com 获取，每小时更新。
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
          <Loader2 className="h-3 w-3 animate-spin" /> 保存中...
        </p>
      )}
    </SectionCard>
  )
}
