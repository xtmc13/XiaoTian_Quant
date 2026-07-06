import { useState, useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { strategyApi, configApi } from '@/lib/api'
import { useStrategyData } from '@/hooks/useStrategyData'
import { SectionCard } from '@/components/ui/SectionCard'
import { CRAParamForm, craParamsToApiPayload, type CRAParams } from './CRAParamForm'
import { ExchangeSelectModal } from './ExchangeSelectModal'
import { FormField, DynamicParamField, STRAT_TYPES, TIMEFRAMES } from './StrategyFormFields'
import { STRATEGY_PRESETS, type Preset } from './StrategyPresets'
import { createDefaultCRAParams } from '@/lib/strategyUtils'
import type { StrategyParamDefs } from '@/types'
import { X, CheckCircle2, Activity, Zap, Globe } from 'lucide-react'

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

interface StrategyCreatePanelProps {
  strategyType: string
  onClose: () => void
  onSaved: () => void
}

function marketTypeFromType(strategyType: string): 'spot' | 'contract' {
  const contractTypes = [
    'trend_long',
    'trend_short',
    'counter_stable',
    'counter_safe',
    'high_frequency',
    'head_tail_arbitrage',
  ]
  return contractTypes.includes(strategyType) ? 'contract' : 'spot'
}

function strategyTypeLabel(strategyType: string, market: 'spot' | 'contract'): string {
  const option = STRAT_TYPES[market].find((t) => t.value === strategyType)
  return option?.label || strategyType
}

export function StrategyCreatePanel({ strategyType, onClose, onSaved }: StrategyCreatePanelProps) {
  const market = useMemo(() => marketTypeFromType(strategyType), [strategyType])
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
  const [selectedExchanges, setSelectedExchanges] = useState<string[]>([])
  const [showExchangeModal, setShowExchangeModal] = useState(false)
  const [executionMode, setExecutionMode] = useState<'live' | 'signal'>('signal')
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

  // Reset fields when strategyType changes
  useEffect(() => {
    setName('')
    setSymbol('BTCUSDT')
    setTimeframe('15m')
    setSelectedExchanges([])
    setExecutionMode('signal')
    setNotifyChannels(['browser'])
    setCraParams(createDefaultCRAParams(market))
    setPresetKey(null)
  }, [strategyType, market])

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

    setIsSubmitting(true)
    try {
      const config: Record<string, unknown> = {
        ...craParamsToApiPayload(craParams),
        ...dynamicParams,
        market_type: market === 'spot' ? 'spot' : 'swap',
        position_side: craParams.direction === 'long' ? 'LONG' : craParams.direction === 'short' ? 'SHORT' : 'BOTH',
        margin_mode: 'cross',
        selected_exchanges: selectedExchanges,
      }
      const payload: Record<string, unknown> = {
        name: name.trim(),
        symbol: symbol.trim().toUpperCase(),
        timeframe,
        leverage: market === 'spot' ? 1 : craParams.leverage,
        trade_direction: market === 'spot' ? 'long' : craParams.direction,
        market_type: market === 'spot' ? 'spot' : 'swap',
        execution_mode: executionMode,
        notification_config: { channels: notifyChannels },
        strategy_type: strategyType,
        status: 'stopped',
        config_json: JSON.stringify(config),
        category: market === 'spot' ? 'spot' : 'contract',
        coin: symbol.trim().toUpperCase().replace('USDT', '').replace('USD', ''),
        direction: market === 'spot' ? 'long' : craParams.direction,
        mode: 'signal',
      }
      await create(payload)
      handleSaveAsDefault()
      toast('success', `策略 "${name.trim()}" 已创建`)
      onSaved()
    } catch (e: unknown) {
      toast('error', '创建失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <div className="h-full flex flex-col bg-quant-card">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
        <div>
          <h3 className="text-sm font-bold">新建 {strategyTypeLabel(strategyType, market)} 策略</h3>
          <p className="text-[10px] text-muted-foreground mt-0.5">
            {market === 'spot' ? '现货策略' : '合约策略'} · 直接配置 CRA 参数
          </p>
        </div>
        <button
          onClick={onClose}
          aria-label="关闭"
          className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Body */}
      <div className="flex-1 overflow-y-auto p-6 space-y-5">
        {/* Presets */}
        <SectionCard title="快速预设">
          <div className="grid grid-cols-3 gap-3">
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
        </SectionCard>

        {/* Basic fields */}
        <SectionCard title="基础信息">
          <div className="space-y-4">
            <FormField label="策略名称">
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className={inputCls}
                placeholder="输入策略名称"
              />
            </FormField>

            <div className="grid grid-cols-2 gap-4">
              <FormField label="市场类型">
                <div className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs text-muted-foreground">
                  {market === 'spot' ? '现货' : '合约'}
                </div>
              </FormField>
              <FormField label="交易对">
                <input
                  value={symbol}
                  onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                  className={inputCls}
                  placeholder="BTCUSDT"
                />
              </FormField>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <FormField label="K线周期">
                <select value={timeframe} onChange={(e) => setTimeframe(e.target.value)} className={inputCls}>
                  {TIMEFRAMES.map((tf) => (
                    <option key={tf} value={tf}>
                      {tf}
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

        {/* CRA params */}
        <CRAParamForm value={craParams} onChange={setCraParams} market={market} />

        {/* Dynamic params */}
        {(paramDefsLoading || paramDefs.length > 0) && (
          <SectionCard title="动态策略参数">
            {paramDefsLoading && <div className="text-xs text-muted-foreground py-2">加载参数定义...</div>}
            {paramDefs.length > 0 && (
              <div className="grid grid-cols-2 gap-4">
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

        {/* Execution settings */}
        <SectionCard title="执行设置">
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <button
                onClick={() => setExecutionMode('live')}
                className={cn(
                  'flex items-start gap-3 p-4 rounded-xl border transition-all text-left',
                  executionMode === 'live'
                    ? 'border-quant-gold bg-quant-gold/5'
                    : 'border-quant-border hover:border-quant-gold/30'
                )}
              >
                <div
                  className={cn(
                    'w-10 h-10 rounded-lg flex items-center justify-center shrink-0',
                    executionMode === 'live' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground'
                  )}
                >
                  <Zap className="w-5 h-5" />
                </div>
                <div>
                  <div className="text-xs font-semibold">实盘交易</div>
                  <div className="text-[10px] text-muted-foreground mt-1">连接交易所API自动执行买卖</div>
                </div>
                {executionMode === 'live' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
              </button>
              <button
                onClick={() => setExecutionMode('signal')}
                className={cn(
                  'flex items-start gap-3 p-4 rounded-xl border transition-all text-left',
                  executionMode === 'signal'
                    ? 'border-quant-gold bg-quant-gold/5'
                    : 'border-quant-border hover:border-quant-gold/30'
                )}
              >
                <div
                  className={cn(
                    'w-10 h-10 rounded-lg flex items-center justify-center shrink-0',
                    executionMode === 'signal'
                      ? 'bg-quant-gold/10 text-quant-gold'
                      : 'bg-quant-bg text-muted-foreground'
                  )}
                >
                  <Activity className="w-5 h-5" />
                </div>
                <div>
                  <div className="text-xs font-semibold">信号通知</div>
                  <div className="text-[10px] text-muted-foreground mt-1">仅发送交易信号，不自动下单</div>
                </div>
                {executionMode === 'signal' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
              </button>
            </div>

            <div>
              <div className="text-xs font-semibold mb-3">通知渠道</div>
              <div className="grid grid-cols-3 gap-2">
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
          </div>
        </SectionCard>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between px-6 py-4 border-t border-quant-border shrink-0">
        <div className="flex items-center gap-4">
          <div className="text-[11px] text-muted-foreground">
            预估总投入: <span className="text-foreground font-mono">${totalAddPosition.toFixed(2)}</span>
          </div>
          <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer hover:text-foreground">
            <input
              type="checkbox"
              checked={saveAsDefault}
              onChange={(e) => setSaveAsDefault(e.target.checked)}
              className="rounded border-quant-border"
            />
            保存为默认策略模板
          </label>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={onClose}
            disabled={isSubmitting}
            className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors disabled:opacity-50"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={isSubmitting}
            className={cn(
              'px-4 py-2 rounded-lg text-xs font-medium transition-opacity',
              isSubmitting ? 'bg-quant-gold/50 text-white cursor-wait' : 'bg-quant-gold text-white hover:opacity-90'
            )}
          >
            {isSubmitting ? '保存中...' : '保存策略'}
          </button>
        </div>
      </div>
    </div>
  )
}
