import { useState, useEffect } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { strategyApi, backtestApi, configApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import type { StrategyTemplate } from '@/types/strategies'
import { FormField, STRAT_TYPES, getDefaultStrategyCode, type StrategyRow } from './StrategyFormFields'
import { CRAParamForm, craParamsToApiPayload, type CRAParams } from './CRAParamForm'
import { STRATEGY_PRESETS, type Preset, type PresetKey } from './StrategyPresets'
import { ExchangeSelectModal } from './ExchangeSelectModal'
import { createDefaultCRAParams, migrateLegacyConfigToCRAParams, isCRAStrategyType, CRA_FEATURE_KEYS } from '@/lib/strategyUtils'
import { X, CheckCircle2, ChevronRight, ChevronDown, Activity, AlertTriangle, FileCode2, BarChart3, Globe } from 'lucide-react'

/* ─── helpers ───────────────────────────────────────────────────────── */
const CONTRACT_STRATEGY_TYPES = new Set([
  'cra_contract',
  'trend_long',
  'trend_short',
  'counter_stable',
  'counter_safe',
  'high_frequency',
  'head_tail_arbitrage',
])

function inferTimeframeFromCRAParams(cra: CRAParams): string {
  const candidates: (string | null)[] = [
    cra.openMacdEnabled && cra.openMacdPeriod !== 'close' ? cra.openMacdPeriod : null,
    cra.openCounterEmaEnabled && cra.openCounterEmaPeriod !== 'close' ? cra.openCounterEmaPeriod : null,
    cra.openTrendEmaEnabled && cra.openTrendEmaPeriod !== 'close' ? cra.openTrendEmaPeriod : null,
    cra.addMacdEnabled && cra.addMacdPeriod !== 'close' ? cra.addMacdPeriod : null,
    cra.addEmaEnabled && cra.addEmaPeriod !== 'close' ? cra.addEmaPeriod : null,
  ]
  const periods = candidates.filter((p): p is string => p !== null)
  return periods[0] || '15m'
}

/* ─── Collapsible Section ─── */
function CollapsibleSection({
  title,
  count,
  defaultOpen = false,
  children,
}: {
  title: string
  count?: number
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="rounded-xl border border-quant-border overflow-hidden">
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="w-full flex items-center justify-between px-4 py-3 bg-quant-bg-tertiary hover:bg-quant-hover transition-colors text-left"
      >
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-quant-gold">{title}</span>
          {count != null && <span className="text-[10px] text-muted-foreground">({count}项)</span>}
        </div>
        {open ? (
          <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" />
        ) : (
          <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" />
        )}
      </button>
      {open && <div className="p-4 space-y-4">{children}</div>}
    </div>
  )
}

/* ─── Modal ─── */
interface StrategyCreateModalProps {
  editing: StrategyRow | null
  defaultMarket?: 'spot' | 'contract'
  defaultStrategyType?: string
  /** P2-10：模板预填的默认 config（snake_case，含 CRA 参数）。 */
  defaultConfig?: Record<string, unknown>
  inline?: boolean
  onClose: () => void
  onSaved: () => void
}

export function StrategyCreateModal({
  editing,
  defaultMarket = 'contract',
  defaultStrategyType,
  defaultConfig,
  inline = false,
  onClose,
  onSaved,
}: StrategyCreateModalProps) {
  const [step, setStep] = useState(0)
  const [mode, setMode] = useState<'signal' | 'script'>(() =>
    editing?.mode === 'script' || editing?.strategy_mode === 'script' ? 'script' : 'signal'
  )
  const [market, setMarket] = useState<'contract' | 'spot'>(() => {
    if (editing) {
      return editing.market_type === 'spot' ? 'spot' : 'contract'
    }
    // 模板预填（P2-10）：按 strategy_type 推导市场，避免现货模板落进合约表单。
    if (defaultStrategyType && !CONTRACT_STRATEGY_TYPES.has(defaultStrategyType)) return 'spot'
    return defaultMarket
  })
  const [presetKey, setPresetKey] = useState<PresetKey | null>(null)
  const [name, setName] = useState(editing?.name || '')
  const [symbol, setSymbol] = useState(editing?.symbol || 'BTCUSDT')
  const [strategyType, setStrategyType] = useState(() => editing?.strategy_type || defaultStrategyType || '')

  useEffect(() => {
    setStrategyType((prev) => {
      const valid = STRAT_TYPES[market].some((t) => t.value === prev)
      return valid ? prev : ''
    })
  }, [market])

  useEffect(() => {
    if (!editing) {
      setCraParams(createDefaultCRAParams(market))
    }
  }, [market, editing])

  useEffect(() => {
    if (editing?.strategy_type) {
      setStrategyType(editing.strategy_type)
    } else if (defaultStrategyType) {
      setStrategyType(defaultStrategyType)
    }
    if (editing?.mode) {
      setMode(editing.mode === 'script' ? 'script' : 'signal')
    }
    if (editing?.execution_mode) {
      setExecutionMode(editing.execution_mode === 'live' ? 'live' : 'paper')
    }
    if (editing?.notification_config?.channels) {
      setNotifyChannels(editing.notification_config.channels)
    }
    if (typeof editing?.initial_capital === 'number') {
      setInitialCapital(editing.initial_capital)
    }
    if (editing?.config_json) {
      try {
        const parsed = JSON.parse(editing.config_json)
        setCraParams(migrateLegacyConfigToCRAParams(parsed, market))
        if (Array.isArray(parsed.selected_exchanges)) {
          setSelectedExchanges(parsed.selected_exchanges)
        }
      } catch {
        /* ignore */
      }
    } else if (defaultConfig) {
      // P2-10 模板预填：strategy_type + 保守默认 config。
      try {
        setCraParams(migrateLegacyConfigToCRAParams(defaultConfig, market))
        if (typeof defaultConfig.symbol === 'string' && defaultConfig.symbol) {
          setSymbol(defaultConfig.symbol.toUpperCase())
        }
        if (Array.isArray(defaultConfig.selected_exchanges)) {
          setSelectedExchanges(defaultConfig.selected_exchanges as string[])
        }
      } catch {
        /* ignore */
      }
    }
  }, [editing, defaultStrategyType, market, defaultConfig])

  const [executionMode, setExecutionMode] = useState<'paper' | 'live'>('paper')
  const [notifyChannels, setNotifyChannels] = useState<string[]>(['browser'])
  const [saveAsDefault, setSaveAsDefault] = useState(false)
  const [selectedExchanges, setSelectedExchanges] = useState<string[]>([])
  const [showExchangeModal, setShowExchangeModal] = useState(false)
  const [backtestCapital, setBacktestCapital] = useState(10000)
  const [initialCapital, setInitialCapital] = useState(1000)

  const {
    data: configuredExchanges,
    isError: isExchangesError,
    error: exchangesError,
  } = useQuery({
    queryKey: ['configured-exchanges'],
    queryFn: () => configApi.exchangesConfigured(),
    staleTime: 30000,
  })

  useEffect(() => {
    if (isExchangesError && exchangesError) {
      toast(
        'error',
        '交易所配置加载失败: ' + (exchangesError instanceof Error ? exchangesError.message : String(exchangesError))
      )
    }
  }, [isExchangesError, exchangesError])

  // CRA params (initialized from market-specific defaults)
  const [craParams, setCraParams] = useState<CRAParams>(() => createDefaultCRAParams(market))

  // ── My strategy templates ──
  const [templates, setTemplates] = useState<StrategyTemplate[]>([])
  const [templatesLoading, setTemplatesLoading] = useState(false)
  const [selectedTemplateId, setSelectedTemplateId] = useState('')

  useEffect(() => {
    if (editing) return
    setTemplatesLoading(true)
    strategyApi
      .templates(market === 'spot' ? 'spot' : 'contract')
      .then((res) => setTemplates(res || []))
      .catch(() => setTemplates([]))
      .finally(() => setTemplatesLoading(false))
  }, [market, editing])

  const applyTemplate = (template: StrategyTemplate) => {
    const config = template.default_config
    if (!config) return
    if (config.strategy_type && typeof config.strategy_type === 'string') {
      setStrategyType(config.strategy_type)
    }
    if (config.symbol && typeof config.symbol === 'string') {
      setSymbol(config.symbol.toUpperCase())
    }
    if (config.leverage && typeof config.leverage === 'number') {
      setCraParams((prev) => ({ ...prev, leverage: config.leverage as number }))
    }
    if (config.selected_exchanges && Array.isArray(config.selected_exchanges)) {
      setSelectedExchanges(config.selected_exchanges)
    }
    if (config.trade_direction) {
      const d = String(config.trade_direction)
      const direction = d === 'both' ? 'dual' : d === 'short' ? 'short' : 'long'
      setCraParams((prev) => ({ ...prev, direction }))
    }
    if (config.config_json && typeof config.config_json === 'string') {
      try {
        const parsed = JSON.parse(config.config_json)
        setCraParams(migrateLegacyConfigToCRAParams(parsed, market))
      } catch {
        /* ignore */
      }
    }
  }

  const applyPreset = (preset: Preset) => {
    setPresetKey(preset.key)
    setCraParams((prev) => ({ ...prev, ...preset.params(market) }))
  }

  // Script mode code
  const [codeWorkspace, setCodeWorkspace] = useState(editing?.strategy_code || '')
  useEffect(() => {
    if (editing?.strategy_code !== undefined) setCodeWorkspace(editing.strategy_code)
  }, [editing?.strategy_code])

  // Seed a type-specific starter template when entering script mode for a new strategy.
  useEffect(() => {
    if (editing) return
    if (mode !== 'script') return
    setCodeWorkspace((prev) => (prev.trim() === '' ? getDefaultStrategyCode(strategyType) : prev))
  }, [mode, strategyType, editing])

  // ── Quick backtest ──
  const [btResult, setBtResult] = useState<{
    winRate: number
    maxDrawdown: number
    profitFactor: number
    sharpe: number
    totalReturn: number
    trades: number
  } | null>(null)
  const [btLoading, setBtLoading] = useState(false)

  const handleRunBacktest = async () => {
    setBtLoading(true)
    setBtResult(null)
    try {
      const res = await backtestApi.run({
        symbol,
        interval: inferTimeframeFromCRAParams(craParams),
        strategy_type: strategyType,
        initial_balance: { USDT: backtestCapital },
        from: new Date(Date.now() - 30 * 86400000).toISOString().split('T')[0],
        to: new Date().toISOString().split('T')[0],
      })
      if (res) {
        setBtResult({
          winRate: res.win_rate ?? 0,
          maxDrawdown: res.max_drawdown_pct ?? 0,
          profitFactor: res.profit_factor ?? 0,
          sharpe: res.sharpe_ratio ?? 0,
          totalReturn: res.total_return_pct ?? 0,
          trades: res.total_trades ?? 0,
        })
      }
    } catch (e: unknown) {
      toast('error', '回测失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setBtLoading(false)
    }
  }

  // ── Create / Update ──
  const createMut = useMutation({
    mutationFn: (data: Record<string, unknown>) => strategyApi.create(data),
    onSuccess: (res) => {
      // P1-7：后端把 live/空 execution_mode 压回 paper 时提示用户。
      if (res?.forced_paper) {
        toast('warning', '已按安全默认设为模拟盘（实盘未在服务端开启）')
      }
      handleSaveAsDefault()
      onSaved()
    },
  })
  const updateMut = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) => strategyApi.update(id, data),
    onSuccess: (res) => {
      if (res?.forced_paper) {
        toast('warning', '已按安全默认设为模拟盘（实盘未在服务端开启）')
      }
      handleSaveAsDefault()
      onSaved()
    },
  })

  const handleSaveAsDefault = () => {
    if (!saveAsDefault) return
    strategyApi
      .createTemplate({
        name: name.trim(),
        category: market === 'spot' ? 'spot' : 'contract',
        description: `默认策略模板: ${name.trim()}`,
        default_config: {
          strategy_type: strategyType,
          symbol: symbol.trim().toUpperCase(),
          leverage: market === 'spot' ? 1 : craParams.leverage,
          trade_direction: market === 'spot' ? 'long' : craParams.direction,
          config_json: JSON.stringify({
            ...craParamsToApiPayload(craParams),
            selected_exchanges: selectedExchanges,
          }),
        },
      })
      .catch(() => {
        toast('error', '策略已保存，但默认模板保存失败')
      })
  }

  const handleSubmit = () => {
    if (!name.trim()) {
      toast('error', '请输入策略名称')
      return
    }
    if (!symbol.trim()) {
      toast('error', '请输入交易对')
      return
    }
    if (!strategyType) {
      toast('error', '请选择策略类型')
      return
    }
    if (market !== 'spot' && (!craParams.leverage || craParams.leverage < 1)) {
      toast('error', '合约策略杠杆必须≥1')
      return
    }
    if (selectedExchanges.length === 0) {
      toast('error', '请至少选择一个交易所')
      return
    }

    // ── P0-4 类型-参数防呆 ──
    const craType = isCRAStrategyType(strategyType)
    if (craType) {
      if (!craParams.firstOrderAmount || craParams.firstOrderAmount < 1) {
        toast('error', '参数与策略类型不匹配：CRA 策略需要首单金额 ≥ 1')
        return
      }
      if (craParams.enableAddPosition && craParams.addPositions.length === 0) {
        toast('error', '参数与策略类型不匹配：CRA 策略需要至少一档补仓（或关闭补仓）')
        return
      }
    }

    // 非 CRA 类型：config 只保留通用字段，CRA 专属键一律不写入，防止保存
    // "残废配置"（运行时会被参数注册表静默丢弃）。
    const config: Record<string, unknown> = {
      ...(craType ? craParamsToApiPayload(craParams) : {}),
      market_type: market === 'spot' ? 'spot' : 'swap',
      position_side: craParams.direction === 'long' ? 'LONG' : craParams.direction === 'short' ? 'SHORT' : 'BOTH',
      margin_mode: 'cross',
      selected_exchanges: selectedExchanges,
    }
    const configKeys = Object.keys(config)
    if (!craType && CRA_FEATURE_KEYS.some((k) => configKeys.includes(k))) {
      toast('error', '参数与策略类型不匹配：补仓/移动止盈参数仅适用于 cra_contract/cra_spot')
      return
    }
    const payload: Record<string, unknown> = {
      name: name.trim(),
      symbol: symbol.trim().toUpperCase(),
      leverage: market === 'spot' ? 1 : craParams.leverage,
      trade_direction: market === 'spot' ? 'long' : craParams.direction,
      market_type: market === 'spot' ? 'spot' : 'swap',
      execution_mode: executionMode, // paper 默认；live 需服务端 trading.live_enabled 开启
      notification_config: { channels: notifyChannels },
      strategy_type: strategyType,
      status: 'stopped',
      config_json: JSON.stringify(config),
      category: market === 'spot' ? 'spot' : 'contract',
      coin: symbol.trim().toUpperCase().replace('USDT', '').replace('USD', ''),
      direction: market === 'spot' ? 'long' : craParams.direction,
      initial_capital: initialCapital,
    }
    if (mode === 'script') {
      payload.strategy_code = codeWorkspace
      payload.mode = 'script'
    } else {
      payload.mode = 'signal'
    }
    if (editing) {
      updateMut.mutate({ id: editing.id, data: payload })
    } else {
      createMut.mutate(payload)
    }
  }

  const steps =
    mode === 'script'
      ? ['基础配置', '参数配置', '代码编辑', '执行设置']
      : ['基础配置', '参数配置', '回测预览', '执行设置']
  const inputCls =
    'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'
  const btnBase = 'flex-1 py-2 text-xs font-medium transition-colors'
  const btnActive = 'bg-quant-gold/10 text-quant-gold'
  const btnInactive = 'text-muted-foreground hover:text-foreground'

  return (
    <div
      role={inline ? undefined : 'dialog'}
      aria-modal={inline ? undefined : 'true'}
      className={cn(
        inline
          ? 'h-full flex flex-col'
          : 'fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4'
      )}
    >
      <div
        className={cn(
          'flex flex-col bg-quant-card overflow-hidden',
          inline
            ? 'h-full border-r border-quant-border'
            : 'w-full max-w-3xl max-h-[85vh] rounded-2xl border border-quant-border shadow-2xl'
        )}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <h3 className="text-sm font-bold">{editing ? '编辑策略' : '创建策略'}</h3>
          <button onClick={onClose} aria-label="关闭" className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Step indicator */}
        <div className="px-6 py-4 border-b border-quant-border shrink-0">
          <div className="flex items-center gap-2">
            {steps.map((s, i) => (
              <div key={s} className="flex items-center gap-2">
                <span
                  className={cn(
                    'w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-bold',
                    i === step
                      ? 'bg-quant-gold text-white'
                      : i < step
                        ? 'bg-quant-green text-white'
                        : 'bg-quant-bg-tertiary text-muted-foreground border border-quant-border'
                  )}
                >
                  {i < step ? <CheckCircle2 className="w-3.5 h-3.5" /> : i + 1}
                </span>
                <span className={cn('text-xs', i === step ? 'text-foreground font-medium' : 'text-muted-foreground')}>
                  {s}
                </span>
                {i < steps.length - 1 && <ChevronRight className="w-3 h-3 text-muted-foreground" />}
              </div>
            ))}
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {/* ═══ STEP 1: 基础配置 ═══ */}
          {step === 0 && (
            <>
              {/* Mode toggle */}
              {!editing && (
                <div className="flex rounded-lg border border-quant-border overflow-hidden">
                  <button
                    onClick={() => setMode('signal')}
                    className={cn(btnBase, mode === 'signal' ? btnActive : btnInactive)}
                  >
                    <Activity className="w-3.5 h-3.5 inline mr-1" />
                    指标信号
                  </button>
                  <button
                    onClick={() => setMode('script')}
                    className={cn(btnBase, 'border-l border-quant-border', mode === 'script' ? btnActive : btnInactive)}
                  >
                    <FileCode2 className="w-3.5 h-3.5 inline mr-1" />
                    脚本代码
                  </button>
                </div>
              )}

              {/* Preset templates（仅 CRA 类型；非 CRA 类型预设的补仓/止盈参数不会生效） */}
              {!editing && mode === 'signal' && (!strategyType || isCRAStrategyType(strategyType)) && (
                <div>
                  <div className="text-xs font-semibold text-muted-foreground mb-3">快速预设</div>
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                    {STRATEGY_PRESETS.map((pr) => (
                      <button
                        key={pr.key}
                        onClick={() => applyPreset(pr)}
                        type="button"
                        className={cn(
                          'relative p-4 rounded-xl border text-left transition-all',
                          presetKey === pr.key
                            ? pr.color + ' ring-1 ring-offset-1 ring-offset-quant-card'
                            : 'border-quant-border hover:border-quant-gold/30'
                        )}
                      >
                        {presetKey === pr.key && (
                          <CheckCircle2 className="absolute top-2 right-2 w-4 h-4 text-quant-gold" />
                        )}
                        <div className="text-xs font-bold mb-1">{pr.label}</div>
                        <div className="text-[10px] text-muted-foreground leading-relaxed">{pr.desc}</div>
                      </button>
                    ))}
                  </div>
                </div>
              )}

              <FormField label="策略名称">
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className={inputCls}
                  placeholder="输入策略名称"
                />
              </FormField>

              {!editing && (
                <FormField label="我的策略">
                  <select
                    value={selectedTemplateId}
                    onChange={(e) => {
                      const id = e.target.value
                      setSelectedTemplateId(id)
                      const template = templates.find((t) => t.id === id)
                      if (template) applyTemplate(template)
                    }}
                    className={inputCls}
                  >
                    <option value="">{templatesLoading ? '加载中...' : '请选择我的策略'}</option>
                    {templates.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name}
                      </option>
                    ))}
                  </select>
                </FormField>
              )}

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <FormField label="市场类型">
                  <div className="flex rounded-lg border border-quant-border overflow-hidden">
                    <button
                      onClick={() => setMarket('contract')}
                      className={cn(btnBase, market === 'contract' ? btnActive : btnInactive)}
                    >
                      合约
                    </button>
                    <button
                      onClick={() => setMarket('spot')}
                      className={cn(
                        btnBase,
                        'border-l border-quant-border',
                        market === 'spot' ? btnActive : btnInactive
                      )}
                    >
                      现货
                    </button>
                  </div>
                </FormField>
                <FormField label="交易对">
                  <input
                    value={symbol}
                    onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                    className={inputCls}
                  />
                </FormField>
              </div>

              <FormField label="初始资金 (USDT)">
                <input
                  type="number"
                  min={0}
                  value={initialCapital}
                  onChange={(e) => setInitialCapital(Number(e.target.value) || 0)}
                  className={inputCls}
                  placeholder="模拟盘初始权益"
                />
              </FormField>

              <FormField label="策略类型">
                <select value={strategyType} onChange={(e) => setStrategyType(e.target.value)} className={inputCls}>
                  <option value="">请选择策略类型</option>
                  {STRAT_TYPES[market].map((t) => (
                    <option key={t.value} value={t.value}>
                      {t.label}
                    </option>
                  ))}
                </select>
              </FormField>

              <FormField label="选择交易所">
                <button
                  type="button"
                  onClick={() => setShowExchangeModal(true)}
                  className={cn(
                    'w-full flex items-center justify-between border rounded-lg px-3 py-2 text-xs transition-colors',
                    selectedExchanges.length > 0
                      ? 'border-quant-gold/30 bg-quant-gold/5 text-foreground'
                      : 'border-quant-border text-muted-foreground hover:text-foreground'
                  )}
                >
                  <span className="flex items-center gap-2">
                    <Globe className="w-3.5 h-3.5" />
                    {selectedExchanges.length > 0 ? `已选择 ${selectedExchanges.length} 个交易所` : '点击选择交易所'}
                  </span>
                  <span className="text-[10px] text-muted-foreground">
                    {selectedExchanges.length > 0 ? selectedExchanges.join(', ') : '未选择'}
                  </span>
                </button>
              </FormField>

              <ExchangeSelectModal
                open={showExchangeModal}
                onClose={() => setShowExchangeModal(false)}
                value={selectedExchanges}
                onChange={setSelectedExchanges}
                configuredExchanges={configuredExchanges}
              />
            </>
          )}

          {/* ═══ STEP 2: CRA 参数配置（使用共享组件） ═══ */}
          {mode === 'signal' && step === 1 && (
            <div className="space-y-3">
              {isCRAStrategyType(strategyType) ? (
                <>
                  <div className="text-xs text-muted-foreground mb-2">调整策略参数，或使用预设快速填充</div>

                  <CollapsibleSection title="CRA 量化参数" count={4} defaultOpen>
                    <CRAParamForm value={craParams} onChange={setCraParams} market={market} />
                  </CollapsibleSection>
                </>
              ) : !strategyType ? (
                <div className="text-xs text-muted-foreground py-6 text-center">
                  请先在「基础配置」中选择策略类型
                </div>
              ) : (
                /* P0-4：非 CRA 类型不展示 CRA 专属参数区，避免类型-参数错配。 */
                <div className="text-xs text-muted-foreground py-6 text-center leading-relaxed">
                  当前策略类型不支持 CRA 补仓/移动止盈参数
                  <br />
                  仅需配置交易对、杠杆、方向等通用字段（后续步骤）
                </div>
              )}
            </div>
          )}

          {/* ═══ STEP 3/2: 回测预览 (signal 模式) ═══ */}
          {mode === 'signal' && step === 2 && (
            <>
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-6 text-center">
                <BarChart3 className="w-8 h-8 text-quant-gold mx-auto mb-3" />
                <div className="text-sm font-semibold mb-1">回测预览</div>
                <div className="text-xs text-muted-foreground mb-4">基于最近30天数据快速回测，验证策略效果</div>
                <FormField label="回测初始资金 (USDT)">
                  <input
                    type="number"
                    min={0}
                    value={backtestCapital}
                    onChange={(e) => setBacktestCapital(Number(e.target.value))}
                    className={cn(inputCls, 'max-w-[200px] mx-auto')}
                  />
                </FormField>
                <button
                  onClick={handleRunBacktest}
                  disabled={btLoading}
                  className={cn(
                    'mt-4 px-6 py-2.5 rounded-lg text-xs font-medium transition-all',
                    btLoading
                      ? 'bg-quant-gold/30 text-quant-gold cursor-wait'
                      : 'bg-quant-gold text-black hover:opacity-90'
                  )}
                >
                  {btLoading ? '回测中...' : '🚀 运行回测'}
                </button>
              </div>

              {btResult && (
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">总收益</div>
                    <div
                      className={cn(
                        'text-lg font-bold font-mono',
                        btResult.totalReturn >= 0 ? 'text-quant-green' : 'text-quant-red'
                      )}
                    >
                      {btResult.totalReturn >= 0 ? '+' : ''}
                      {btResult.totalReturn.toFixed(2)}%
                    </div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">胜率</div>
                    <div className="text-lg font-bold font-mono text-foreground">{btResult.winRate.toFixed(1)}%</div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">最大回撤</div>
                    <div
                      className={cn(
                        'text-lg font-bold font-mono',
                        btResult.maxDrawdown > 20 ? 'text-quant-red' : 'text-quant-green'
                      )}
                    >
                      {btResult.maxDrawdown.toFixed(2)}%
                    </div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">盈亏比</div>
                    <div className="text-lg font-bold font-mono text-foreground">
                      {btResult.profitFactor.toFixed(2)}
                    </div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">夏普比率</div>
                    <div className="text-lg font-bold font-mono text-foreground">{btResult.sharpe.toFixed(2)}</div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">交易次数</div>
                    <div className="text-lg font-bold font-mono text-foreground">{btResult.trades}</div>
                  </div>
                </div>
              )}

              {!btResult && !btLoading && (
                <div className="text-[11px] text-muted-foreground text-center py-4">
                  点击上方按钮运行回测，结果将在这里展示
                </div>
              )}
            </>
          )}

          {/* ═══ Script: Step 2 代码编辑 ═══ */}
          {mode === 'script' && step === 2 && (
            <FormField label="策略代码 (Python)">
              <textarea
                value={codeWorkspace}
                onChange={(e) => setCodeWorkspace(e.target.value)}
                className="w-full h-64 bg-quant-bg border border-quant-border rounded-lg p-3 font-mono text-[11px] leading-relaxed resize-none focus:outline-none focus:border-quant-gold"
                spellCheck={false}
              />
            </FormField>
          )}

          {/* ═══ 执行设置 (signal: step 3, script: step 3) ═══ */}
          {step === (mode === 'script' ? 3 : 3) && (
            <>
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4">
                <div className="text-xs font-semibold mb-3">执行模式</div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <button
                    onClick={() => setExecutionMode('paper')}
                    className={cn(
                      'flex items-start gap-3 p-4 rounded-xl border transition-all text-left',
                      executionMode === 'paper'
                        ? 'border-quant-gold bg-quant-gold/5'
                        : 'border-quant-border hover:border-quant-gold/30'
                    )}
                  >
                    <div
                      className={cn(
                        'w-10 h-10 rounded-lg flex items-center justify-center shrink-0',
                        executionMode === 'paper' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground'
                      )}
                    >
                      <Activity className="w-5 h-5" />
                    </div>
                    <div>
                      <div className="text-xs font-semibold">模拟盘（默认）</div>
                      <div className="text-[10px] text-muted-foreground mt-1">
                        撮合引擎模拟成交（paper），不产生真实交易所下单
                      </div>
                    </div>
                    {executionMode === 'paper' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
                  </button>
                  <button
                    onClick={() => {
                      if (executionMode !== 'live' && !confirm('实盘将使用真实资金下单，确认开启？')) return
                      setExecutionMode('live')
                    }}
                    className={cn(
                      'flex items-start gap-3 p-4 rounded-xl border transition-all text-left',
                      executionMode === 'live'
                        ? 'border-quant-red bg-quant-red/10'
                        : 'border-quant-red/30 hover:border-quant-red/60 hover:bg-quant-red/5'
                    )}
                  >
                    <div
                      className={cn(
                        'w-10 h-10 rounded-lg flex items-center justify-center shrink-0',
                        executionMode === 'live' ? 'bg-quant-red/15 text-quant-red' : 'bg-quant-bg text-quant-red/70'
                      )}
                    >
                      <AlertTriangle className="w-5 h-5" />
                    </div>
                    <div>
                      <div className="text-xs font-semibold text-quant-red">实盘（真实资金）</div>
                      <div className="text-[10px] text-quant-red/80 mt-1">
                        信号直连真实交易所下单；需服务端开启 trading.live_enabled，可能造成本金损失
                      </div>
                    </div>
                    {executionMode === 'live' && <CheckCircle2 className="w-4 h-4 text-quant-red ml-auto shrink-0" />}
                  </button>
                </div>
                {executionMode === 'live' && (
                  <div className="mt-3 px-3 py-2 rounded-lg bg-quant-red/10 border border-quant-red/30 text-[11px] text-quant-red leading-relaxed">
                    实盘模式将使用真实资金下单。保存时若服务端未开启实盘开关（trading.live_enabled），请求将被拒绝。
                  </div>
                )}
              </div>

              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4">
                <div className="text-xs font-semibold mb-3">通知渠道</div>
                <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
                  {[
                    { key: 'browser', label: '浏览器' },
                    { key: 'email', label: '邮件' },
                    { key: 'telegram', label: 'Telegram' },
                    { key: 'discord', label: 'Discord' },
                    { key: 'webhook', label: 'Webhook' },
                    { key: 'phone', label: '短信' },
                  ].map((ch) => (
                    <label
                      key={ch.key}
                      className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer hover:text-foreground transition-colors"
                    >
                      <input
                        type="checkbox"
                        checked={notifyChannels.includes(ch.key)}
                        onChange={(e) =>
                          setNotifyChannels((prev) =>
                            e.target.checked ? [...prev, ch.key] : prev.filter((c) => c !== ch.key)
                          )
                        }
                        className="rounded border-quant-border"
                      />
                      {ch.label}
                    </label>
                  ))}
                </div>
              </div>
            </>
          )}

          {/* Script: Step 1 params (CRA params for script too) */}
          {mode === 'script' && step === 1 && (
            <div className="text-xs text-muted-foreground py-8 text-center">
              脚本模式无需额外参数，点击下一步编辑代码
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-4 border-t border-quant-border shrink-0">
          <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer hover:text-foreground">
            <input
              type="checkbox"
              checked={saveAsDefault}
              onChange={(e) => setSaveAsDefault(e.target.checked)}
              className="rounded border-quant-border"
            />
            保存为默认策略模板
          </label>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
            >
              取消
            </button>
            {step > 0 && (
              <button
                onClick={() => setStep(step - 1)}
                className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
              >
                上一步
              </button>
            )}
            {step < steps.length - 1 ? (
              <button
                onClick={() => setStep(step + 1)}
                className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
              >
                下一步
              </button>
            ) : (
              <button
                onClick={handleSubmit}
                className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
              >
                {editing ? '保存修改' : '创建策略'}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
