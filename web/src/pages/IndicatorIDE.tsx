/**
 * Indicator IDE — QuantDinger-style split-panel layout with I/O contract.
 * Code editor (left) + Chart/Backtest workspace (right).
 */
import { useState, useEffect, useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, formatCurrency } from '@/lib/utils'
import { marketApi, indicatorApi } from '@/lib/api'
import { KlineChart } from '@/components/charts/KlineChart'
import { CodeEditor } from '@/components/ide/CodeEditor'
import { ParamPanel } from '@/components/ide/ParamPanel'
import { ValidationBanner } from '@/components/ide/ValidationBanner'
import { SectionCard } from '@/components/ui/SectionCard'
import { KPICard } from '@/components/ui/KPICard'
import type { KLineBar } from '@/lib/technicalIndicators'
import {
  Play, Save, RotateCcw, Code, BarChart3, TrendingUp, TrendingDown,
  Target, Activity, Zap, ChevronUp, ChevronDown, Download,
  Maximize2, Minimize2, Plus, Trash2, Loader2, AlertCircle,
  Search, Clock, X, Settings, Send, Sparkles, PanelLeft,
  Copy, Upload, BookOpen, GitBranch, PauseCircle, Wand2,
} from 'lucide-react'
import {
  DEFAULT_INDICATOR_CODE,
  parseParamsFromCode,
  type ParseResult,
  type ValidationHint,
} from '@/lib/indicatorContract'

/* ── Constants ───────────────────────────────────────────────────── */

const INTERVALS = ['1m', '5m', '15m', '30m', '1h', '4h', '1d', '1w']

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

  // Code
  const [code, setCode] = useState(DEFAULT_INDICATOR_CODE)
  const [codeDirty, setCodeDirty] = useState(false)
  const [selectedIndicatorId, setSelectedIndicatorId] = useState<number | null>(null)

  // Parsed contract state
  const [parsed, setParsed] = useState<ParseResult>(() => parseParamsFromCode(DEFAULT_INDICATOR_CODE))
  const [paramValues, setParamValues] = useState<Record<string, any>>({})
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
  const [experimentResult, setExperimentResult] = useState<any>(null)
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
  const [backtestResult, setBacktestResult] = useState<any>(null)

  // Indicator list
  const [indicators, setIndicators] = useState<SavedIndicator[]>([])

  /* ── Load saved indicators ── */
  useEffect(() => {
    indicatorApi.list().then((res: any) => {
      const list = Array.isArray(res) ? res : (res?.data || [])
      setIndicators(Array.isArray(list) ? list : [])
    }).catch(() => {})
  }, [])

  /* ── Parse code on change ── */
  useEffect(() => {
    const result = parseParamsFromCode(code)
    setParsed(result)
    // Reset param values when params declarations change
    const defaults: Record<string, any> = {}
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
    const raw = (klinesRaw as any)?.data ?? klinesRaw
    const arr = Array.isArray(raw) ? raw : (raw as any)?.klines || []
    if (!Array.isArray(arr) || !arr.length) return []
    return arr.map((k: any) => ({
      timestamp: k.time || k.timestamp || 0,
      open: parseFloat(k.open) || 0, high: parseFloat(k.high) || 0,
      low: parseFloat(k.low) || 0, close: parseFloat(k.close) || 0,
      volume: parseFloat(k.volume) || 0,
    }))
  }, [klinesRaw])

  /* ── Snapshot ── */
  const { data: snapshotRaw } = useQuery({
    queryKey: ['ide-snap', symbol],
    queryFn: () => marketApi.snapshot(symbol),
    refetchInterval: 5000,
  })
  const snapshot = (snapshotRaw as any)?.data ?? snapshotRaw
  const lastPrice = snapshot?.price ? Number(snapshot.price) : 0
  const change24h = snapshot?.change_24h ? Number(snapshot.change_24h) : 0

  /* ── Select indicator ── */
  const selectIndicator = useCallback((ind: SavedIndicator) => {
    setSelectedIndicatorId(ind.id)
    setCode(ind.code)
    setCodeDirty(false)
    setValidationHints([])
  }, [])

  /* ── Validate ── */
  const handleValidate = useCallback(async () => {
    setValidating(true)
    try {
      const res = await indicatorApi.validate(code)
      const data = res?.data ?? res
      if (data?.hints) {
        setValidationHints(data.hints)
      } else {
        setValidationHints([])
      }
    } catch (e: any) {
      setValidationHints([{ severity: 'error', code: 'VALIDATE_REQUEST_FAILED', params: { msg: e.message } }])
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
        symbol,
        interval,
      })
      setCodeDirty(false)
      indicatorApi.list().then((res: any) => {
        const list = Array.isArray(res) ? res : (res?.data || [])
        setIndicators(Array.isArray(list) ? list : [])
      }).catch(() => {})
    } catch (_) {}
  }, [code, symbol, interval, selectedIndicatorId, parsed, handleValidate])

  const handleDelete = useCallback(async () => {
    if (!selectedIndicatorId || !confirm('确认删除？')) return
    try {
      await indicatorApi.delete(selectedIndicatorId)
      setSelectedIndicatorId(null)
      setCode(DEFAULT_INDICATOR_CODE)
      setCodeDirty(false)
      setValidationHints([])
      indicatorApi.list().then((res: any) => {
        const list = Array.isArray(res) ? res : (res?.data || [])
        setIndicators(Array.isArray(list) ? list : [])
      }).catch(() => {})
    } catch (_) {}
  }, [selectedIndicatorId])

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
            if (result?.hints) {
              setValidationHints(result.hints)
            }
          },
          onCodeReplace: (newCode) => {
            setCode(newCode)
            setCodeDirty(true)
            setAiStreamedCode(newCode)
          },
          onDebug: (info) => {
            console.log('[AI Debug]', info)
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
    } catch (e: any) {
      setAiStatus('error')
      setValidationHints([{ severity: 'error', code: 'AI_GENERATE_ERROR', params: { msg: e.message || '生成失败' } }])
    } finally {
      setAiGenerating(false)
    }
  }, [aiPrompt, code])

  /* ── Experiment (Auto-tune) ── */
  const handleRunExperiment = useCallback(async () => {
    if (parsed.params.length === 0) return
    setExperimentRunning(true); setExperimentResult(null)
    try {
      const payload: any = {
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
    } catch (e: any) {
      setExperimentResult({ error: e.message || '实验失败' })
    } finally { setExperimentRunning(false) }
  }, [code, symbol, interval, optimizer, parsed.params.length, initialCapital, commission, slippage])

  /* ── Backtest ── */
  const handleRunBacktest = useCallback(async () => {
    setRunning(true); setBacktestResult(null)
    try {
      const payload: any = {
        symbol, interval, code,
        initial_balance: { USDT: initialCapital },
        leverage, commission: commission / 100, slippage: slippage / 100,
        num_bars: 500,
      }
      if (startDate) payload.start_date = startDate
      if (endDate) payload.end_date = endDate
      const result = await indicatorApi.backtest(payload)
      setBacktestResult(result)
    } catch (e: any) {
      setBacktestResult({ error: e.message || '回测失败' })
    } finally { setRunning(false) }
  }, [symbol, interval, code, initialCapital, leverage, commission, slippage, startDate, endDate])

  /* ── Backtest metrics ── */
  const backtestMetrics = useMemo(() => {
    if (!backtestResult?.report) return null
    const r = backtestResult.report; const t = 'up' as const; const d = 'down' as const; const n = 'neutral' as const
    return [
      { label: '总收益率', value: `${r.total_return_pct >= 0 ? '+' : ''}${r.total_return_pct?.toFixed(2)}%`, icon: TrendingUp, trend: r.total_return_pct >= 0 ? t : d },
      { label: '最终权益', value: `$${formatCurrency(r.final_equity)}`, icon: BarChart3, trend: n },
      { label: '最大回撤', value: `${r.max_drawdown_pct?.toFixed(2)}%`, icon: TrendingDown, trend: d },
      { label: '夏普比率', value: r.sharpe_ratio?.toFixed(2), icon: Target, trend: n },
      { label: '胜率', value: `${r.win_rate_pct?.toFixed(1)}%`, icon: Target, trend: t },
      { label: '盈亏比', value: r.profit_factor?.toFixed(2), icon: Activity, trend: n },
    ]
  }, [backtestResult])

  const isUp = change24h >= 0

  /* ═══════════════════════════════════════════════════════════════ */
  /*  Render                                                          */
  /* ═══════════════════════════════════════════════════════════════ */

  return (
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
                {/* New */}
                <button onClick={() => { setCode(DEFAULT_INDICATOR_CODE); setSelectedIndicatorId(null); setCodeDirty(false); setValidationHints([]) }} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="新建"><Plus className="w-3.5 h-3.5" /></button>
                {/* Save */}
                <button onClick={handleSave} disabled={!codeDirty} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5 disabled:opacity-30" title="保存"><Save className="w-3.5 h-3.5" /></button>
                {/* Delete */}
                <button onClick={handleDelete} disabled={!selectedIndicatorId} className="p-1.5 rounded text-muted-foreground hover:text-quant-red hover:bg-white/5 disabled:opacity-30" title="删除"><Trash2 className="w-3.5 h-3.5" /></button>
                {/* Validate */}
                <button onClick={handleValidate} disabled={validating} className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-white/5 disabled:opacity-30" title="验证代码">
                  {validating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <AlertCircle className="w-3.5 h-3.5" />}
                </button>
                {/* Publish */}
                <button className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="发布到社区"><Upload className="w-3.5 h-3.5" /></button>
                {/* Create Strategy */}
                <button className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="从指标创建策略"><GitBranch className="w-3.5 h-3.5" /></button>
                {/* Save As */}
                <button className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title="另存为"><Copy className="w-3.5 h-3.5" /></button>
                {/* Fullscreen */}
                <button onClick={() => setEditorFullscreen(!editorFullscreen)} className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5" title={editorFullscreen ? '退出全屏' : '全屏编辑器'}>
                  {editorFullscreen ? <Minimize2 className="w-3.5 h-3.5" /> : <Maximize2 className="w-3.5 h-3.5" />}
                </button>
                {/* Run/Stop on chart */}
                <button onClick={() => setChartIndicatorRunning(!chartIndicatorRunning)} className={cn('p-1.5 rounded', chartIndicatorRunning ? 'text-quant-green bg-quant-green/10' : 'text-muted-foreground hover:text-foreground hover:bg-white/5')} title={chartIndicatorRunning ? '停止' : '在图表上运行'}>
                  {chartIndicatorRunning ? <PauseCircle className="w-3.5 h-3.5" /> : <Play className="w-3.5 h-3.5" />}
                </button>
                {/* Collapse */}
                <button onClick={() => setCodePanelExpanded(!codePanelExpanded)} className="p-1 rounded text-muted-foreground hover:text-foreground ml-1">
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
                  <a href="#" className="text-quant-gold hover:underline ml-auto">查看文档 →</a>
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
                        {experimentResult?.error && (
                          <div className="text-[10px] text-red-400">{experimentResult.error}</div>
                        )}
                        {experimentResult?.best_score > 0 && (
                          <div className="space-y-1">
                            <div className="text-[10px] text-quant-green">
                              最佳评分: {experimentResult.best_score.toFixed(1)}
                            </div>
                            {experimentResult.is_score?.factor_scores && (
                              <div className="grid grid-cols-3 gap-1">
                                {Object.entries(experimentResult.is_score.factor_scores).map(([k, v]: [string, any]) => (
                                  <div key={k} className="rounded bg-quant-bg px-1.5 py-0.5 text-[9px]">
                                    <span className="text-muted-foreground">{k}</span>
                                    <span className="ml-1 text-quant-gold font-mono">{v.toFixed(0)}</span>
                                  </div>
                                ))}
                              </div>
                            )}
                            {experimentResult.oos_validation?.passed === false && (
                              <div className="text-[10px] text-amber-400">⚠ 样本外验证未通过（可能过拟合）</div>
                            )}
                            {experimentResult.oos_validation?.passed && (
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
                {INTERVALS.map(int => (
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
                  <KlineChart data={klines} loading={klLoading} activeIndicators={activeIndicators} onActiveIndicatorsChange={setActiveIndicators} theme="dark" />
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
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">结束日期</label>
                      <input type="date" value={endDate} onChange={e => setEndDate(e.target.value)}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" />
                    </div>
                  </div>
                  {/* Capital + Leverage */}
                  <div className="grid grid-cols-4 gap-3">
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">初始资金</label>
                      <input type="number" min={100} value={initialCapital} onChange={e => setInitialCapital(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">杠杆</label>
                      <input type="number" min={1} max={125} value={leverage} onChange={e => setLeverage(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">手续费 %</label>
                      <input type="number" min={0} max={10} step={0.01} value={commission} onChange={e => setCommission(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" />
                    </div>
                    <div>
                      <label className="text-[10px] text-muted-foreground mb-1 block">滑点 %</label>
                      <input type="number" min={0} max={10} step={0.01} value={slippage} onChange={e => setSlippage(Number(e.target.value))}
                        className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs text-white outline-none focus:border-quant-gold" />
                    </div>
                  </div>
                  <button onClick={handleRunBacktest} disabled={running}
                    className="flex items-center gap-1.5 rounded bg-quant-gold px-4 py-1.5 text-xs font-medium text-black hover:opacity-90 disabled:opacity-50">
                    {running ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}
                    {running ? '运行中...' : '运行回测'}
                  </button>
                </SectionCard>

                {/* Error */}
                {backtestResult?.error && (
                  <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-400"><AlertCircle className="w-3.5 h-3.5" />{backtestResult.error}</div>
                )}

                {/* Results */}
                {backtestMetrics && (
                  <>
                    <div className="grid grid-cols-3 gap-3">
                      {backtestMetrics.map(m => <KPICard key={m.label} icon={<m.icon className="w-3.5 h-3.5 text-muted-foreground" />} label={m.label} value={m.value} trend={m.trend} />)}
                    </div>
                    {backtestResult?.trades?.length > 0 && (
                      <SectionCard title={`交易记录 (${backtestResult.trades.length}笔)`}>
                        <div className="overflow-x-auto max-h-52">
                          <table className="w-full text-[10px]">
                            <thead><tr className="text-muted-foreground text-left"><th className="px-2 py-1 font-medium">#</th><th className="px-2 py-1 font-medium">方向</th><th className="px-2 py-1 font-medium">价格</th><th className="px-2 py-1 font-medium">数量</th><th className="px-2 py-1 font-medium">盈亏</th></tr></thead>
                            <tbody>
                              {backtestResult.trades.map((t: any, i: number) => (
                                <tr key={i} className="border-t border-quant-border/40"><td className="px-2 py-1 text-muted-foreground">{i + 1}</td><td className="px-2 py-1">{t.side === 'buy' ? '买' : '卖'}</td><td className="px-2 py-1 font-mono">${formatCurrency(t.exit_price || t.entry_price)}</td><td className="px-2 py-1 font-mono">{t.qty}</td><td className={cn('px-2 py-1 font-mono font-bold', (t.pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>${t.pnl?.toFixed(2) || '-'}</td></tr>
                              ))}
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
  )
}
