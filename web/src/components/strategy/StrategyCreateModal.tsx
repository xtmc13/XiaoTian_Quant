import { useState, useEffect } from 'react'
import { useMutation } from '@tanstack/react-query'
import { strategyApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { StrategyParamDefs } from '@/types'
import { FormField, Toggle, DynamicParamField, STRAT_TYPES, TIMEFRAMES, DEFAULT_CODE, type StrategyRow } from './StrategyFormFields'
import { X, CheckCircle2, ChevronRight, Activity, FileCode2, SlidersHorizontal, Zap } from 'lucide-react'

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
    if (editing?.config_json) { try { const parsed = JSON.parse(editing.config_json); setDynamicParams((prev) => ({ ...prev, ...parsed })) } catch { /* ignore */ } }
  }, [editing])

  const [timeframe, setTimeframe] = useState(editing?.timeframe || '15m'), [leverage, setLeverage] = useState(editing?.leverage || 5)
  const [direction, setDirection] = useState<'long' | 'short' | 'dual'>('long'), [initialCapital, setInitialCapital] = useState(editing?.initial_capital || 1000)
  const [executionMode, setExecutionMode] = useState<'live' | 'signal'>('signal'), [notifyChannels, setNotifyChannels] = useState<string[]>(['browser'])

  // CRA params
  const [orderCount, setOrderCount] = useState(editing?.order_count || 7), [firstOrderAmount, setFirstOrderAmount] = useState(editing?.first_order_amount || 100)
  const [addPosSpread, setAddPosSpread] = useState(editing?.add_position_spread || 3), [addPosCallback, setAddPosCallback] = useState(editing?.add_position_callback || 0.1)
  const [takeProfitRatio, setTakeProfitRatio] = useState(editing?.take_profit_ratio || 1.3), [profitCallback, setProfitCallback] = useState(editing?.profit_callback || 0.1)
  const [tradeCountMode, setTradeCountMode] = useState<'single' | 'cycle'>(editing?.trade_count_mode || 'cycle'), [openIndicator, setOpenIndicator] = useState<string>(editing?.open_indicator || 'macd_golden')
  const [addPosIndicator, setAddPosIndicator] = useState<string>(editing?.add_position_indicator || 'macd'), [addPosMultiple, setAddPosMultiple] = useState(editing?.add_position_multiple || 1)
  const [waterfallProtection, setWaterfallProtection] = useState(editing?.waterfall_protection ?? 2), [openDouble, setOpenDouble] = useState(editing?.open_double || false)
  const [trendIndicator, setTrendIndicator] = useState(editing?.trend_indicator ?? false), [trendTimeframe, setTrendTimeframe] = useState(editing?.trend_timeframe || '15m')
  const [takeProfitMethod, setTakeProfitMethod] = useState(editing?.take_profit_method || 'full'), [movingTP, setMovingTP] = useState(editing?.moving_take_profit || { enabled: false, tier1_ratio: 1.5, tier1_drawback: 30, tier2_drawback: 20 })
  const [reverseTP, setReverseTP] = useState(editing?.reverse_take_profit ?? false), [reverseSL, setReverseSL] = useState(editing?.reverse_stop_loss ?? false)
  const [amplitude, setAmplitude] = useState(editing?.amplitude || { '5m': 2, '15m': 4, '30m': 7, '1h': 10 }), [burnCut, setBurnCut] = useState<{ enabled: boolean; dual_burn_start: number; global_burn_start: number }>(editing?.burn_cut && typeof editing.burn_cut === 'object' ? editing.burn_cut as { enabled: boolean; dual_burn_start: number; global_burn_start: number } : { enabled: false, dual_burn_start: 3, global_burn_start: 5 })
  const [customReduce, setCustomReduce] = useState(editing?.custom_reduce ?? false), [onlineOrderLimit, setOnlineOrderLimit] = useState(editing?.online_order_limit || 10)
  const [profitProtection, setProfitProtection] = useState(editing?.profit_protection ?? false), [followTrend, setFollowTrend] = useState(editing?.follow_trend ?? false)
  const [followTrendMax, setFollowTrendMax] = useState(editing?.follow_trend_max || 5), [stopLossRatio, setStopLossRatio] = useState(editing?.stop_loss_ratio || 0)
  const [stopLossAmount, setStopLossAmount] = useState(editing?.stop_loss_amount || 0), [stopLossPrice, setStopLossPrice] = useState(editing?.stop_loss_price || 0)
  const [firstOrderPrice, setFirstOrderPrice] = useState(editing?.first_order_price || 0), [closeAddPosition, setCloseAddPosition] = useState(editing?.close_add_position ?? false)

  // Script mode code workspace
  const [codeWorkspace, setCodeWorkspace] = useState(editing?.strategy_code || '')
  useEffect(() => {
    if (editing?.strategy_code !== undefined) setCodeWorkspace(editing.strategy_code)
  }, [editing?.strategy_code])

  const createMut = useMutation({ mutationFn: (data: Record<string, unknown>) => strategyApi.create(data), onSuccess: onSaved })
  const updateMut = useMutation({ mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) => strategyApi.update(id, data), onSuccess: onSaved })

  const handleSubmit = () => {
    // ── Validation ──
    if (!name.trim()) { alert('请输入策略名称'); return }
    if (!symbol.trim()) { alert('请输入交易对'); return }
    if (!strategyType) { alert('请选择策略类型'); return }
    if (market !== 'spot' && (!leverage || leverage < 1)) { alert('合约策略杠杆必须≥1'); return }

    const config: Record<string, unknown> = {
      order_count: orderCount, first_order_amount: firstOrderAmount, add_position_spread: addPosSpread, add_position_callback: addPosCallback,
      take_profit_ratio: takeProfitRatio, profit_callback: profitCallback, trade_count_mode: tradeCountMode, open_indicator: openIndicator,
      add_position_indicator: addPosIndicator, add_position_multiple: addPosMultiple, waterfall_protection: waterfallProtection, open_double: openDouble,
      trend_indicator: trendIndicator, trend_timeframe: trendTimeframe, take_profit_method: takeProfitMethod, moving_take_profit: movingTP,
      reverse_take_profit: reverseTP, reverse_stop_loss: reverseSL, amplitude, burn_cut: burnCut, custom_reduce: customReduce,
      online_order_limit: onlineOrderLimit, profit_protection: profitProtection, follow_trend: followTrend, follow_trend_max: followTrendMax,
      stop_loss_ratio: stopLossRatio, stop_loss_amount: stopLossAmount, stop_loss_price: stopLossPrice, first_order_price: firstOrderPrice,
      close_add_position: closeAddPosition, ...dynamicParams,
      // ── Contract fields ──
      market_type: market === 'spot' ? 'spot' : 'swap',
      position_side: direction === 'long' ? 'LONG' : direction === 'short' ? 'SHORT' : 'BOTH',
      margin_mode: 'cross', // default cross margin
      leverage: market === 'spot' ? 1 : leverage,
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
    if (mode === 'script') {
      payload.strategy_code = codeWorkspace
      payload.mode = 'script'
    } else {
      payload.mode = 'signal'
    }
    if (editing) { updateMut.mutate({ id: editing.id, data: payload }) } else { createMut.mutate(payload) }
  }

  const steps = mode === 'script' ? ['基础配置', '代码编辑', '执行设置'] : ['基础配置', '执行设置']
  const inputCls = "w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
  const btnBase = "flex-1 py-2 text-xs font-medium transition-colors"
  const btnActive = "bg-quant-gold/10 text-quant-gold"
  const btnInactive = "text-muted-foreground hover:text-foreground"

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="w-full max-w-2xl max-h-[85vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <h3 className="text-sm font-bold">{editing ? '编辑策略' : '创建策略'}{mode === 'script' ? ' - 脚本' : ''}</h3>
          <button onClick={onClose} aria-label="关闭" className="text-muted-foreground hover:text-foreground"><X className="w-4 h-4" /></button>
        </div>

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

        <div className="flex-1 overflow-y-auto p-6 space-y-5">
          {step === 0 && (
            <>
              {!editing && (
                <div className="flex rounded-lg border border-quant-border overflow-hidden">
                  <button onClick={() => setMode('signal')} className={cn(btnBase, mode === 'signal' ? btnActive : btnInactive)}><Activity className="w-3.5 h-3.5 inline mr-1" />指标信号</button>
                  <button onClick={() => setMode('script')} className={cn(btnBase, 'border-l border-quant-border', mode === 'script' ? btnActive : btnInactive)}><FileCode2 className="w-3.5 h-3.5 inline mr-1" />脚本代码</button>
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

              <FormField label="策略类型">
                <select value={strategyType} onChange={(e) => setStrategyType(e.target.value)} className={inputCls}>
                  {STRAT_TYPES[market].map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
                </select>
              </FormField>

              {paramDefsLoading && <div className="text-xs text-muted-foreground py-2">加载参数定义...</div>}
              {paramDefs.length > 0 && (
                <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4">
                  <div className="flex items-center gap-2 text-xs font-semibold text-quant-gold"><SlidersHorizontal className="w-3.5 h-3.5" />策略参数配置</div>
                  <div className="grid grid-cols-2 gap-4">
                    {paramDefs.map((def) => (
                      <DynamicParamField key={def.name} def={def} value={dynamicParams[def.name]} onChange={(val) => setDynamicParams((prev) => ({ ...prev, [def.name]: val }))} />
                    ))}
                  </div>
                </div>
              )}

              <div className="grid grid-cols-3 gap-4">
                <FormField label="K线周期">
                  <select value={timeframe} onChange={(e) => setTimeframe(e.target.value)} className={inputCls}>{TIMEFRAMES.map((tf) => <option key={tf} value={tf}>{tf}</option>)}</select>
                </FormField>
                <FormField label="杠杆">
                  <input type="number" min={1} max={125} value={leverage} onChange={(e) => setLeverage(Number(e.target.value))} disabled={market === 'spot'} className={cn(inputCls, 'disabled:opacity-40')} />
                </FormField>
                <FormField label="首单额度 (USDT)">
                  <input type="number" value={initialCapital} onChange={(e) => setInitialCapital(Number(e.target.value))} className={inputCls} />
                </FormField>
              </div>

              {market !== 'spot' && (
                <FormField label="交易方向">
                  <div className="flex gap-2">
                    {(['long', 'short', 'dual'] as const).map((d) => (
                      <button key={d} onClick={() => setDirection(d)} className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', direction === d ? 'bg-quant-green/10 border-quant-green/20 text-quant-green' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                        {d === 'long' ? '做多' : d === 'short' ? '做空' : '双向'}
                      </button>
                    ))}
                  </div>
                </FormField>
              )}

              {/* CRA params */}
              <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4">
                <div className="flex items-center gap-2 text-xs font-semibold text-quant-gold"><SlidersHorizontal className="w-3.5 h-3.5" />CRA 量化参数配置</div>

                <div className="grid grid-cols-2 gap-4">
                  <FormField label="做单数量 (5-7单)"><input type="number" min={1} max={20} value={orderCount} onChange={(e) => setOrderCount(Number(e.target.value))} className={inputCls} /></FormField>
                  <FormField label="首单仓位 (10-10000 USDT)"><input type="number" min={10} max={10000} step={10} value={firstOrderAmount} onChange={(e) => setFirstOrderAmount(Number(e.target.value))} className={inputCls} /></FormField>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3"><span className="text-xs text-muted-foreground">开仓加倍（首单x2）</span><Toggle value={openDouble} onChange={setOpenDouble} /></label>
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3"><span className="text-xs text-muted-foreground">关闭补仓（仅止盈）</span><Toggle value={closeAddPosition} onChange={setCloseAddPosition} /></label>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <FormField label="补仓价差 (0.5-50%)"><input type="number" min={0.5} max={50} step={0.5} value={addPosSpread} onChange={(e) => setAddPosSpread(Number(e.target.value))} className={inputCls} /></FormField>
                  <FormField label="补仓回调 (0.01-0.5%)"><input type="number" min={0.01} max={0.5} step={0.01} value={addPosCallback} onChange={(e) => setAddPosCallback(Number(e.target.value))} className={inputCls} /></FormField>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <FormField label="止盈比例 (%)"><input type="number" min={0.1} max={50} step={0.1} value={takeProfitRatio} onChange={(e) => setTakeProfitRatio(Number(e.target.value))} className={inputCls} /></FormField>
                  <FormField label="盈利回调 (0.01-0.5%)"><input type="number" min={0.01} max={0.5} step={0.01} value={profitCallback} onChange={(e) => setProfitCallback(Number(e.target.value))} className={inputCls} /></FormField>
                </div>

                <FormField label="止盈方式">
                  <div className="grid grid-cols-2 gap-2">
                    {[{ key: 'full', label: '全仓止盈', desc: '全仓盈利后卖出' }, { key: 'tail', label: '尾单止盈', desc: '最后一单盈利后卖出' }, { key: 'head_tail', label: '首尾止盈', desc: '首单+尾单盈利后卖出' }, { key: 'moving', label: '移动止盈', desc: '动态分档止盈' }].map((m) => (
                      <button key={m.key} onClick={() => setTakeProfitMethod(m.key as typeof takeProfitMethod)} className={cn('p-3 rounded-lg border text-left transition-colors', takeProfitMethod === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                        <div className="text-xs font-medium">{m.label}</div><div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
                      </button>
                    ))}
                  </div>
                </FormField>

                {takeProfitMethod === 'moving' && (
                  <div className="grid grid-cols-3 gap-3">
                    <FormField label="止盈比例 (%)"><input type="number" min={0.1} max={10} step={0.1} value={movingTP.tier1_ratio} onChange={(e) => setMovingTP({ ...movingTP, tier1_ratio: Number(e.target.value) })} className={inputCls} /></FormField>
                    <FormField label="第一档回撤 (%)"><input type="number" min={5} max={100} value={movingTP.tier1_drawback} onChange={(e) => setMovingTP({ ...movingTP, tier1_drawback: Number(e.target.value) })} className={inputCls} /></FormField>
                    <FormField label="第二档回撤 (%)"><input type="number" min={5} max={100} value={movingTP.tier2_drawback} onChange={(e) => setMovingTP({ ...movingTP, tier2_drawback: Number(e.target.value) })} className={inputCls} /></FormField>
                    <div className="col-span-3 text-[10px] text-muted-foreground">计算公式: {movingTP.tier1_ratio}% ± ({movingTP.tier1_ratio}% × {movingTP.tier1_drawback}%)，移动止盈开启后分仓/首尾止盈失效</div>
                  </div>
                )}

                <FormField label="开仓指标策略">
                  <select value={openIndicator} onChange={(e) => setOpenIndicator(e.target.value)} className={inputCls}>
                    <option value="macd_golden">MACD金叉开多</option><option value="macd_death">MACD死叉开空</option><option value="ema">EMA拐点开仓</option><option value="close">关闭（无脑买入）</option>
                  </select>
                </FormField>

                <FormField label="补仓指标（EMA和MACD补仓）">
                  <select value={addPosIndicator} onChange={(e) => setAddPosIndicator(e.target.value)} className={inputCls}>
                    <option value="macd">MACD金叉/死叉补仓</option><option value="ema">EMA4上下拐点补仓</option><option value="close">关闭（仅按跌幅补仓）</option>
                  </select>
                  <p className="text-[10px] text-muted-foreground mt-1">开启后需同时满足跌幅条件和指标条件才补仓，大行情时非常抗跌</p>
                </FormField>

                <div className="space-y-3">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div><div className="text-xs font-medium">趋势指标 (EMA4)</div><div className="text-[10px] text-muted-foreground">监控EMA指数平滑移动平均线</div></div>
                    <Toggle value={trendIndicator} onChange={setTrendIndicator} />
                  </label>
                  {trendIndicator && (
                    <FormField label="EMA4 时间周期">
                      <div className="flex gap-2">
                        {(['5m', '15m', '30m', '60m'] as const).map((tf) => (
                          <button key={tf} onClick={() => setTrendTimeframe(tf)} className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', trendTimeframe === tf ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>{tf}</button>
                        ))}
                      </div>
                      <p className="text-[10px] text-muted-foreground mt-1">时间越长准确性越高，但也越容易错过行情</p>
                    </FormField>
                  )}
                </div>

                <FormField label="防瀑布保护 (分钟内最大涨跌%)">
                  <input type="number" min={0.5} max={20} step={0.5} value={waterfallProtection} onChange={(e) => setWaterfallProtection(Number(e.target.value))} className={inputCls} />
                  <p className="text-[10px] text-muted-foreground mt-1">1分钟内单一币种涨跌超过设定值自动暂停补仓，默认2%</p>
                </FormField>

                <div className="grid grid-cols-2 gap-4">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div><div className="text-xs font-medium">反向止盈</div><div className="text-[10px] text-muted-foreground">MACD反向信号清仓</div></div>
                    <Toggle value={reverseTP} onChange={setReverseTP} />
                  </label>
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div><div className="text-xs font-medium">反向止损</div><div className="text-[10px] text-muted-foreground">MACD判断错误直接止损</div></div>
                    <Toggle value={reverseSL} onChange={setReverseSL} />
                  </label>
                </div>

                <div className="space-y-3">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div><div className="text-xs font-medium">顺势而为</div><div className="text-[10px] text-muted-foreground">逆势补仓后顺势单倍投，最高5倍</div></div>
                    <Toggle value={followTrend} onChange={setFollowTrend} />
                  </label>
                  {followTrend && (
                    <FormField label="顺势最大倍数 (逆势单补仓次数+首单，最高5倍)">
                      <input type="number" min={1} max={5} value={followTrendMax} onChange={(e) => setFollowTrendMax(Number(e.target.value))} className={inputCls} />
                    </FormField>
                  )}
                </div>

                <div className="space-y-3">
                  <label className="flex items-center justify-between rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div><div className="text-xs font-medium">斩仓和燃烧</div><div className="text-[10px] text-muted-foreground">用顺势单盈利消耗逆势单浮亏</div></div>
                    <Toggle value={burnCut.enabled} onChange={(v) => setBurnCut({ ...burnCut, enabled: v })} />
                  </label>
                  {burnCut.enabled && (
                    <div className="grid grid-cols-2 gap-4">
                      <FormField label="双向燃烧起始仓">
                        <input type="number" min={1} max={10} value={burnCut.dual_burn_start} onChange={(e) => setBurnCut({ ...burnCut, dual_burn_start: Number(e.target.value) })} className={inputCls} />
                        <p className="text-[10px] text-muted-foreground">默认第3仓启动</p>
                      </FormField>
                      <FormField label="全局燃烧起始仓">
                        <input type="number" min={1} max={10} value={burnCut.global_burn_start} onChange={(e) => setBurnCut({ ...burnCut, global_burn_start: Number(e.target.value) })} className={inputCls} />
                        <p className="text-[10px] text-muted-foreground">默认第5仓启动跨币种燃烧</p>
                      </FormField>
                    </div>
                  )}
                </div>

                <div className="space-y-3">
                  <div className="text-xs font-medium text-muted-foreground">止损设置（三选一）</div>
                  <div className="grid grid-cols-3 gap-3">
                    <FormField label="止损比例 (%)"><input type="number" min={0} max={100} step={0.1} value={stopLossRatio} onChange={(e) => setStopLossRatio(Number(e.target.value))} className={inputCls} /></FormField>
                    <FormField label="止损金额 (USDT)"><input type="number" min={0} value={stopLossAmount} onChange={(e) => setStopLossAmount(Number(e.target.value))} className={inputCls} /></FormField>
                    <FormField label="止损价格"><input type="number" min={0} value={stopLossPrice} onChange={(e) => setStopLossPrice(Number(e.target.value))} className={inputCls} /></FormField>
                  </div>
                </div>

                <FormField label="首单挂单价格 (0=实时市价)">
                  <input type="number" min={0} value={firstOrderPrice} onChange={(e) => setFirstOrderPrice(Number(e.target.value))} className={inputCls} />
                  <p className="text-[10px] text-muted-foreground mt-1">输入固定价格后，只有价格低于设定值系统才会买入</p>
                </FormField>

                <div className="grid grid-cols-3 gap-3">
                  <FormField label="限制在线单量"><input type="number" min={1} max={50} value={onlineOrderLimit} onChange={(e) => setOnlineOrderLimit(Number(e.target.value))} className={inputCls} /></FormField>
                  <label className="flex flex-col justify-center rounded-lg border border-quant-border bg-quant-bg p-3"><span className="text-xs text-muted-foreground mb-1">盈利保护</span><Toggle value={profitProtection} onChange={setProfitProtection} /></label>
                  <label className="flex flex-col justify-center rounded-lg border border-quant-border bg-quant-bg p-3"><span className="text-xs text-muted-foreground mb-1">自定义减仓</span><Toggle value={customReduce} onChange={setCustomReduce} /></label>
                </div>

                <div className="space-y-2">
                  <div className="text-xs font-medium text-muted-foreground">振幅设置（各周期建议值）</div>
                  <div className="grid grid-cols-4 gap-3">
                    {([{ key: '5m', label: '5分钟', suggest: 2 }, { key: '15m', label: '15分钟', suggest: 4 }, { key: '30m', label: '30分钟', suggest: 7 }, { key: '1h', label: '1小时', suggest: 10 }] as const).map((a) => (
                      <FormField key={a.key} label={a.label}>
                        <input type="number" min={0.1} max={50} step={0.1} value={amplitude[a.key]} onChange={(e) => setAmplitude({ ...amplitude, [a.key]: Number(e.target.value) })} className={inputCls} />
                        <p className="text-[10px] text-muted-foreground">建议{a.suggest}%</p>
                      </FormField>
                    ))}
                  </div>
                </div>

                <FormField label="交易次数">
                  <div className="flex gap-2">
                    {[{ key: 'single', label: '单次循环', desc: '止盈后不再买入，补仓继续' }, { key: 'cycle', label: '策略循环', desc: '卖出后持续买入直到次数用尽' }].map((m) => (
                      <button key={m.key} onClick={() => setTradeCountMode(m.key as typeof tradeCountMode)} className={cn('flex-1 p-3 rounded-lg border text-left transition-colors', tradeCountMode === m.key ? 'bg-quant-gold/10 border-quant-gold/30' : 'border-quant-border bg-quant-bg hover:border-quant-gold/20')}>
                        <div className="text-xs font-medium">{m.label}</div><div className="text-[10px] text-muted-foreground mt-0.5">{m.desc}</div>
                      </button>
                    ))}
                  </div>
                </FormField>
              </div>
            </>
          )}

          {mode === 'script' && step === 1 && (
            <FormField label="策略代码 (Python)">
              <textarea value={codeWorkspace} onChange={(e) => setCodeWorkspace(e.target.value)} className="w-full h-64 bg-quant-bg border border-quant-border rounded-lg p-3 font-mono text-[11px] leading-relaxed resize-none focus:outline-none focus:border-quant-gold" spellCheck={false} />
            </FormField>
          )}

          {step === (mode === 'script' ? 2 : 1) && (
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
        </div>

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
