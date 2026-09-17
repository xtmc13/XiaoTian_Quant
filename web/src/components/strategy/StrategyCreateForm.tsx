import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { strategyApi, configApi } from '@/lib/api'
import { useStrategyData } from '@/hooks/useStrategyData'
import { SectionCard } from '@/components/ui/SectionCard'
import { CRAParamForm, craParamsToApiPayload, type CRAParams } from './CRAParamForm'
import { ExchangeSelectModal } from './ExchangeSelectModal'
import { DynamicParamField, STRAT_TYPES } from './StrategyFormFields'
import { STRATEGY_PRESETS, type Preset } from './StrategyPresets'
import { createDefaultCRAParams, isCRAStrategyType, CRA_FEATURE_KEYS } from '@/lib/strategyUtils'
import type { StrategyParamDefs, ExchangeConfiguredStatus } from '@/types'
import { CheckCircle2, Activity, AlertTriangle, Globe } from 'lucide-react'

/**
 * 策略创建表单复用层：hook 承载全部状态/校验/提交逻辑，
 * StrategyCreateFormSections 渲染四个锚点区块（快速预设/基础信息/参数·指标与壳/
 * 执行设置）。StrategyCreatePanel（弹窗壳）与 CreateStrategyPage（/create 独立页）
 * 共用同一份实现，避免逻辑双份漂移。
 *
 * 策略身份 = 市场（spot/contract）+ 指标选择 + 壳参数：strategyType 不再由用户
 * 选择，hook 内自动派生（contract→cra_contract，spot→cra_spot）。initialType
 * 仅用于兼容入口（侧边栏类型快捷按钮/编辑回写）：传入非 cra 原值时保留不改。
 *
 * 锚点 id：create-sec-presets / create-sec-basic / create-sec-params / create-sec-exec
 */

export const CREATE_SECTION_IDS = ['create-sec-presets', 'create-sec-basic', 'create-sec-params', 'create-sec-exec'] as const

/** 旧类型 → 市场映射（兼容入口用：侧边栏类型快捷按钮等；创建表单本身不再选类型）。 */
export function marketTypeFromType(strategyType: string): 'spot' | 'contract' {
  const contractTypes = [
    'cra_contract',
    'trend_long',
    'trend_short',
    'counter_stable',
    'counter_safe',
    'high_frequency',
    'head_tail_arbitrage',
  ]
  return contractTypes.includes(strategyType) ? 'contract' : 'spot'
}

/** 策略类型自动派生：策略身份 = 市场 + 指标 + 壳参数（类型下拉已移除）。 */
export function deriveStrategyType(market: 'spot' | 'contract'): string {
  return market === 'contract' ? 'cra_contract' : 'cra_spot'
}

// Derive the strategy bar timeframe from enabled indicator periods.
const PERIOD_ORDER: Record<string, number> = {
  '1m': 1,
  '5m': 2,
  '15m': 3,
  '30m': 4,
  '1h': 5,
  '4h': 6,
  '1d': 7,
  close: 99,
}
function deriveTimeframeFromCRA(params: CRAParams): string {
  const candidates: string[] = []
  if (params.openMacdEnabled && params.openMacdPeriod !== 'close') candidates.push(params.openMacdPeriod)
  if (params.openCounterEmaEnabled && params.openCounterEmaPeriod !== 'close')
    candidates.push(params.openCounterEmaPeriod)
  if (params.openTrendEmaEnabled && params.openTrendEmaPeriod !== 'close') candidates.push(params.openTrendEmaPeriod)
  if (params.addMacdEnabled && params.addMacdPeriod !== 'close') candidates.push(params.addMacdPeriod)
  if (params.addEmaEnabled && params.addEmaPeriod !== 'close') candidates.push(params.addEmaPeriod)

  if (candidates.length === 0) return '15m'
  return candidates.sort((a, b) => PERIOD_ORDER[a] - PERIOD_ORDER[b])[0]
}

export function strategyTypeLabel(strategyType: string, market: 'spot' | 'contract'): string {
  const option = STRAT_TYPES[market].find((t) => t.value === strategyType)
  return option?.label || strategyType
}

// Strategy types rendered entirely by CRAParamForm; no dynamic params needed.
const CRA_ONLY_TYPES = new Set(['cra_contract', 'cra_spot', 'trend_long', 'trend_short'])

export interface StrategyCreateFormState {
  market: 'spot' | 'contract'
  /** 派生（或用户改选/initialType 覆盖）后的策略类型，提交 payload 用。 */
  strategyType: string
  /** 改选策略类型（现货：现货网格/马丁趋势/华尔街/激进）。 */
  setStrategyType: (t: string) => void
  name: string
  setName: Dispatch<SetStateAction<string>>
  symbol: string
  setSymbol: Dispatch<SetStateAction<string>>
  timeframe: string
  initialCapital: number
  setInitialCapital: Dispatch<SetStateAction<number>>
  selectedExchanges: string[]
  setSelectedExchanges: Dispatch<SetStateAction<string[]>>
  showExchangeModal: boolean
  setShowExchangeModal: Dispatch<SetStateAction<boolean>>
  configuredExchanges: Record<string, ExchangeConfiguredStatus> | undefined
  executionMode: 'paper' | 'live'
  setExecutionMode: Dispatch<SetStateAction<'paper' | 'live'>>
  notifyChannels: string[]
  setNotifyChannels: Dispatch<SetStateAction<string[]>>
  saveAsDefault: boolean
  setSaveAsDefault: Dispatch<SetStateAction<boolean>>
  craParams: CRAParams
  setCraParams: Dispatch<SetStateAction<CRAParams>>
  presetKey: string | null
  applyPreset: (preset: Preset) => void
  paramDefs: StrategyParamDefs['params']
  paramDefsLoading: boolean
  dynamicParams: Record<string, unknown>
  setDynamicParams: Dispatch<SetStateAction<Record<string, unknown>>>
  totalAddPosition: number
  isSubmitting: boolean
  handleSubmit: () => Promise<void>
}

export interface StrategyCreateFormOptions {
  /** 兼容覆盖：编辑回写或旧快捷入口传入非 cra 原类型时保留该值（创建场景不传）。 */
  initialType?: string
  /** 编辑模式：存在时保存走 update 而非 create。 */
  editId?: string
}

export function useStrategyCreateForm(
  market: 'spot' | 'contract',
  onSaved: () => void,
  options?: StrategyCreateFormOptions
): StrategyCreateFormState {
  const initialType = options?.initialType
  const editId = options?.editId
  // 类型默认按市场派生（contract→cra_contract，spot→cra_spot）；用户可用
  // setStrategyType 在表单里改选（如现货的 马丁趋势/华尔街/激进），编辑时
  // initialType 作为初始值且不强制覆盖用户改选。
  const derivedType = useMemo(() => deriveStrategyType(market), [market])
  const [typeOverride, setTypeOverride] = useState<string | null>(null)
  const strategyType = typeOverride ?? initialType ?? derivedType
  const setStrategyType = (t: string) => setTypeOverride(t)
  // 市场切换时清除改选，跟随新市场默认值
  useEffect(() => setTypeOverride(null), [market])
  const { create } = useStrategyData()
  const [isSubmitting, setIsSubmitting] = useState(false)

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

  // Basic fields
  const [name, setName] = useState('')
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [timeframe, setTimeframe] = useState('15m')
  const [initialCapital, setInitialCapital] = useState(1000)
  const [selectedExchanges, setSelectedExchanges] = useState<string[]>([])
  const [showExchangeModal, setShowExchangeModal] = useState(false)
  const [executionMode, setExecutionMode] = useState<'paper' | 'live'>('paper')
  const [notifyChannels, setNotifyChannels] = useState<string[]>(['browser'])
  const [saveAsDefault, setSaveAsDefault] = useState(false)

  // CRA params
  const [craParams, setCraParams] = useState<CRAParams>(() => createDefaultCRAParams(market))
  const [presetKey, setPresetKey] = useState<string | null>(null)

  // Dynamic params
  const [paramDefs, setParamDefs] = useState<StrategyParamDefs['params']>([])
  const [dynamicParams, setDynamicParams] = useState<Record<string, unknown>>({})
  const [paramDefsLoading, setParamDefsLoading] = useState(false)

  const totalAddPosition = useMemo(() => {
    const first = craParams.firstOrderAmount * craParams.firstOrderMultiplier
    return craParams.addPositions.reduce((sum, pos) => sum + first * pos.multiplier, first)
  }, [craParams.firstOrderAmount, craParams.firstOrderMultiplier, craParams.addPositions])

  useEffect(() => {
    if (!strategyType) return
    if (CRA_ONLY_TYPES.has(strategyType)) {
      setParamDefs([])
      setDynamicParams({})
      return
    }
    setParamDefsLoading(true)
    strategyApi
      .paramDefs(strategyType)
      .then((res: StrategyParamDefs) => {
        const defs = res?.params || []
        setParamDefs(defs)
        const defaults: Record<string, unknown> = {}
        defs.forEach((d) => {
          defaults[d.name] = d.default
        })
        setDynamicParams(defaults)
      })
      .catch(() => {
        setParamDefs([])
        setDynamicParams({})
      })
      .finally(() => setParamDefsLoading(false))
  }, [strategyType])

  // Reset fields when strategyType/market changes
  useEffect(() => {
    const defaults = createDefaultCRAParams(market)
    setName('')
    setSymbol('BTCUSDT')
    setTimeframe(deriveTimeframeFromCRA(defaults))
    setSelectedExchanges([])
    setExecutionMode('paper')
    setNotifyChannels(['browser'])
    setCraParams(defaults)
    setPresetKey(null)
  }, [strategyType, market])

  // Keep the main timeframe in sync with active indicator periods (no hardcoding).
  useEffect(() => {
    setTimeframe(deriveTimeframeFromCRA(craParams))
  }, [craParams])

  const applyPreset = (preset: Preset) => {
    setPresetKey(preset.key)
    setCraParams((prev) => ({ ...prev, ...preset.params(market) }))
  }

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
          timeframe,
          leverage: market === 'spot' ? 1 : craParams.leverage,
          trade_direction: market === 'spot' ? 'long' : craParams.direction,
          config_json: JSON.stringify({
            ...craParamsToApiPayload(craParams),
            ...dynamicParams,
            selected_exchanges: selectedExchanges,
          }),
        },
      })
      .catch(() => {
        toast('error', '策略已保存，但默认模板保存失败')
      })
  }

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast('error', '请输入策略名称')
      return
    }
    if (!symbol.trim()) {
      toast('error', '请输入交易对')
      return
    }
    if (market === 'contract' && (!craParams.leverage || craParams.leverage < 1)) {
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

    setIsSubmitting(true)
    try {
      const dynamicConfig = craType
        ? dynamicParams
        : Object.fromEntries(Object.entries(dynamicParams).filter(([k]) => !CRA_FEATURE_KEYS.includes(k)))
      const config: Record<string, unknown> = {
        ...(craType ? craParamsToApiPayload(craParams) : {}),
        ...dynamicConfig,
        market_type: market === 'spot' ? 'spot' : 'swap',
        position_side: craParams.direction === 'long' ? 'LONG' : craParams.direction === 'short' ? 'SHORT' : 'BOTH',
        margin_mode: 'cross',
        selected_exchanges: selectedExchanges,
      }
      if (!craType && CRA_FEATURE_KEYS.some((k) => k in config)) {
        toast('error', '参数与策略类型不匹配：补仓/移动止盈参数仅适用于 cra_contract/cra_spot')
        return
      }
      const payload: Record<string, unknown> = {
        name: name.trim(),
        symbol: symbol.trim().toUpperCase(),
        timeframe,
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
        mode: 'signal',
        initial_capital: initialCapital,
      }
      // 编辑模式走 update，创建走 create。
      const res = editId
        ? await strategyApi.update(editId, payload)
        : await create(payload)
      handleSaveAsDefault()
      // P1-7：后端把非 paper 值压回 paper 时提示用户。
      if (res?.forced_paper) {
        toast('warning', '已按安全默认设为模拟盘（实盘未在服务端开启）')
      }
      toast('success', editId ? `策略 "${name.trim()}" 已保存` : `策略 "${name.trim()}" 已创建`)
      onSaved()
    } catch (e: unknown) {
      toast('error', '创建失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setIsSubmitting(false)
    }
  }

  return {
    market,
    strategyType,
    setStrategyType,
    name,
    setName,
    symbol,
    setSymbol,
    timeframe,
    initialCapital,
    setInitialCapital,
    selectedExchanges,
    setSelectedExchanges,
    showExchangeModal,
    setShowExchangeModal,
    configuredExchanges,
    executionMode,
    setExecutionMode,
    notifyChannels,
    setNotifyChannels,
    saveAsDefault,
    setSaveAsDefault,
    craParams,
    setCraParams,
    presetKey,
    applyPreset,
    paramDefs,
    paramDefsLoading,
    dynamicParams,
    setDynamicParams,
    totalAddPosition,
    isSubmitting,
    handleSubmit,
  }
}

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

/** 四个锚点区块（快速预设/基础信息/参数·指标与壳/执行设置），弹窗与 /create 共用。 */
export function StrategyCreateFormSections({
  form,
  showPresets = true,
}: {
  form: StrategyCreateFormState
  showPresets?: boolean
}) {
  const {
    market,
    strategyType,
    setStrategyType,
    name,
    setName,
    symbol,
    setSymbol,
    timeframe,
    initialCapital,
    setInitialCapital,
    selectedExchanges,
    setSelectedExchanges,
    showExchangeModal,
    setShowExchangeModal,
    configuredExchanges,
    executionMode,
    setExecutionMode,
    notifyChannels,
    setNotifyChannels,
    craParams,
    setCraParams,
    presetKey,
    applyPreset,
    paramDefs,
    paramDefsLoading,
    dynamicParams,
    setDynamicParams,
  } = form

  return (
    <>
      {/* 1 快速预设 */}
      {showPresets && (
        <SectionCard title="快速预设">
          <div id="create-sec-presets" className="scroll-mt-20 -m-1 p-1">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              {STRATEGY_PRESETS.map((pr) => (
                <button
                  key={pr.key}
                  onClick={() => applyPreset(pr)}
                  type="button"
                  className={cn(
                    'relative p-3 rounded-xl border text-left transition-all',
                    presetKey === pr.key
                      ? pr.color + ' ring-1 ring-offset-1 ring-offset-quant-card'
                      : 'border-quant-border hover:border-quant-gold/30'
                  )}
                >
                  {presetKey === pr.key && (
                    <CheckCircle2 className="absolute top-2 right-2 w-3.5 h-3.5 text-quant-gold" />
                  )}
                  <div className="text-xs font-bold mb-1">{pr.label}</div>
                  <div className="text-[10px] text-muted-foreground leading-relaxed">{pr.desc}</div>
                </button>
              ))}
            </div>
          </div>
        </SectionCard>
      )}

      {/* 2 基础信息（两列紧凑盒式 grid，对齐效果图屏幕2；移动端单列） */}
      <SectionCard title="基础信息">
        <div id="create-sec-basic" className="scroll-mt-20 -m-1 p-1">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {/* 策略类型（现货可选：现货网格/马丁趋势/华尔街/激进；合约自动=合约网格） */}
            {market === 'spot' && (
              <div className="bg-quant-bg border border-quant-gold/50 rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0">
                <span className="text-muted-foreground shrink-0">策略类型</span>
                <select
                  value={strategyType}
                  onChange={(e) => setStrategyType(e.target.value)}
                  className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground bg-quant-bg"
                >
                  {STRAT_TYPES.spot.map((t) => (
                    <option key={t.value} value={t.value}>
                      {t.label}
                    </option>
                  ))}
                </select>
              </div>
            )}
            {/* 策略名称 */}
            <div className="bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0">
              <span className="text-muted-foreground shrink-0">策略名称</span>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none text-foreground placeholder:text-muted-foreground/60"
                placeholder="输入策略名称"
              />
            </div>
            {/* 交易对（主色蓝高亮格） */}
            <div className="bg-quant-bg border border-quant-gold/50 rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0">
              <span className="text-muted-foreground shrink-0">交易对</span>
              <input
                value={symbol}
                onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
                placeholder="BTCUSDT"
              />
            </div>
            {/* 交易所选择 */}
            <button
              type="button"
              onClick={() => setShowExchangeModal(true)}
              className={cn(
                'bg-quant-bg border rounded-lg px-3 py-2 text-xs flex items-center gap-2 text-left transition-colors min-w-0',
                selectedExchanges.length > 0
                  ? 'border-quant-gold/30 text-foreground'
                  : 'border-quant-border text-muted-foreground hover:text-foreground'
              )}
            >
              <Globe className="w-3.5 h-3.5 shrink-0" />
              <span className="shrink-0">交易所</span>
              <span className="flex-1 min-w-0 truncate">
                {selectedExchanges.length > 0 ? selectedExchanges.join(', ') : '点击选择'}
              </span>
              {selectedExchanges.length > 0 && <span className="text-quant-gold shrink-0">✓</span>}
            </button>
            {/* 初始资金 */}
            <div className="bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0">
              <span className="text-muted-foreground shrink-0">初始资金</span>
              <input
                type="number"
                min={0}
                value={initialCapital}
                onChange={(e) => setInitialCapital(Number(e.target.value) || 0)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
                placeholder="1000"
              />
              <span className="text-muted-foreground shrink-0">USDT</span>
            </div>
            {/* 杠杆 + 逐全仓（仅合约；主色蓝高亮格）。值与 CRAParamForm 同一状态。 */}
            {market === 'contract' && (
              <div className="bg-quant-bg border border-quant-gold/50 rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0">
                <span className="text-muted-foreground shrink-0">杠杆</span>
                <input
                  type="number"
                  min={1}
                  max={150}
                  value={craParams.leverage}
                  onChange={(e) =>
                    setCraParams((prev) => ({ ...prev, leverage: Number(e.target.value) || 1 }))
                  }
                  className="w-16 bg-transparent focus:outline-none font-bold text-quant-gold"
                />
                <span className="text-muted-foreground shrink-0">x · 全仓</span>
              </div>
            )}
            {/* K线周期（由启用指标周期自动推导，只读） */}
            <div className="bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0">
              <span className="text-muted-foreground shrink-0">K线周期</span>
              <span className="flex-1 min-w-0 font-bold text-foreground">{timeframe}</span>
            </div>
          </div>
        </div>
      </SectionCard>

      <ExchangeSelectModal
        open={showExchangeModal}
        onClose={() => setShowExchangeModal(false)}
        value={selectedExchanges}
        onChange={setSelectedExchanges}
        configuredExchanges={configuredExchanges}
      />

      {/* 3 参数 · 指标与壳 */}
      <div id="create-sec-params" className="scroll-mt-20">
        {isCRAStrategyType(strategyType) ? (
          <CRAParamForm value={craParams} onChange={setCraParams} market={market} />
        ) : (
          <SectionCard title="参数">
            <div className="text-xs text-muted-foreground py-4 text-center">
              当前策略类型不支持 CRA 补仓/移动止盈参数，仅使用通用字段与动态参数
            </div>
          </SectionCard>
        )}

        {!CRA_ONLY_TYPES.has(strategyType) && (paramDefsLoading || paramDefs.length > 0) && (
          <SectionCard title="动态策略参数">
            {paramDefsLoading && <div className="text-xs text-muted-foreground py-2">加载参数定义...</div>}
            {paramDefs.length > 0 && (
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                {paramDefs.map((def) => (
                  <DynamicParamField
                    key={def.name}
                    def={def}
                    value={dynamicParams[def.name]}
                    onChange={(val) => setDynamicParams((prev) => ({ ...prev, [def.name]: val }))}
                  />
                ))}
              </div>
            )}
          </SectionCard>
        )}
      </div>

      {/* 4 执行设置 */}
      <SectionCard title="执行设置">
        <div id="create-sec-exec" className="space-y-4 scroll-mt-20 -m-1 p-1">
          {/* 模拟盘默认；实盘需服务端 trading.live_enabled 开启，否则后端 400。 */}
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
            <div className="px-3 py-2 rounded-lg bg-quant-red/10 border border-quant-red/30 text-[11px] text-quant-red leading-relaxed">
              实盘模式将使用真实资金下单。保存时若服务端未开启实盘开关（trading.live_enabled），请求将被拒绝。
            </div>
          )}

          <div>
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
                      setNotifyChannels(
                        e.target.checked ? [...notifyChannels, ch.key] : notifyChannels.filter((c) => c !== ch.key)
                      )
                    }
                    className="rounded border-quant-border"
                  />
                  {ch.label}
                </label>
              ))}
            </div>
          </div>
        </div>
      </SectionCard>
    </>
  )
}
