/**
 * Indicator IDE — XiaoTianQuant-style split-panel layout with I/O contract.
 * Code editor (left) + Chart/Backtest workspace (right).
 */
import { useState, useEffect, useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { cn, formatCurrency } from '@/lib/utils'
import { marketApi, indicatorApi, strategyApi, communityApi } from '@/lib/api'
import { TRADING_INTERVALS } from '@/lib/constants'
import { KlineChart } from '@/components/charts/KlineChart'
import { EquityCurve } from '@/components/charts/EquityCurve'
import { CodeEditor } from '@/components/ide/CodeEditor'
import { ParamPanel } from '@/components/ide/ParamPanel'
import { ValidationBanner } from '@/components/ide/ValidationBanner'
import { SectionCard } from '@/components/ui/SectionCard'
import { KPICard } from '@/components/ui/KPICard'
import type { KLineBar } from '@/lib/technicalIndicators'
import {
  Play, Save, Code, BarChart3, TrendingUp, TrendingDown,
  Target, Activity, Zap, ChevronUp, ChevronDown,
  Maximize2, Minimize2, Plus, Trash2, Loader2, AlertCircle,
  X, Sparkles, PanelLeft,
  Copy, Upload, BookOpen, GitBranch, PauseCircle, Wand2,
} from 'lucide-react'
import {
  DEFAULT_INDICATOR_CODE,
  INDICATOR_TEMPLATES,
  parseParamsFromCode,
  type ParseResult,
  type ValidationHint,
} from '@/lib/indicatorContract'

/* ── Types ───────────────────────────────────────────────────────── */

interface IndicatorConfig {
  id: string; name: string; shortName: string
  type: 'line' | 'band' | 'macd' | 'adx'
  params: Record<string, number>
  style?: { color?: string; lineWidth?: number }
  visible?: boolean; instanceId?: string
}

interface SavedIndicator {
  id: number; name: string; code: string
  symbol?: string; timeframe?: string
  is_encrypted?: number; pricing_type?: string
}

/* ── Main Page ───────────────────────────────────────────────────── */

export function IndicatorIDE() {
  // Selection
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  const [searchParams, setSearchParams] = useSearchParams()

  // Code
  const [code, setCode] = useState(DEFAULT_INDICATOR_CODE)
  const [codeDirty, setCodeDirty] = useState(false)
  const [selectedIndicatorId, setSelectedIndicatorId] = useState<number | null>(null)

  // Parsed contract state
  const [parsed, setParsed] = useState<ParseResult>(() => parseParamsFromCode(DEFAULT_INDICATOR_CODE))
  const [paramValues, setParamValues] = useState<Record<string, unknown>>({})
  const [validationHints, setValidationHints] = useState<ValidationHint[]>([])
  const [validating, setValidating] = useState(false)

  // UI
  const [codeDrawerVisible, setCodeDrawerVisible] = useState(true)
  const [codePanelExpanded, setCodePanelExpanded] = useState(true)
  const [editorFullscreen, setEditorFullscreen] = useState(false)
  const [chartFullscreen, setChartFullscreen] = useState(false)
  const [workspaceTab, setWorkspaceTab] = useState<'chart' | 'backtest'>('chart')
  const [activeIndicators, setActiveIndicators] = useState<IndicatorConfig[]>([])
  const [chartIndicatorRunning, setChartIndicatorRunning] = useState(false)

  // AI
  const [aiPrompt, setAiPrompt] = useState('')
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiPanelExpanded, setAiPanelExpanded] = useState(false)
  const [aiStatus, setAiStatus] = useState<string>('') // generating | validating | auto_fixing | completed | error
  const [aiStreamedCode, setAiStreamedCode] = useState<string>('')

  // Experiment
  const [experimentRunning, setExperimentRunning] = useState(false)
  const [experimentResult, setExperimentResult] = useState<Record<string, unknown> | null>(null)
  const [experimentPanelExpanded, setExperimentPanelExpanded] = useState(false)
  const [optimizer, setOptimizer] = useState<'de' | 'tpe'>('de')

  // Backtest
  const [initialCapital, setInitialCapital] = useState(10000)
  const [leverage, setLeverage] = useState(1)
  const [commission, setCommission] = useState(0.05)
  const [slippage, setSlippage] = useState(0.01)
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [running, setRunning] = useState(false)
  const [backtestResult, setBacktestResult] = useState<Record<string, unknown> | null>(null)

  // Indicator list
  const [indicators, setIndicators] = useState<SavedIndicator[]>([])

  /* ── Select indicator ── */
  const selectIndicator = useCallback((ind: SavedIndicator) => {
    setSelectedIndicatorId(ind.id)
    setCode(ind.code)
    setCodeDirty(false)
    setValidationHints([])
  }, [])

  // Chart signals from indicator execution
  const [chartSignals, setChartSignals] = useState<Array<{timestamp: number; price: number; side: 'buy' | 'sell'; text: string; color: string}>>([])

  /* ── Load saved indicators ── */
  useEffect(() => {
    indicatorApi.list().then((res: unknown) => {
      const list = Array.isArray(res) ? res : ((res as Record<string, unknown>)?.data || [])
      setIndicators(Array.isArray(list) ? list : [])
    }).catch(() => {})
  }, [])

  /* ── Auto-load indicator from URL ?id=xxx ── */
  useEffect(() => {
    const idParam = searchParams.get('id')
    if (!idParam || indicators.length === 0) return
    const id = Number(idParam)
    if (!id) return
    const ind = indicators.find(i => i.id === id)
    if (ind) {
      selectIndicator(ind)
      // Clear id from URL after loading to avoid re-trigger on refresh
      setSearchParams({}, { replace: true })
    }
  }, [searchParams, indicators, selectIndicator, setSearchParams])

  /* ── Parse code on change ── */
  useEffect(() => {
    const result = parseParamsFromCode(code)
    setParsed(result)
    // Reset param values when params declarations change
    const defaults: Record<string, unknown> = {}
    for (const p of result.params) {
      defaults[p.name] = p.default
    }
    setParamValues(prev => {
      const merged = { ...defaults }
      for (const k of Object.keys(defaults)) {
        if (prev[k] !== undefined) merged[k] = prev[k]
      }
      return merged
    })
  }, [code])

  /* ── KLine data ── */
  const { data: klinesRaw, isLoading: klLoading } = useQuery({
    queryKey: ['ide-kline', symbol, interval],
    queryFn: () => marketApi.klines(symbol, interval, 500),
    refetchInterval: 10000,
  })

  const klines: KLineBar[] = useMemo(() => {
    if (!klinesRaw) return []
    const raw = Array.isArray(klinesRaw) ? klinesRaw : (klinesRaw as Record<string, unknown>)?.data ?? klinesRaw
    const arr = Array.isArray(raw) ? raw : (raw as Record<string, unknown>)?.klines || []
    if (!Array.isArray(arr) || !arr.length) return []
    return arr.map((k: Record<string, unknown>) => ({
      timestamp: (k.time || k.timestamp || 0) as number,
      open: parseFloat(String(k.open)) || 0, high: parseFloat(String(k.high)) || 0,
      low: parseFloat(String(k.low)) || 0, close: parseFloat(String(k.close)) || 0,
      volume: parseFloat(String(k.volume)) || 0,
    }))
  }, [klinesRaw])

  /* ── Execute indicator on chart when running ── */
  useEffect(() => {
    if (!chartIndicatorRunning || klines.length === 0) {
      setChartSignals([])
      return
    }
    let cancelled = false
    const runIndicator = async () => {
      try {
        const dfJSON = klines.map(k => ({
          time: k.timestamp,
          open: k.open,
          high: k.high,
          low: k.low,
          close: k.close,
          volume: k.volume,
        }))
        const res = await indicatorApi.execute({
          code,
          params: paramValues,
          df_json: dfJSON,
        }) as unknown as { data?: { output?: { signals?: Array<{type: string; text?: string; data?: (number|null)[]; color?: string}>; plots?: Array<{name: string; data: unknown[]; color?: string; overlay?: boolean}> } }; success?: boolean }
        if (cancelled) return
        const output = res?.data?.output
        if (!output?.signals) {
          setChartSignals([])
          return
        }
        const signals: Array<{timestamp: number; price: number; side: 'buy' | 'sell'; text: string; color: string}> = []
        for (const sig of output.signals) {
          if (!sig.data) continue
          for (let i = 0; i < sig.data.length; i++) {
            const price = sig.data[i]
            if (price === null || price === undefined) continue
            const kline = klines[i]
            if (!kline) continue
            signals.push({
              timestamp: kline.timestamp ?? 0,
              price: Number(price),
              side: sig.type === 'buy' ? 'buy' : 'sell',
              text: sig.text || (sig.type === 'buy' ? 'B' : 'S'),
              color: sig.color || (sig.type === 'buy' ? '#00E676' : '#FF5252'),
            })
          }
        }
        if (!cancelled) setChartSignals(signals)
      } catch {
        if (!cancelled) setChartSignals([])
      }
    }
    runIndicator()
    return () => { cancelled = true }
  }, [chartIndicatorRunning, code, klines, paramValues])

  /* ── Snapshot ── */
  const { data: snapshotRaw } = useQuery({
    queryKey: ['ide-snap', symbol],
    queryFn: () => marketApi.snapshot(symbol),
    refetchInterval: 5000,
  })
  const snapshot = snapshotRaw
  const lastPrice = snapshot?.price ? Number(snapshot.price) : 0
  const change24h = snapshot?.change_24h ? Number(snapshot.change_24h) : 0

  /* ── Validate ── */
  const handleValidate = useCallback(async () => {
    setValidating(true)
    try {
      const res = await indicatorApi.validate(code) as unknown as { data?: { hints?: ValidationHint[] }; hints?: ValidationHint[] }
      const data = res?.data ?? res
      if (data?.hints) {
        setValidationHints(data.hints)
      } else {
        setValidationHints([])
      }
    } catch (e: unknown) {
      setValidationHints([{ severity: 'error', code: 'VALIDATE_REQUEST_FAILED', params: { msg: e instanceof Error ? e.message : String(e) } }])
    } finally {
      setValidating(false)
    }
  }, [code])

  /* ── Save ── */
  const handleSave = useCallback(async () => {
    // Validate first
    await handleValidate()
    try {
      await indicatorApi.save({
        id: selectedIndicatorId || 0,
        name: parsed.name,
        description: parsed.description,
        code,
      })
      setCodeDirty(false)
      indicatorApi.list().then((res: unknown) => {
        const list = Array.isArray(res) ? res : ((res as Record<string, unknown>)?.data || [])
        setIndicators(Array.isArray(list) ? list : [])
      }).catch(() => {})
    } catch { /* ignore */ }
  }, [code, selectedIndicatorId, parsed, handleValidate])

  const handleDelete = useCallback(async () => {
    if (!selectedIndicatorId || !confirm('确认删除？')) return
    try {
      await indicatorApi.delete(selectedIndicatorId)
      setSelectedIndicatorId(null)
      setCode(DEFAULT_INDICATOR_CODE)
      setCodeDirty(false)
      setValidationHints([])
      indicatorApi.list().then((res: unknown) => {
        const list = Array.isArray(res) ? res : ((res as Record<string, unknown>)?.data || [])
        setIndicators(Array.isArray(list) ? list : [])
      }).catch(() => {})
    } catch { /* ignore */ }
  }, [selectedIndicatorId])

  const handleSaveAs = useCallback(async () => {
    try {
      await indicatorApi.saveAs({
        name: parsed.name + ' (副本)',
        description: parsed.description,
        code,
      })
      setCodeDirty(false)
      indicatorApi.list().then((res: unknown) => {
        const list = Array.isArray(res) ? res : ((res as Record<string, unknown>)?.data || [])
        setIndicators(Array.isArray(list) ? list : [])
      }).catch(() => {})
    } catch { /* ignore */ }
  }, [code, parsed])

  const handlePublish = useCallback(async () => {
    if (!selectedIndicatorId) {
      // 先保存再发布
      await handleSave()
      const latest = await indicatorApi.list()
      const list = Array.isArray(latest) ? latest : []
      const newest = list[list.length - 1]
      if (!newest?.id) return
      setSelectedIndicatorId(newest.id)
    }
    const pricingType = confirm('是否设置为付费指标？\n确定=付费，取消=免费') ? 'paid' : 'free'
    const price = pricingType === 'paid' ? Number(prompt('请输入积分价格', '100') || '100') : 0
    try {
      await communityApi.publish({
        indicatorId: selectedIndicatorId || 0,
        pricingType,
        price,
      })
      alert('发布成功！')
    } catch (e: unknown) {
      alert('发布失败: ' + (e instanceof Error ? e.message : '未知错误'))
    }
  }, [selectedIndicatorId, handleSave])

  /* ── AI Generate (SSE Streaming) ── */
  const handleAiGenerate = useCallback(async () => {
    if (!aiPrompt.trim()) return
    setAiGenerating(true)
    setAiStatus('generating')
    setAiStreamedCode('')
    setValidationHints([])
    let streamed = ''
    try {
      await indicatorApi.aiGenerateStream(
        { prompt: aiPrompt, existingCode: code !== DEFAULT_INDICATOR_CODE ? code : '' },
        {
          onCodeChunk: (chunk) => {
            streamed += chunk
            setAiStreamedCode(streamed)
          },
          onStatus: (status) => {
            setAiStatus(status)
            if (status === 'auto_fixing_round_1' || status === 'auto_fixing_round_2' || status === 'auto_fixing_round_3') {
              setAiStatus('auto_fixing')
            }
          },
          onValidation: (result) => {
            const r = result as unknown as { hints?: ValidationHint[] }
            if (r?.hints) {
              setValidationHints(r.hints)
            }
          },
          onCodeReplace: (newCode) => {
            setCode(newCode)
            setCodeDirty(true)
            setAiStreamedCode(newCode)
          },
          onDebug: (info) => {
            console.warn('[AI Debug]', info)
          },
          onDone: () => {
            setAiStatus('completed')
            // If we have streamed code but no explicit replacement, use the streamed version
            if (streamed && code === (code !== DEFAULT_INDICATOR_CODE ? code : DEFAULT_INDICATOR_CODE)) {
              const cleaned = streamed.replace(/```python\n?/g, '').replace(/```\n?/g, '').trim()
              if (cleaned) {
                setCode(cleaned)
                setCodeDirty(true)
              }
            }
          },
          onError: (err) => {
            setAiStatus('error')
            setValidationHints([{ severity: 'error', code: 'AI_GENERATE_ERROR', params: { msg: err } }])
          },
        }
      )
    } catch (e: unknown) {
      setAiStatus('error')
      setValidationHints([{ severity: 'error', code: 'AI_GENERATE_ERROR', params: { msg: e instanceof Error ? e.message : '生成失败' } }])
    } finally {
      setAiGenerating(false)
    }
  }, [aiPrompt, code])

  /* ── Experiment (Auto-tune) ── */
  const handleRunExperiment = useCallback(async () => {
    if (parsed.params.length === 0) return
    setExperimentRunning(true); setExperimentResult(null)
    try {
      const payload: Record<string, unknown> = {
        code,
        symbol,
        interval,
        optimizer,
        oos_ratio: 0.3,
        backtest_config: {
          initial_balance: initialCapital,
          commission: commission / 100,
          slippage: slippage / 100,
        },
      }
      const res = await indicatorApi.experiment.run(payload)
      setExperimentResult(res?.data || res)
      const best = res?.data?.best_params || res?.best_params
      if (best) {
        setParamValues(prev => ({ ...prev, ...best }))
      }
    } catch (e: unknown) {
      setExperimentResult({ error: e instanceof Error ? e.message : '实验失败' })
    } finally { setExperimentRunning(false) }
  }, [code, symbol, interval, optimizer, parsed.params.length, initialCapital, commission, slippage])

  /* ── Backtest ── */
  const handleRunBacktest = useCallback(async () => {
    setRunning(true); setBacktestResult(null)
    try {
      const payload: Record<string, unknown> = {
        code,
        symbol,
        interval,
        klines: klines.map(k => ({
          time: k.timestamp,
          open: k.open,
          high: k.high,
          low: k.low,
          close: k.close,
          volume: k.volume,
        })),
        backtest_config: {
          initial_balance: initialCapital,
          commission: commission / 100,
          slippage: slippage / 100,
        },
      }
      if (startDate) payload.start_date = startDate
      if (endDate) payload.end_date = endDate
      const result = await indicatorApi.backtest(payload)
      setBacktestResult(result)
    } catch (e: unknown) {
      setBacktestResult({ error: e instanceof Error ? e.message : '回测失败' })
    } finally { setRunning(false) }
  }, [code, symbol, interval, klines, initialCapital, commission, slippage, startDate, endDate])

  /* ── Backtest metrics ── */
  const backtestMetrics = useMemo(() => {
    if (!backtestResult || backtestResult.error) return null
    const r = backtestResult as Record<string, unknown>
    const totalReturnPct = Number(r.total_return_pct ?? 0)
    const totalReturn = Number(r.total_return ?? 0)
    const finalEquity = initialCapital + totalReturn
    const maxDrawdownPct = Number(r.max_drawdown_pct ?? 0)
    const sharpeRatio = Number(r.sharpe_ratio ?? 0)
    const winRate = Number(r.win_rate ?? 0)
    const profitFactor = Number(r.profit_factor ?? 0)
    const t = 'up' as const; const d = 'down' as const; const n = 'neutral' as const
    return [
      { label: '总收益率', value: `${totalReturnPct >= 0 ? '+' : ''}${totalReturnPct.toFixed(2)}%`, icon: TrendingUp, trend: totalReturnPct >= 0 ? t : d },
      { label: '最终权益', value: `$${formatCurrency(finalEquity)}`, icon: BarChart3, trend: n },
      { label: '最大回撤', value: `${maxDrawdownPct.toFixed(2)}%`, icon: TrendingDown, trend: d },
      { label: '夏普比率', value: sharpeRatio.toFixed(2), icon: Target, trend: n },
      { label: '胜率', value: `${winRate.toFixed(1)}%`, icon: Target, trend: t },
      { label: '盈亏比', value: profitFactor.toFixed(2), icon: Activity, trend: n },
    ]
  }, [backtestResult, initialCapital])

  const isUp = change24h >= 0

  /* ── Create Strategy from Indicator ── */
  const [showCreateStrategy, setShowCreateStrategy] = useState(false)
  const [stratForm, setStratForm] = useState({
    name: '', symbol: 'BTCUSDT', interval: '1h', leverage: 5,
    direction: 'long' as 'long' | 'short' | 'dual',
    orderCount: 7, firstOrderAmount: 100, addPosSpread: 3,
    addPosCallback: 0.1, tpRatio: 1.3, profitCallback: 0.1,
    tpMethod: 'full' as 'full' | 'tail' | 'head_tail' | 'moving',
    openInd: 'macd_golden', addInd: 'macd',
    waterfall: 2, openDouble: false, trendInd: false,
    trendTf: '15m', followTrend: false, burnCut: false,
    closeAddPos: false, tradeCountMode: 'cycle' as 'single' | 'cycle',
  })

  const handleCreateStrategyFromIndicator = useCallback(async () => {
    try {
      await strategyApi.create({
        name: stratForm.name || `${parsed.name}策略`,
        symbol: stratForm.symbol,
        timeframe: stratForm.interval,
        leverage: stratForm.leverage,
        trade_direction: stratForm.direction,
        market_type: 'swap',
        strategy_type: 'custom_indicator',
        strategy_code: code,
        status: 'stopped',
        order_count: stratForm.orderCount,
        first_order_amount: stratForm.firstOrderAmount,
        add_position_spread: stratForm.addPosSpread,
        add_position_callback: stratForm.addPosCallback,
        take_profit_ratio: stratForm.tpRatio,
        profit_callback: stratForm.profitCallback,
        take_profit_method: stratForm.tpMethod,
        open_indicator: stratForm.openInd,
        add_position_indicator: stratForm.addInd,
        waterfall_protection: stratForm.waterfall,
        open_double: stratForm.openDouble,
        trend_indicator: stratForm.trendInd,
        trend_timeframe: stratForm.trendTf,
        follow_trend: stratForm.followTrend,
        burn_cut: { enabled: stratForm.burnCut, dual_burn_start: 3, global_burn_start: 5 },
        close_add_position: stratForm.closeAddPos,
        trade_count_mode: stratForm.tradeCountMode,
      })
      setShowCreateStrategy(false)
      alert('策略创建成功！请到策略管理页面启动。')
    } catch (e: unknown) {
      alert('创建策略失败: ' + (e instanceof Error ? e.message : String(e)))
    }
  }, [stratForm, code, parsed.name])

  /* ═══════════════════════════════════════════════════════════════ */
  /*  Render                                                          */
  /* ═══════════════════════════════════════════════════════════════ */

  return (
    <>
      <div className="h-full flex flex-col bg-quant-bg">
      {/* ── Code rail (collapsed state) ── */}
      {!codeDrawerVisible && !chartFullscreen && (
        <div
          onClick={() => setCodeDrawerVisible(true)}
          className="absolute left-0 top-1/2 -translate-y-1/2 z-10 w-8 h-20 flex flex-col items-center justify-center gap-1 rounded-r-lg border border-l-0 border-quant-border bg-quant-bg-secondary cursor-pointer hover:bg-quant-bg-tertiary transition-colors"
        >
          <Code className="w-4 h-4 text-muted-foreground" />
          <span className="text-[9px] text-muted-foreground" style={{ writingMode: 'vertical-rl' }}>代码</span>
        </div>
      )}

      <div className="flex-1 flex min-h-0">
        {/* ═══════════════════════════════════════════════════════════
            LEFT: Code Panel
        ═══════════════════════════════════════════════════════════ */}
        {codeDrawerVisible && !chartFullscreen && (
          <div className={cn(
            'flex flex-col border-r border-quant-border bg-quant-bg-secondary shrink-0 transition-all',
            editorFullscreen ? 'w-full absolute inset-0 z-20' : 'w-[440px]',
          )}>
            {/* ── Code panel header ── */}
            <div className="flex items-center justify-between px-3 py-2 border-b border-quant-border shrink-0">
              <div className="flex items-center gap-2">
                <Code className="w-4 h-4 text-muted-foreground" />
                {codeDirty && <span className="px-1.5 py-0 rounded text-[9px] font-medium bg-amber-500/10 text-amber-400">已修改</span>}
              </div>
              <div className="flex items-center gap-0.5">
                {/* Template selector */}
                <select
                  value=""
                  onChange={e => {
                    const tmpl = INDICATOR_TEMPLATES.find(t => t.key === e.target.value)
                    if (tmpl) {
                      setCode(tmpl.code)
                      setCodeDirty(true)
                      setSelectedIndicatorId(null)
                      setValidationHints([])
                    }
                    e.target.value = ''
                  }}
                  className="bg-quant-bg border border-quant-border rounded px-1.5 py-1 text-[10px] text-white outline-none focus:border-quant-gold mr-1"
                  title="加载模板"
                >
                  <option value="">模板 ▾</option>
                  {INDICATOR_TEMPLATES.map(t => <option key={t.key} value={t.key}>{t.label}</option>)}
                </select>
                {/* New */}
                <button onClick={() => { setCode(DEFAULT_INDICATOR_CODE); setSelectedIndicatorId(null); setCodeDirty(false); setValidationHints([]) }} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="新建" aria-label="新建"><Plus className="w-3.5 h-3.5" /></button>
                {/* Save */}
                <button onClick={handleSave} disabled={!codeDirty} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5 disabled:opacity-30" title="保存" aria-label="保存"><Save className="w-3.5 h-3.5" /></button>
                {/* Delete */}
                <button onClick={handleDelete} disabled={!selectedIndicatorId} className="p-1.5 rounded text-muted-foreground hover:text-quant-red hover:bg-white/5 disabled:opacity-30" title="删除" aria-label="删除"><Trash2 className="w-3.5 h-3.5" /></button>
                {/* Validate */}
                <button onClick={handleValidate} disabled={validating} className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-white/5 disabled:opacity-30" title="验证代码" aria-label="验证代码">
                  {validating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <AlertCircle className="w-3.5 h-3.5" />}
                </button>
                {/* Publish */}
                <button onClick={handlePublish} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="发布到社区" aria-label="发布到社区"><Upload className="w-3.5 h-3.5" /></button>
                {/* Create Strategy */}
                <button onClick={() => setShowCreateStrategy(true)} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="从指标创建策略" aria-label="从指标创建策略"><GitBranch className="w-3.5 h-3.5" /></button>
                {/* Save As */}
                <button onClick={handleSaveAs} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="另存为" aria-label="另存为"><Copy className="w-3.5 h-3.5" /></button>
                {/* Fullscreen */}
                <button onClick={() => setEditorFullscreen(!editorFullscreen)} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title={editorFullscreen ? '退出全屏' : '全屏编辑器'} aria-label={editorFullscreen ? '退出全屏' : '全屏编辑器'}>
                  {editorFullscreen ? <Minimize2 className="w-3.5 h-3.5" /> : <Maximize2 className="w-3.5 h-3.5" />}
                </button>
                {/* Run/Stop on chart */}
                <button onClick={() => setChartIndicatorRunning(!chartIndicatorRunning)} className={cn('p-1.5 rounded', chartIndicatorRunning ? 'text-quant-green bg-quant-green/10' : 'text-muted-foreground hover:text-foreground hover:bg-white/5')} title={chartIndicatorRunning ? '停止' : '在图表上运行'} aria-label={chartIndicatorRunning ? '停止' : '在图表上运行'}>
                  {chartIndicatorRunning ? <PauseCircle className="w-3.5 h-3.5" /> : <Play className="w-3.5 h-3.5" />}
                </button>
                {/* Collapse */}
                <button onClick={() => setCodePanelExpanded(!codePanelExpanded)} className="p-1 rounded text-muted-foreground hover:text-foreground ml-1" aria-label={codePanelExpanded ? '收起代码面板' : '展开代码面板'}>
                  {codePanelExpanded ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
                </button>
              </div>
            </div>

            {/* ── Code panel body ── */}
            {codePanelExpanded && (
              <>
                {/* Guide bar */}
                <div className="flex items-center gap-1.5 px-3 py-1.5 text-[10px] text-muted-foreground border-b border-quant-border bg-quant-bg-tertiary">
                  <BookOpen className="w-3 h-3" />
                  <span>策略开发指南</span>
                  <button onClick={() => window.open('/docs/strategy-guide', '_blank')} className="text-quant-gold hover:underline ml-auto text-[10px]">查看文档 →</button>
                </div>

                {/* AI Panel toggle */}
                <div className="px-3 py-1.5 border-b border-quant-border bg-quant-bg-tertiary">
                  <button onClick={() => setAiPanelExpanded(!aiPanelExpanded)} className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground transition-colors">
                    <Sparkles className="w-3 h-3 text-quant-gold" />
                    AI 代码生成
                    {aiPanelExpanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                  </button>
                  {aiPanelExpanded && (
                    <div className="mt-2 space-y-2">
                      <textarea
                        value={aiPrompt}
                        onChange={e => setAiPrompt(e.target.value)}
                        placeholder="描述你想要的策略，AI 将为你生成代码..."
                        className="w-full bg-quant-bg border border-quant-border rounded-md px-3 py-2 text-xs text-white placeholder-muted-foreground outline-none focus:border-quant-gold resize-none h-16"
                      />
                      <button onClick={handleAiGenerate} disabled={aiGenerating || !aiPrompt.trim()}
                        className="flex items-center gap-1.5 rounded-md bg-quant-gold px-3 py-1.5 text-xs font-medium text-black hover:opacity-90 disabled:opacity-50">
                        {aiGenerating ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
                        {aiGenerating
                          ? (aiStatus === 'generating' ? '生成中...'
                            : aiStatus === 'validating' ? '验证中...'
                            : aiStatus === 'auto_fixing' ? '自动修复中...'
                            : '处理中...')
                          : '生成代码'}
                      </button>
                      {/* Stream preview */}
                      {aiGenerating && aiStreamedCode && (
                        <div className="rounded border border-quant-border bg-quant-bg p-2">
                          <div className="text-[9px] text-muted-foreground mb-1 flex items-center gap-1">
                            <Loader2 className="w-2.5 h-2.5 animate-spin" />
                            {aiStatus === 'generating' ? '实时生成' : aiStatus === 'validating' ? '验证代码' : aiStatus === 'auto_fixing' ? '自动修复' : '处理中'}
                          </div>
                          <pre className="text-[10px] text-quant-green/80 font-mono whitespace-pre-wrap max-h-32 overflow-y-auto">{aiStreamedCode.slice(-400)}</pre>
                        </div>
                      )}
                    </div>
                  )}
                </div>

                {/* Validation hints */}
                {validationHints.length > 0 && (
                  <div className="px-3 pt-2 border-b border-quant-border bg-quant-bg-tertiary">
                    <ValidationBanner hints={validationHints} />
                  </div>
                )}

                {/* CodeMirror editor */}
                <div className="flex-1 min-h-0">
                  <CodeEditor
                    value={code}
                    onChange={v => { setCode(v); setCodeDirty(true); setValidationHints([]) }}
                    theme="dark"
                    placeholder="输入 Python 策略代码..."
                  />
                </div>

                {/* Param panel */}
                {parsed.params.length > 0 && (
                  <div className="px-3 py-2 border-t border-quant-border bg-quant-bg-tertiary shrink-0 max-h-40 overflow-y-auto">
                    <ParamPanel
                      params={parsed.params}
                      values={paramValues}
                      onChange={(name, value) => setParamValues(prev => ({ ...prev, [name]: value }))}
                    />
                  </div>
                )}

                {/* Experiment panel */}
                {parsed.params.length > 0 && (
                  <div className="px-3 py-2 border-t border-quant-border bg-quant-bg-tertiary shrink-0">
                    <button
                      onClick={() => setExperimentPanelExpanded(!experimentPanelExpanded)}
                      className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground transition-colors w-full"
                    >
                      <Wand2 className="w-3 h-3 text-quant-gold" />
                      自动调参 (DE / TPE)
                      {experimentPanelExpanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                    </button>
                    {experimentPanelExpanded && (
                      <div className="mt-2 space-y-2">
                        <div className="flex items-center gap-2">
                          <select
                            value={optimizer}
                            onChange={e => setOptimizer(e.target.value as 'de' | 'tpe')}
                            className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-[11px] text-white outline-none focus:border-quant-gold"
                          >
                            <option value="de">差分进化 (DE)</option>
                            <option value="tpe">贝叶斯优化 (TPE)</option>
                          </select>
                          <button
                            onClick={handleRunExperiment}
                            disabled={experimentRunning}
                            className="flex items-center gap-1 rounded bg-quant-gold/20 px-2 py-1 text-[11px] font-medium text-quant-gold hover:bg-quant-gold/30 disabled:opacity-50"
                          >
                            {experimentRunning ? <Loader2 className="w-3 h-3 animate-spin" /> : <Wand2 className="w-3 h-3" />}
                            {experimentRunning ? '优化中...' : '开始优化'}
                          </button>
                        </div>
                        {!!experimentResult?.error && (
                          <div className="text-[10px] text-red-400">{String(experimentResult.error)}</div>
                        )}
                        {experimentResult && (experimentResult.best_score as number) > 0 && (
                          <div className="space-y-1">
                            <div className="text-[10px] text-quant-green">
                              最佳评分: {(experimentResult.best_score as number).toFixed(1)}
                            </div>
                            {!!(experimentResult.is_score as Record<string, unknown>)?.factor_scores && (
                              <div className="grid grid-cols-3 gap-1">
                                {Object.entries(((experimentResult.is_score as Record<string, unknown>).factor_scores) as Record<string, number>).map(([k, v]: [string, number]) => (
                                  <div key={k} className="rounded bg-quant-bg px-1.5 py-0.5 text-[9px]">
                                    <span className="text-muted-foreground">{k}</span>
                                    <span className="ml-1 text-quant-gold font-mono">{v.toFixed(0)}</span>
                                  </div>
                                ))}
                              </div>
                            )}
                            {(experimentResult.oos_validation as Record<string, unknown>)?.passed === false && (
                              <div className="text-[10px] text-amber-400">⚠ 样本外验证未通过（可能过拟合）</div>
                            )}
                            {!!(experimentResult.oos_validation as Record<string, unknown>)?.passed && (
                              <div className="text-[10px] text-quant-green">✓ 样本外验证通过</div>
                            )}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </>
            )}

            {/* Hide drawer handle */}
            <div className="flex items-center justify-center py-1 border-t border-quant-border shrink-0">
              <button onClick={() => setCodeDrawerVisible(false)} className="p-1 rounded text-muted-foreground hover:text-foreground">
                <ChevronUp className="w-4 h-4 rotate-90" />
              </button>
            </div>
          </div>
        )}

        {/* ═══════════════════════════════════════════════════════════
            RIGHT: Workspace
        ═══════════════════════════════════════════════════════════ */}
        <div className="flex-1 flex flex-col min-w-0">
          {/* Workspace toolbar */}
          <div className="h-10 flex items-center justify-between px-3 border-b border-quant-border bg-quant-bg-secondary shrink-0">
            <div className="flex items-center gap-2">
              {!codeDrawerVisible && (
                <button onClick={() => setCodeDrawerVisible(true)} className="p-1 rounded text-muted-foreground hover:text-foreground" title="代码面板">
                  <PanelLeft className="w-4 h-4" />
                </button>
              )}
              {/* Tabs */}
              <div className="flex rounded bg-quant-bg-tertiary p-0.5">
                {(['chart', 'backtest'] as const).map(t => (
                  <button key={t} onClick={() => setWorkspaceTab(t)}
                    className={cn('px-3 py-1 rounded text-xs font-medium transition-colors', workspaceTab === t ? 'bg-quant-card text-foreground' : 'text-muted-foreground hover:text-foreground')}>
                    {t === 'chart' ? '图表' : '回测'}
                  </button>
                ))}
              </div>
            </div>

            <div className="flex items-center gap-2">
              {/* Indicator selector */}
              <select
                value={selectedIndicatorId ?? ''}
                onChange={e => {
                  const id = Number(e.target.value)
                  const ind = indicators.find(i => i.id === id)
                  if (ind) selectIndicator(ind)
                }}
                className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs text-white outline-none focus:border-quant-gold max-w-[160px] truncate"
              >
                <option value="">选择指标...</option>
                {indicators.map(ind => (
                  <option key={ind.id} value={ind.id}>{ind.name}</option>
                ))}
              </select>

              {/* Symbol */}
              <select value={symbol} onChange={e => setSymbol(e.target.value)}
                className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs text-white outline-none focus:border-quant-gold">
                {['BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'ADAUSDT', 'DOGEUSDT'].map(s => (
                  <option key={s} value={s}>{s.replace('USDT', '/USDT')}</option>
                ))}
              </select>

              {/* Timeframe */}
              <div className="flex rounded bg-quant-bg-tertiary p-0.5">
                {TRADING_INTERVALS.map(int => (
                  <button key={int} onClick={() => setInterval(int)}
                    className={cn('px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors', interval === int ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
                    {int}
                  </button>
                ))}
              </div>

              <button onClick={() => setChartFullscreen(!chartFullscreen)} className="p-1 rounded text-muted-foreground hover:text-foreground" title={chartFullscreen ? '退出全屏' : '图表全屏'}>
                {chartFullscreen ? <Minimize2 className="w-3.5 h-3.5" /> : <Maximize2 className="w-3.5 h-3.5" />}
              </button>
            </div>
          </div>

          {/* Content */}
          <div className="flex-1 flex min-h-0">
            {/* ── Chart Tab ── */}
            {workspaceTab === 'chart' && (
              <div className="flex-1 flex flex-col min-h-0 p-2 gap-1.5">
                {/* Price bar */}
                <div className="flex items-center gap-4 px-2 py-1 rounded bg-quant-bg-secondary border border-quant-border shrink-0">
                  <span className="font-bold text-sm">{symbol.replace('USDT', '/USDT')}</span>
                  <span className={cn('font-mono text-sm font-bold', isUp ? 'text-quant-green' : 'text-quant-red')}>${lastPrice.toFixed(2)}</span>
                  <span className={cn('text-xs font-mono', isUp ? 'text-quant-green' : 'text-quant-red')}>{change24h >= 0 ? '+' : ''}{change24h.toFixed(2)}%</span>
                  {chartIndicatorRunning && <span className="text-[10px] text-quant-gold bg-quant-gold/10 px-1.5 py-0 rounded">指标运行中</span>}
                </div>

                {/* KLineChart */}
                <div className="flex-1 min-h-0 rounded-lg overflow-hidden border border-quant-border">
                  <KlineChart data={klines} loading={klLoading} signals={chartSignals} activeIndicators={activeIndicators} onActiveIndicatorsChange={setActiveIndicators} theme="dark" />
                </div>
              </div>
            )}

            {/* ── Backtest Tab ── */}
            {workspaceTab === 'backtest' && (
              <div className="flex-1 overflow-y-auto p-3 space-y-4">
                <SectionCard title="回测参数" bodyClassName="space-y-3">
                  {/* Date */}
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">开始日期</label>
                      <input type="date" value={startDate} onChange={e => setStartDate(e.target.value)}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" aria-label="开始日期" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">结束日期</label>
                      <input type="date" value={endDate} onChange={e => setEndDate(e.target.value)}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" aria-label="结束日期" />
                    </div>
                  </div>
                  {/* Capital + Leverage */}
                  <div className="grid grid-cols-4 gap-3">
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">初始资金</label>
                      <input type="number" min={100} value={initialCapital} onChange={e => setInitialCapital(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" aria-label="初始资金" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">杠杆</label>
                      <input type="number" min={1} max={125} value={leverage} onChange={e => setLeverage(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" aria-label="杠杆倍数" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">手续费 %</label>
                      <input type="number" min={0} max={10} step={0.01} value={commission} onChange={e => setCommission(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" aria-label="手续费百分比" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">滑点 %</label>
                      <input type="number" min={0} max={10} step={0.01} value={slippage} onChange={e => setSlippage(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" aria-label="滑点百分比" />
                    </div>
                  </div>
                  <button onClick={handleRunBacktest} disabled={running}
                    className="flex items-center gap-1.5 rounded bg-quant-gold px-4 py-1.5 text-xs font-medium text-black hover:opacity-90 disabled:opacity-50">
                    {running ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}
                    {running ? '运行中...' : '运行回测'}
                  </button>
                </SectionCard>

                {/* Error */}
                {!!backtestResult?.error && (
                  <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400"><AlertCircle className="w-3.5 h-3.5" />{String(backtestResult.error)}</div>
                )}

                {/* Results */}
                {backtestMetrics && (
                  <>
                    <div className="grid grid-cols-3 gap-3">
                      {backtestMetrics.map(m => <KPICard key={m.label} icon={<m.icon className="w-3.5 h-3.5 text-muted-foreground" />} label={m.label} value={m.value} trend={m.trend} />)}
                    </div>
                    {/* Equity Curve */}
                    {Array.isArray(backtestResult?.equity_curve) && (backtestResult.equity_curve as Array<{timestamp: number; equity: number}>).length > 1 && (
                      <SectionCard title="权益曲线">
                        <EquityCurve data={backtestResult.equity_curve as Array<{timestamp: number; equity: number}>} height={160} />
                      </SectionCard>
                    )}
                    {backtestResult && Array.isArray(backtestResult.trades) && backtestResult.trades.length > 0 && (
                      <SectionCard title={`交易记录 (${(backtestResult.trades as unknown as { length: number }).length}笔)`}>
                        <div className="overflow-x-auto max-h-52">
                          <table className="w-full text-[10px]">
                            <thead><tr className="text-muted-foreground text-left"><th scope="col" className="px-2 py-1 font-medium">#</th><th scope="col" className="px-2 py-1 font-medium">方向</th><th scope="col" className="px-2 py-1 font-medium">入场价</th><th scope="col" className="px-2 py-1 font-medium">出场价</th><th scope="col" className="px-2 py-1 font-medium">数量</th><th scope="col" className="px-2 py-1 font-medium">盈亏</th></tr></thead>
                            <tbody>
                              {(backtestResult.trades as Record<string, unknown>[]).map((t, i: number) => {
                                const side = String(t.side || t.Side || '')
                                const entryPrice = Number(t.entry_price ?? t.EntryPrice ?? 0)
                                const exitPrice = Number(t.exit_price ?? t.ExitPrice ?? 0)
                                const qty = Number(t.quantity ?? t.Quantity ?? t.qty ?? 0)
                                const pnl = Number(t.realized_pnl ?? t.RealizedPnL ?? t.pnl ?? 0)
                                return (
                                  <tr key={i} className="border-t border-quant-border/40">
                                    <td className="px-2 py-1 text-muted-foreground">{i + 1}</td>
                                    <td className="px-2 py-1">{side === 'buy' || side === 'BUY' || side === 'LONG' ? '买' : '卖'}</td>
                                    <td className="px-2 py-1 font-mono">${formatCurrency(entryPrice)}</td>
                                    <td className="px-2 py-1 font-mono">${formatCurrency(exitPrice)}</td>
                                    <td className="px-2 py-1 font-mono">{qty.toFixed(4)}</td>
                                    <td className={cn('px-2 py-1 font-mono font-bold', pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>${pnl.toFixed(2)}</td>
                                  </tr>
                                )
                              })}
                            </tbody>
                          </table>
                        </div>
                      </SectionCard>
                    )}
                  </>
                )}

                {!running && !backtestResult && (
                  <div className="py-12 flex items-center justify-center text-xs text-muted-foreground">配置参数后点击「运行回测」</div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>

    {/* ── Create Strategy from Indicator Modal ── */}
    {showCreateStrategy && (
      <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
        <div className="w-full max-w-2xl max-h-[85vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
          <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
            <h3 className="text-sm font-bold flex items-center gap-2">
              <GitBranch className="w-4 h-4 text-quant-gold" />
              从指标创建策略
            </h3>
            <button onClick={() => setShowCreateStrategy(false)} aria-label="关闭" className="text-muted-foreground hover:text-foreground"><X className="w-4 h-4" /></button>
          </div>
          <div className="flex-1 overflow-y-auto p-6 space-y-4">
            <div className="text-xs text-muted-foreground mb-2">
              基于当前指标 <span className="text-quant-gold font-medium">{parsed.name}</span> 创建量化策略
            </div>

            {/* 基础配置 */}
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">策略名称</label>
                <input value={stratForm.name} onChange={(e) => setStratForm({ ...stratForm, name: e.target.value })}
                  placeholder={`${parsed.name}策略`}
                  className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">交易对</label>
                <input value={stratForm.symbol} onChange={(e) => setStratForm({ ...stratForm, symbol: e.target.value.toUpperCase() })}
                  className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
              </div>
            </div>
            <div className="grid grid-cols-3 gap-4">
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">K线周期</label>
                <select value={stratForm.interval} onChange={(e) => setStratForm({ ...stratForm, interval: e.target.value })}
                  className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                  {TRADING_INTERVALS.map((i) => <option key={i} value={i}>{i}</option>)}
                </select>
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">杠杆</label>
                <input type="number" min={1} max={125} value={stratForm.leverage} onChange={(e) => setStratForm({ ...stratForm, leverage: Number(e.target.value) })}
                  className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">方向</label>
                <select value={stratForm.direction} onChange={(e) => setStratForm({ ...stratForm, direction: e.target.value as typeof stratForm.direction })}
                  className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                  <option value="long">做多</option>
                  <option value="short">做空</option>
                  <option value="dual">双向</option>
                </select>
              </div>
            </div>

            {/* CRA 参数 */}
            <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4">
              <div className="text-xs font-semibold text-quant-gold">CRA 量化参数</div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">做单数量</label>
                  <input type="number" min={1} max={20} value={stratForm.orderCount} onChange={(e) => setStratForm({ ...stratForm, orderCount: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">首单仓位 (USDT)</label>
                  <input type="number" min={10} max={10000} step={10} value={stratForm.firstOrderAmount} onChange={(e) => setStratForm({ ...stratForm, firstOrderAmount: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓价差 (%)</label>
                  <input type="number" min={0.5} max={50} step={0.5} value={stratForm.addPosSpread} onChange={(e) => setStratForm({ ...stratForm, addPosSpread: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓回调 (%)</label>
                  <input type="number" min={0.01} max={0.5} step={0.01} value={stratForm.addPosCallback} onChange={(e) => setStratForm({ ...stratForm, addPosCallback: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈比例 (%)</label>
                  <input type="number" min={0.1} max={50} step={0.1} value={stratForm.tpRatio} onChange={(e) => setStratForm({ ...stratForm, tpRatio: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">盈利回调 (%)</label>
                  <input type="number" min={0.01} max={0.5} step={0.01} value={stratForm.profitCallback} onChange={(e) => setStratForm({ ...stratForm, profitCallback: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
                </div>
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">止盈方式</label>
                <div className="flex gap-2">
                  {([
                    { key: 'full', label: '全仓止盈' },
                    { key: 'tail', label: '尾单止盈' },
                    { key: 'head_tail', label: '首尾止盈' },
                    { key: 'moving', label: '移动止盈' },
                  ] as const).map((m) => (
                    <button key={m.key} onClick={() => setStratForm({ ...stratForm, tpMethod: m.key })}
                      className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', stratForm.tpMethod === m.key ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                      {m.label}
                    </button>
                  ))}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">开仓指标</label>
                  <select value={stratForm.openInd} onChange={(e) => setStratForm({ ...stratForm, openInd: e.target.value })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                    <option value="macd_golden">MACD金叉开多</option>
                    <option value="macd_death">MACD死叉开空</option>
                    <option value="ema">EMA拐点开仓</option>
                    <option value="close">关闭（无脑买入）</option>
                  </select>
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">补仓指标</label>
                  <select value={stratForm.addInd} onChange={(e) => setStratForm({ ...stratForm, addInd: e.target.value })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold">
                    <option value="macd">MACD补仓</option>
                    <option value="ema">EMA4补仓</option>
                    <option value="close">仅按跌幅</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">防瀑布 (%)</label>
                <input type="number" min={0.5} max={20} step={0.5} value={stratForm.waterfall} onChange={(e) => setStratForm({ ...stratForm, waterfall: Number(e.target.value) })}
                  className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold" />
              </div>
              <div className="flex flex-wrap gap-3">
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input type="checkbox" checked={stratForm.openDouble} onChange={(e) => setStratForm({ ...stratForm, openDouble: e.target.checked })} className="rounded" />
                  开仓加倍
                </label>
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input type="checkbox" checked={stratForm.trendInd} onChange={(e) => setStratForm({ ...stratForm, trendInd: e.target.checked })} className="rounded" />
                  趋势指标(EMA4)
                </label>
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input type="checkbox" checked={stratForm.followTrend} onChange={(e) => setStratForm({ ...stratForm, followTrend: e.target.checked })} className="rounded" />
                  顺势而为
                </label>
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input type="checkbox" checked={stratForm.burnCut} onChange={(e) => setStratForm({ ...stratForm, burnCut: e.target.checked })} className="rounded" />
                  斩仓燃烧
                </label>
                <label className="flex items-center gap-2 text-xs text-muted-foreground">
                  <input type="checkbox" checked={stratForm.closeAddPos} onChange={(e) => setStratForm({ ...stratForm, closeAddPos: e.target.checked })} className="rounded" />
                  关闭补仓
                </label>
              </div>
              <div>
                <label className="text-[11px] text-muted-foreground mb-1.5 block">交易次数</label>
                <div className="flex gap-2">
                  {([
                    { key: 'single', label: '单次循环' },
                    { key: 'cycle', label: '策略循环' },
                  ] as const).map((m) => (
                    <button key={m.key} onClick={() => setStratForm({ ...stratForm, tradeCountMode: m.key })}
                      className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', stratForm.tradeCountMode === m.key ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                      {m.label}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          </div>
          <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
            <button onClick={() => setShowCreateStrategy(false)} className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors">取消</button>
            <button onClick={handleCreateStrategyFromIndicator} className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity flex items-center gap-1.5">
              <Zap className="w-3.5 h-3.5" /> 创建策略
            </button>
          </div>
        </div>
      </div>
    )}
  </>
  )
}
