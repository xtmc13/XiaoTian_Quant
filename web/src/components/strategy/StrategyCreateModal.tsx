import { useState, useEffect, useMemo } from 'react'
import { useMutation } from '@tanstack/react-query'
import { strategyApi, backtestApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import type { StrategyParamDefs } from '@/types'
import { FormField, Toggle, DynamicParamField, STRAT_TYPES, TIMEFRAMES, DEFAULT_CODE, type StrategyRow } from './StrategyFormFields'
import { CRAParamForm, modalToCra, craToModal, DEFAULT_CRA_PARAMS, type CRAParams } from './CRAParamForm'
import {
  X, CheckCircle2, ChevronRight, ChevronDown, Activity, FileCode2, SlidersHorizontal, Zap,
  BarChart3, TrendingUp, TrendingDown, Target, Percent
} from 'lucide-react'

/* ─── Types ─── */
type PresetKey = 'conservative' | 'balanced' | 'aggressive'

interface Preset {
  key: PresetKey
  label: string
  desc: string
  color: string
  params: Partial<ReturnType<typeof craToModal>>
}

const PRESETS: Preset[] = [
  { key: 'conservative', label: '保守型', desc: '低风险，小仓位分批入场，严格风控', color: 'text-emerald-400 border-emerald-500/30 bg-emerald-500/5',
    params: { orderCount: 5, firstOrderAmount: 50, addPosSpread: 5, addPosCallback: 0.3, takeProfitRatio: 1.5, profitCallback: 0.2, waterfallProtection: 1.5, onlineOrderLimit: 5, stopLossRatio: 5 } },
  { key: 'balanced', label: '平衡型', desc: '适中风险，标准参数配置', color: 'text-amber-400 border-amber-500/30 bg-amber-500/5',
    params: { orderCount: 7, firstOrderAmount: 100, addPosSpread: 3, addPosCallback: 0.1, takeProfitRatio: 1.3, profitCallback: 0.1, waterfallProtection: 2, onlineOrderLimit: 10 } },
  { key: 'aggressive', label: '激进型', desc: '高收益高回撤，适合趋势行情', color: 'text-red-400 border-red-500/30 bg-red-500/5',
    params: { orderCount: 10, firstOrderAmount: 200, addPosSpread: 1.5, addPosCallback: 0.05, takeProfitRatio: 2.0, profitCallback: 0.05, waterfallProtection: 4, onlineOrderLimit: 20, openDouble: true, followTrend: true, followTrendMax: 5 } },
]

/* ─── Collapsible Section ─── */
function CollapsibleSection({ title, count, defaultOpen = false, children }: { title: string; count?: number; defaultOpen?: boolean; children: React.ReactNode }) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="rounded-xl border border-quant-border overflow-hidden">
      <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-4 py-3 bg-quant-bg-tertiary hover:bg-quant-hover transition-colors text-left">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-quant-gold">{title}</span>
          {count != null && <span className="text-[10px] text-muted-foreground">({count}项)</span>}
        </div>
        {open ? <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" /> : <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" />}
      </button>
      {open && <div className="p-4 space-y-4">{children}</div>}
    </div>
  )
}

/* ─── Modal ─── */
interface StrategyCreateModalProps {
  editing: StrategyRow | null
  onClose: () => void
  onSaved: () => void
}

export function StrategyCreateModal({ editing, onClose, onSaved }: StrategyCreateModalProps) {
  const [step, setStep] = useState(0)
  const [mode, setMode] = useState<'signal' | 'script'>('signal')
  const [market, setMarket] = useState<'contract' | 'spot'>('contract')
  const [presetKey, setPresetKey] = useState<PresetKey | null>(null)
  const [name, setName] = useState(editing?.name || '')
  const [symbol, setSymbol] = useState(editing?.symbol || 'BTCUSDT')
  const [strategyType, setStrategyType] = useState('breakout')
  const [dynamicParams, setDynamicParams] = useState<Record<string, unknown>>({})
  const [paramDefs, setParamDefs] = useState<StrategyParamDefs['params']>([])
  const [paramDefsLoading, setParamDefsLoading] = useState(false)

  useEffect(() => {
    if (!strategyType) return
    setParamDefsLoading(true)
    strategyApi.paramDefs(strategyType).then((res: StrategyParamDefs) => {
      const defs = res?.params || []
      setParamDefs(defs)
      const defaults: Record<string, unknown> = {}
      defs.forEach((d) => { defaults[d.name] = d.default })
      setDynamicParams(defaults)
    }).catch(() => { setParamDefs([]); setDynamicParams({}) }).finally(() => setParamDefsLoading(false))
  }, [strategyType])

  useEffect(() => {
    if (editing?.strategy_type) setStrategyType(editing.strategy_type)
    if (editing?.config_json) {
      try {
        const parsed = JSON.parse(editing.config_json)
        setDynamicParams((prev) => ({ ...prev, ...parsed }))
        // Sync CRA params from config_json into p state
        setP({
          orderCount: parsed.order_count ?? parsed.orderCount ?? 7,
          firstOrderAmount: parsed.first_order_amount ?? parsed.firstOrderAmount ?? 100,
          addPosSpread: parsed.add_position_spread ?? parsed.addPosSpread ?? 3,
          addPosCallback: parsed.add_position_callback ?? parsed.addPosCallback ?? 0.1,
          takeProfitRatio: parsed.take_profit_ratio ?? parsed.takeProfitRatio ?? 1.3,
          profitCallback: parsed.profit_callback ?? parsed.profitCallback ?? 0.1,
          tradeCountMode: parsed.trade_count_mode ?? parsed.tradeCountMode ?? 'cycle',
          openIndicator: parsed.open_indicator ?? parsed.openIndicator ?? 'macd_golden',
          addPosIndicator: parsed.add_position_indicator ?? parsed.addPosIndicator ?? 'macd',
          addPosMultiple: parsed.add_position_multiple ?? parsed.addPosMultiple ?? 1,
          waterfallProtection: parsed.waterfall_protection ?? parsed.waterfall ?? 2,
          openDouble: !!parsed.open_double,
          trendIndicator: !!parsed.trend_indicator,
          trendTimeframe: parsed.trend_timeframe ?? parsed.trendTf ?? '15m',
          takeProfitMethod: (parsed.take_profit_method ?? parsed.tpMethod ?? 'full') as 'full' | 'tail' | 'head_tail' | 'moving',
          movingTP: parsed.moving_take_profit ?? { enabled: false, tier1_ratio: 1.5, tier1_drawback: 30, tier2_drawback: 20 },
          reverseTP: !!parsed.reverse_take_profit,
          reverseSL: !!parsed.reverse_stop_loss,
          amplitude: parsed.amplitude ?? { '5m': 2, '15m': 4, '30m': 7, '1h': 10 },
          burnCut: typeof parsed.burn_cut === 'object' ? parsed.burn_cut as { enabled: boolean; dual_burn_start: number; global_burn_start: number } : { enabled: !!parsed.burn_cut, dual_burn_start: 3, global_burn_start: 5 },
          customReduce: !!parsed.custom_reduce,
          onlineOrderLimit: parsed.online_order_limit ?? 10,
          profitProtection: !!parsed.profit_protection,
          followTrend: !!parsed.follow_trend,
          followTrendMax: parsed.follow_trend_max ?? 5,
          stopLossRatio: parsed.stop_loss_ratio ?? 0,
          stopLossAmount: parsed.stop_loss_amount ?? 0,
          stopLossPrice: parsed.stop_loss_price ?? 0,
          firstOrderPrice: parsed.first_order_price ?? 0,
          closeAddPosition: !!parsed.close_add_position,
        })
      } catch { /* ignore */ }
    }
  }, [editing])

  const [timeframe, setTimeframe] = useState(editing?.timeframe || '15m'), [leverage, setLeverage] = useState(editing?.leverage || 5)
  const [direction, setDirection] = useState<'long' | 'short' | 'dual'>('long'), [initialCapital, setInitialCapital] = useState(editing?.initial_capital || 1000)
  const [executionMode, setExecutionMode] = useState<'live' | 'signal'>('signal'), [notifyChannels, setNotifyChannels] = useState<string[]>(['browser'])

  // CRA params (initialized from CRAParamForm defaults, converted to modal format)
  const [p, setP] = useState<ReturnType<typeof craToModal>>(() => craToModal(DEFAULT_CRA_PARAMS))

  const applyPreset = (preset: Preset) => {
    setPresetKey(preset.key)
    setP(prev => ({ ...prev, ...preset.params }))
  }

  // Script mode code
  const [codeWorkspace, setCodeWorkspace] = useState(editing?.strategy_code || '')
  useEffect(() => {
    if (editing?.strategy_code !== undefined) setCodeWorkspace(editing.strategy_code)
  }, [editing?.strategy_code])

  // ── Quick backtest ──
  const [btResult, setBtResult] = useState<{ winRate: number; maxDrawdown: number; profitFactor: number; sharpe: number; totalReturn: number; trades: number } | null>(null)
  const [btLoading, setBtLoading] = useState(false)

  const handleRunBacktest = async () => {
    setBtLoading(true)
    setBtResult(null)
    try {
      const res = await backtestApi.run({
        symbol, interval: timeframe, strategy_type: strategyType,
        initial_balance: { USDT: initialCapital },
        from: new Date(Date.now() - 30 * 86400000).toISOString().split('T')[0],
        to: new Date().toISOString().split('T')[0],
      })
      if (res) {
        setBtResult({
          winRate: res.win_rate ?? 0, maxDrawdown: res.max_drawdown_pct ?? 0,
          profitFactor: res.profit_factor ?? 0, sharpe: res.sharpe_ratio ?? 0,
          totalReturn: res.total_return_pct ?? 0, trades: res.total_trades ?? 0,
        })
      }
    } catch (e: unknown) {
      alert('回测失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally { setBtLoading(false) }
  }

  // ── Risk preview ──
  const riskPreview = useMemo(() => {
    const marginPerOrder = initialCapital / (market === 'spot' ? 1 : Math.max(leverage, 1))
    const totalExposure = marginPerOrder * p.orderCount * (p.openDouble ? 2 : 1)
    const maxLoss = p.stopLossRatio > 0 ? initialCapital * p.stopLossRatio / 100 : p.stopLossAmount > 0 ? p.stopLossAmount : 0
    return { marginPerOrder, totalExposure, maxLoss }
  }, [initialCapital, leverage, market, p.orderCount, p.openDouble, p.stopLossRatio, p.stopLossAmount])

  // ── Create / Update ──
  const createMut = useMutation({ mutationFn: (data: Record<string, unknown>) => strategyApi.create(data), onSuccess: onSaved })
  const updateMut = useMutation({ mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) => strategyApi.update(id, data), onSuccess: onSaved })

  const handleSubmit = () => {
    if (!name.trim()) { alert('请输入策略名称'); return }
    if (!symbol.trim()) { alert('请输入交易对'); return }
    if (!strategyType) { alert('请选择策略类型'); return }
    if (market !== 'spot' && (!leverage || leverage < 1)) { alert('合约策略杠杆必须≥1'); return }

    const config: Record<string, unknown> = {
      order_count: p.orderCount, first_order_amount: p.firstOrderAmount, add_position_spread: p.addPosSpread, add_position_callback: p.addPosCallback,
      take_profit_ratio: p.takeProfitRatio, profit_callback: p.profitCallback, trade_count_mode: p.tradeCountMode, open_indicator: p.openIndicator,
      add_position_indicator: p.addPosIndicator, add_position_multiple: p.addPosMultiple, waterfall_protection: p.waterfallProtection, open_double: p.openDouble,
      trend_indicator: p.trendIndicator, trend_timeframe: p.trendTimeframe, take_profit_method: p.takeProfitMethod, moving_take_profit: p.movingTP,
      reverse_take_profit: p.reverseTP, reverse_stop_loss: p.reverseSL, amplitude: p.amplitude, burn_cut: p.burnCut, custom_reduce: p.customReduce,
      online_order_limit: p.onlineOrderLimit, profit_protection: p.profitProtection, follow_trend: p.followTrend, follow_trend_max: p.followTrendMax,
      stop_loss_ratio: p.stopLossRatio, stop_loss_amount: p.stopLossAmount, stop_loss_price: p.stopLossPrice, first_order_price: p.firstOrderPrice,
      close_add_position: p.closeAddPosition, ...dynamicParams,
      market_type: market === 'spot' ? 'spot' : 'swap',
      position_side: direction === 'long' ? 'LONG' : direction === 'short' ? 'SHORT' : 'BOTH',
      margin_mode: 'cross', leverage: market === 'spot' ? 1 : leverage,
    }
    const payload: Record<string, unknown> = {
      name: name.trim(), symbol: symbol.trim().toUpperCase(), timeframe, leverage: market === 'spot' ? 1 : leverage,
      trade_direction: market === 'spot' ? 'long' : direction, market_type: market === 'spot' ? 'spot' : 'swap',
      initial_capital: initialCapital, execution_mode: executionMode, notification_config: { channels: notifyChannels },
      strategy_type: strategyType, status: 'stopped', config_json: JSON.stringify(config),
      category: market === 'spot' ? 'spot' : 'contract',
      coin: symbol.trim().toUpperCase().replace('USDT', '').replace('USD', ''),
      direction: market === 'spot' ? 'long' : direction,
    }
    if (mode === 'script') { payload.strategy_code = codeWorkspace; payload.mode = 'script' }
    else { payload.mode = 'signal' }
    if (editing) { updateMut.mutate({ id: editing.id, data: payload }) } else { createMut.mutate(payload) }
  }

  const steps = mode === 'script' ? ['基础配置', '参数配置', '代码编辑', '执行设置'] : ['基础配置', '参数配置', '回测预览', '执行设置']
  const inputCls = "w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
  const btnBase = "flex-1 py-2 text-xs font-medium transition-colors"
  const btnActive = "bg-quant-gold/10 text-quant-gold"
  const btnInactive = "text-muted-foreground hover:text-foreground"

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-full max-w-3xl max-h-[90vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <h3 className="text-sm font-bold">{editing ? '编辑策略' : '创建策略'}</h3>
          <button onClick={onClose} aria-label="关闭" className="text-muted-foreground hover:text-foreground"><X className="w-4 h-4" /></button>
        </div>

        {/* Step indicator */}
        <div className="px-6 py-4 border-b border-quant-border shrink-0">
          <div className="flex items-center gap-2">
            {steps.map((s, i) => (
              <div key={s} className="flex items-center gap-2">
                <span className={cn('w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-bold', i === step ? 'bg-quant-gold text-white' : i < step ? 'bg-quant-green text-white' : 'bg-quant-bg-tertiary text-muted-foreground border border-quant-border')}>
                  {i < step ? <CheckCircle2 className="w-3.5 h-3.5" /> : i + 1}
                </span>
                <span className={cn('text-xs', i === step ? 'text-foreground font-medium' : 'text-muted-foreground')}>{s}</span>
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
                  <button onClick={() => setMode('signal')} className={cn(btnBase, mode === 'signal' ? btnActive : btnInactive)}><Activity className="w-3.5 h-3.5 inline mr-1" />指标信号</button>
                  <button onClick={() => setMode('script')} className={cn(btnBase, 'border-l border-quant-border', mode === 'script' ? btnActive : btnInactive)}><FileCode2 className="w-3.5 h-3.5 inline mr-1" />脚本代码</button>
                </div>
              )}

              {/* Preset templates */}
              {!editing && mode === 'signal' && (
                <div>
                  <div className="text-xs font-semibold text-muted-foreground mb-3">快速预设</div>
                  <div className="grid grid-cols-3 gap-3">
                    {PRESETS.map((pr) => (
                      <button key={pr.key} onClick={() => applyPreset(pr)} type="button"
                        className={cn('relative p-4 rounded-xl border text-left transition-all', presetKey === pr.key ? pr.color + ' ring-1 ring-offset-1 ring-offset-quant-card' : 'border-quant-border hover:border-quant-gold/30')}>
                        {presetKey === pr.key && <CheckCircle2 className="absolute top-2 right-2 w-4 h-4 text-quant-gold" />}
                        <div className="text-xs font-bold mb-1">{pr.label}</div>
                        <div className="text-[10px] text-muted-foreground leading-relaxed">{pr.desc}</div>
                      </button>
                    ))}
                  </div>
                </div>
              )}

              <FormField label="策略名称">
                <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} placeholder="输入策略名称" />
              </FormField>

              <div className="grid grid-cols-2 gap-4">
                <FormField label="市场类型">
                  <div className="flex rounded-lg border border-quant-border overflow-hidden">
                    <button onClick={() => setMarket('contract')} className={cn(btnBase, market === 'contract' ? btnActive : btnInactive)}>合约</button>
                    <button onClick={() => setMarket('spot')} className={cn(btnBase, 'border-l border-quant-border', market === 'spot' ? btnActive : btnInactive)}>现货</button>
                  </div>
                </FormField>
                <FormField label="交易对">
                  <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className={inputCls} />
                </FormField>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <FormField label="策略类型">
                  <select value={strategyType} onChange={(e) => setStrategyType(e.target.value)} className={inputCls}>
                    {STRAT_TYPES[market].map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                  </select>
                </FormField>
                <FormField label="K线周期">
                  <select value={timeframe} onChange={(e) => setTimeframe(e.target.value)} className={inputCls}>{TIMEFRAMES.map((tf) => <option key={tf} value={tf}>{tf}</option>)}</select>
                </FormField>
              </div>

              <div className="grid grid-cols-3 gap-4">
                <FormField label="初始资金 (USDT)">
                  <input type="number" value={initialCapital} onChange={(e) => setInitialCapital(Number(e.target.value))} className={inputCls} />
                </FormField>
                <FormField label="杠杆">
                  <input type="number" min={1} max={125} value={leverage} onChange={(e) => setLeverage(Number(e.target.value))} disabled={market === 'spot'} className={cn(inputCls, 'disabled:opacity-40')} />
                </FormField>
                <FormField label="交易方向">
                  <div className="flex gap-1 rounded-lg border border-quant-border overflow-hidden">
                    {(['long', 'short', 'dual'] as const).map((d) => (
                      <button key={d} onClick={() => setDirection(d)} className={cn('flex-1 py-2 text-xs font-medium transition-colors', direction === d ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
                        {d === 'long' ? '做多' : d === 'short' ? '做空' : '双向'}
                      </button>
                    ))}
                  </div>
                </FormField>
              </div>

              {/* Risk preview */}
              {!editing && mode === 'signal' && (
                <div className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
                  <div className="text-[10px] text-muted-foreground mb-2 flex items-center gap-1"><Target className="w-3 h-3" />风险预算预览</div>
                  <div className="grid grid-cols-4 gap-3 text-[11px]">
                    <div><span className="text-muted-foreground">单笔保证金</span><div className="font-mono text-foreground">${riskPreview.marginPerOrder.toFixed(2)}</div></div>
                    <div><span className="text-muted-foreground">最大总敞口</span><div className="font-mono text-foreground">${riskPreview.totalExposure.toFixed(2)}</div></div>
                    <div><span className="text-muted-foreground">最大亏损</span><div className={cn("font-mono", riskPreview.maxLoss > 0 ? "text-quant-red" : "text-muted-foreground")}>{riskPreview.maxLoss > 0 ? `$${riskPreview.maxLoss.toFixed(2)}` : '未设置'}</div></div>
                    <div><span className="text-muted-foreground">杠杆倍数</span><div className="font-mono text-foreground">{market === 'spot' ? '1x' : `${leverage}x`}</div></div>
                  </div>
                </div>
              )}

              {/* Dynamic strategy params */}
              {paramDefsLoading && <div className="text-xs text-muted-foreground py-2">加载参数定义...</div>}
              {paramDefs.length > 0 && (
                <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4">
                  <div className="flex items-center gap-2 text-xs font-semibold text-quant-gold"><SlidersHorizontal className="w-3.5 h-3.5" />策略参数</div>
                  <div className="grid grid-cols-2 gap-4">
                    {paramDefs.map((def) => (
                      <DynamicParamField key={def.name} def={def} value={dynamicParams[def.name]} onChange={(val) => setDynamicParams((prev) => ({ ...prev, [def.name]: val }))} />
                    ))}
                  </div>
                </div>
              )}
            </>
          )}

          {/* ═══ STEP 2: CRA 参数配置（使用共享组件） ═══ */}
          {mode === 'signal' && step === 1 && (
            <div className="space-y-3">
              <div className="text-xs text-muted-foreground mb-2">调整策略参数，或使用预设快速填充</div>

              <CollapsibleSection title="CRA 量化参数" count={4} defaultOpen>
                <CRAParamForm
                  value={modalToCra(p)}
                  onChange={(cra) => setP(craToModal(cra))}
                  showTradeCountMode
                  showExtended
                />
              </CollapsibleSection>

              <CollapsibleSection title="趋势指标时间框架" count={1}>
                {p.trendIndicator && (
                  <div className="flex gap-2">
                    {(['5m', '15m', '30m', '60m'] as const).map((tf) => (
                      <button key={tf} onClick={() => setP({ ...p, trendTimeframe: tf })} className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', p.trendTimeframe === tf ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>{tf}</button>
                    ))}
                  </div>
                )}
              </CollapsibleSection>
            </div>
          )}

          {/* ═══ STEP 3/2: 回测预览 (signal 模式) ═══ */}
          {mode === 'signal' && step === 2 && (
            <>
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-6 text-center">
                <BarChart3 className="w-8 h-8 text-quant-gold mx-auto mb-3" />
                <div className="text-sm font-semibold mb-1">回测预览</div>
                <div className="text-xs text-muted-foreground mb-4">基于最近30天数据快速回测，验证策略效果</div>
                <button onClick={handleRunBacktest} disabled={btLoading}
                  className={cn('px-6 py-2.5 rounded-lg text-xs font-medium transition-all', btLoading ? 'bg-quant-gold/30 text-quant-gold cursor-wait' : 'bg-quant-gold text-black hover:opacity-90')}>
                  {btLoading ? '回测中...' : '🚀 运行回测'}
                </button>
              </div>

              {btResult && (
                <div className="grid grid-cols-3 gap-3">
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">总收益</div>
                    <div className={cn('text-lg font-bold font-mono', btResult.totalReturn >= 0 ? 'text-quant-green' : 'text-quant-red')}>{btResult.totalReturn >= 0 ? '+' : ''}{btResult.totalReturn.toFixed(2)}%</div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">胜率</div>
                    <div className="text-lg font-bold font-mono text-foreground">{btResult.winRate.toFixed(1)}%</div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">最大回撤</div>
                    <div className={cn('text-lg font-bold font-mono', btResult.maxDrawdown > 20 ? 'text-quant-red' : 'text-quant-green')}>{btResult.maxDrawdown.toFixed(2)}%</div>
                  </div>
                  <div className="rounded-xl border border-quant-border p-4 text-center">
                    <div className="text-[10px] text-muted-foreground">盈亏比</div>
                    <div className="text-lg font-bold font-mono text-foreground">{btResult.profitFactor.toFixed(2)}</div>
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
                <div className="text-[11px] text-muted-foreground text-center py-4">点击上方按钮运行回测，结果将在这里展示</div>
              )}
            </>
          )}

          {/* ═══ Script: Step 2 代码编辑 ═══ */}
          {mode === 'script' && step === 2 && (
            <FormField label="策略代码 (Python)">
              <textarea value={codeWorkspace} onChange={(e) => setCodeWorkspace(e.target.value)} className="w-full h-64 bg-quant-bg border border-quant-border rounded-lg p-3 font-mono text-[11px] leading-relaxed resize-none focus:outline-none focus:border-quant-gold" spellCheck={false} />
            </FormField>
          )}

          {/* ═══ 执行设置 (signal: step 3, script: step 3) ═══ */}
          {step === (mode === 'script' ? 3 : 3) && (
            <>
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4">
                <div className="text-xs font-semibold mb-3">执行模式</div>
                <div className="grid grid-cols-2 gap-3">
                  <button onClick={() => setExecutionMode('live')} className={cn('flex items-start gap-3 p-4 rounded-xl border transition-all text-left', executionMode === 'live' ? 'border-quant-gold bg-quant-gold/5' : 'border-quant-border hover:border-quant-gold/30')}>
                    <div className={cn('w-10 h-10 rounded-lg flex items-center justify-center shrink-0', executionMode === 'live' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground')}><Zap className="w-5 h-5" /></div>
                    <div><div className="text-xs font-semibold">实盘交易</div><div className="text-[10px] text-muted-foreground mt-1">连接交易所API自动执行买卖</div></div>
                    {executionMode === 'live' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
                  </button>
                  <button onClick={() => setExecutionMode('signal')} className={cn('flex items-start gap-3 p-4 rounded-xl border transition-all text-left', executionMode === 'signal' ? 'border-quant-gold bg-quant-gold/5' : 'border-quant-border hover:border-quant-gold/30')}>
                    <div className={cn('w-10 h-10 rounded-lg flex items-center justify-center shrink-0', executionMode === 'signal' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground')}><Activity className="w-5 h-5" /></div>
                    <div><div className="text-xs font-semibold">信号通知</div><div className="text-[10px] text-muted-foreground mt-1">仅发送交易信号，不自动下单</div></div>
                    {executionMode === 'signal' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
                  </button>
                </div>
              </div>

              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4">
                <div className="text-xs font-semibold mb-3">通知渠道</div>
                <div className="grid grid-cols-3 gap-2">
                  {[{ key: 'browser', label: '浏览器' }, { key: 'email', label: '邮件' }, { key: 'telegram', label: 'Telegram' }, { key: 'discord', label: 'Discord' }, { key: 'webhook', label: 'Webhook' }, { key: 'phone', label: '短信' }].map((ch) => (
                    <label key={ch.key} className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer hover:text-foreground transition-colors">
                      <input type="checkbox" checked={notifyChannels.includes(ch.key)} onChange={(e) => setNotifyChannels((prev) => e.target.checked ? [...prev, ch.key] : prev.filter((c) => c !== ch.key))} className="rounded border-quant-border" />
                      {ch.label}
                    </label>
                  ))}
                </div>
              </div>
            </>
          )}

          {/* Script: Step 1 params (CRA params for script too) */}
          {mode === 'script' && step === 1 && (
            <div className="text-xs text-muted-foreground py-8 text-center">脚本模式无需额外参数，点击下一步编辑代码</div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
          <button onClick={onClose} className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors">取消</button>
          {step > 0 && <button onClick={() => setStep(step - 1)} className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors">上一步</button>}
          {step < steps.length - 1 ? (
            <button onClick={() => setStep(step + 1)} className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity">下一步</button>
          ) : (
            <button onClick={handleSubmit} className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity">{editing ? '保存修改' : '创建策略'}</button>
          )}
        </div>
      </div>
    </div>
  )
}
