/**
 * Indicator IDE — 量化丁格布局复刻
 * 左栏：代码编辑器（上）+ AI 协作面板（下），可拖拽分栏；
 * 右栏：图表窗口（工具条 + K 线图）。
 */
import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { marketApi, indicatorApi, strategyApi, communityApi } from '@/lib/api'
import { TRADING_INTERVALS } from '@/lib/constants'
import type { TickerSnapshot } from '@/types'
import { toast } from '@/lib/useToast'
import {
  CRAParamForm,
  type CRAParams,
  DEFAULT_CRA_PARAMS,
  craParamsToApiPayload,
} from '@/components/strategy/CRAParamForm'
import { KlineChart } from '@/components/charts/KlineChart'
import { CodeEditor } from '@/components/ide/CodeEditor'
import { ParamPanel } from '@/components/ide/ParamPanel'
import { ValidationBanner } from '@/components/ide/ValidationBanner'
import type { KLineBar } from '@/lib/technicalIndicators'
import {
  Play,
  Code,
  Zap,
  ChevronUp,
  ChevronDown,
  Maximize2,
  Minimize2,
  Plus,
  Trash2,
  Loader2,
  AlertCircle,
  X,
  Bot,
  Send,
  Copy,
  Upload,
  BookOpen,
  GitBranch,
  PauseCircle,
  Wand2,
  Eye,
  Check,
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
  id: string
  name: string
  shortName: string
  type: 'line' | 'band' | 'macd' | 'adx'
  params: Record<string, number>
  style?: { color?: string; lineWidth?: number }
  visible?: boolean
  instanceId?: string
}

interface SavedIndicator {
  id: number
  name: string
  code: string
  symbol?: string
  timeframe?: string
  is_encrypted?: number
  pricing_type?: string
}

interface IdeChatMessage {
  role: 'user' | 'bot'
  content: string
  status?: string
  candidate?: { code: string; applied: boolean; dismissed: boolean }
}

const AI_STATUS_TEXT: Record<string, string> = {
  generating: '正在生成指标代码...',
  validating: '验证代码...',
  auto_fixing: '自动修复中...',
}

const AI_GREETING: IdeChatMessage = {
  role: 'bot',
  content: '你好！描述你想要的指标，我来帮你生成代码。\n\n例如："做一个 MACD 金叉死叉带成交量过滤的指标"',
}

const QUICK_PROMPTS = [
  { label: '解释代码', prompt: '解释当前指标代码的逻辑' },
  { label: '调整参数', prompt: '帮我调整当前指标的参数' },
  { label: '增加信号', prompt: '给当前指标增加一个交易信号' },
  { label: '优化可视化', prompt: '优化当前指标的可视化效果' },
]

/* ── Main Page ───────────────────────────────────────────────────── */

export function IndicatorIDE() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  // Code state
  const [code, setCode] = useState(DEFAULT_INDICATOR_CODE)
  const [codeDirty, setCodeDirty] = useState(false)
  const [selectedIndicatorId, setSelectedIndicatorId] = useState<number | null>(null)
  // Parsed params / validation
  const [parsed, setParsed] = useState<ParseResult>(() => parseParamsFromCode(DEFAULT_INDICATOR_CODE))
  const [paramValues, setParamValues] = useState<Record<string, unknown>>({})
  const [validationHints, setValidationHints] = useState<ValidationHint[]>([])
  const [validating, setValidating] = useState(false)
  // Chart
  const [chartFullscreen, setChartFullscreen] = useState(false)
  const [activeIndicators, setActiveIndicators] = useState<IndicatorConfig[]>([])
  const [chartIndicatorRunning, setChartIndicatorRunning] = useState(false)
  // Left column split (code | AI), percentage of code section
  const [splitPct, setSplitPct] = useState(55)
  const leftColRef = useRef<HTMLDivElement>(null)
  // AI chat
  const [aiPrompt, setAiPrompt] = useState('')
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiStreamedCode, setAiStreamedCode] = useState('')
  const [aiPanelExpanded, setAiPanelExpanded] = useState(true)
  const [messages, setMessages] = useState<IdeChatMessage[]>([AI_GREETING])
  const [preview, setPreview] = useState<{ code: string; msgIdx: number } | null>(null)
  const chatScrollRef = useRef<HTMLDivElement>(null)
  // Experiment (auto-tune)
  const [experimentRunning, setExperimentRunning] = useState(false)
  const [experimentResult, setExperimentResult] = useState<Record<string, unknown> | null>(null)
  const [experimentPanelExpanded, setExperimentPanelExpanded] = useState(false)
  const [optimizer, setOptimizer] = useState<'de' | 'tpe'>('de')
  // Saved indicators
  const [indicators, setIndicators] = useState<SavedIndicator[]>([])
  // Chart signals from indicator execution
  const [chartSignals, setChartSignals] = useState<
    Array<{ timestamp: number; price: number; side: 'buy' | 'sell'; text: string; color: string }>
  >([])

  const reloadIndicators = useCallback(() => {
    indicatorApi
      .list()
      .then((res: unknown) => {
        const list = Array.isArray(res) ? res : (res as Record<string, unknown>)?.data || []
        setIndicators(Array.isArray(list) ? list : [])
      })
      .catch(() => {})
  }, [])

  /* ── Load saved indicators ── */
  useEffect(() => {
    reloadIndicators()
  }, [reloadIndicators])

  /* ── Select indicator ── */
  const selectIndicator = useCallback((ind: SavedIndicator) => {
    setSelectedIndicatorId(ind.id)
    setCode(ind.code)
    setCodeDirty(false)
    setValidationHints([])
  }, [])

  /* ── Auto-load indicator from URL ?id=xxx ── */
  useEffect(() => {
    const idParam = searchParams.get('id')
    if (!idParam || indicators.length === 0) return
    const id = Number(idParam)
    if (!id) return
    const ind = indicators.find((i) => i.id === id)
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
    setParamValues((prev) => {
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
    const raw = Array.isArray(klinesRaw) ? klinesRaw : ((klinesRaw as Record<string, unknown>)?.data ?? klinesRaw)
    const arr = Array.isArray(raw) ? raw : (raw as Record<string, unknown>)?.klines || []
    if (!Array.isArray(arr) || !arr.length) return []
    return arr.map((k: Record<string, unknown>) => ({
      timestamp: (k.time || k.timestamp || 0) as number,
      open: parseFloat(String(k.open)) || 0,
      high: parseFloat(String(k.high)) || 0,
      low: parseFloat(String(k.low)) || 0,
      close: parseFloat(String(k.close)) || 0,
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
        const dfJSON = klines.map((k) => ({
          time: k.timestamp,
          open: k.open,
          high: k.high,
          low: k.low,
          close: k.close,
          volume: k.volume,
        }))
        const res = (await indicatorApi.execute({
          code,
          params: paramValues,
          df_json: dfJSON,
        })) as unknown as {
          data?: {
            output?: {
              signals?: Array<{ type: string; text?: string; data?: (number | null)[]; color?: string }>
              plots?: Array<{ name: string; data: unknown[]; color?: string; overlay?: boolean }>
            }
          }
          success?: boolean
        }
        if (cancelled) return
        const output = res?.data?.output
        if (!output?.signals) {
          setChartSignals([])
          return
        }
        const signals: Array<{ timestamp: number; price: number; side: 'buy' | 'sell'; text: string; color: string }> =
          []
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
    return () => {
      cancelled = true
    }
  }, [chartIndicatorRunning, code, klines, paramValues])

  /* ── Snapshot ── */
  const { data: snapshotRaw } = useQuery({
    queryKey: ['ide-snap', symbol],
    queryFn: () => marketApi.snapshot(symbol).then((d) => d as TickerSnapshot),
    refetchInterval: 5000,
  })
  const snapshot = snapshotRaw as TickerSnapshot | undefined
  const lastPrice = snapshot?.price ? Number(snapshot.price) : 0
  const change24h = snapshot?.change_24h ? Number(snapshot.change_24h) : 0

  /* ── Validate ── */
  const handleValidate = useCallback(async () => {
    setValidating(true)
    try {
      const res = (await indicatorApi.validate(code)) as unknown as {
        data?: { hints?: ValidationHint[] }
        hints?: ValidationHint[]
      }
      const data = res?.data ?? res
      if (data?.hints) {
        setValidationHints(data.hints)
      } else {
        setValidationHints([])
      }
    } catch (e: unknown) {
      setValidationHints([
        {
          severity: 'error',
          code: 'VALIDATE_REQUEST_FAILED',
          params: { msg: e instanceof Error ? e.message : String(e) },
        },
      ])
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
      reloadIndicators()
    } catch {
      /* ignore */
    }
  }, [code, selectedIndicatorId, parsed, handleValidate, reloadIndicators])

  const handleDelete = useCallback(async () => {
    if (!selectedIndicatorId || !confirm('确认删除？')) return
    try {
      await indicatorApi.delete(selectedIndicatorId)
      setSelectedIndicatorId(null)
      setCode(DEFAULT_INDICATOR_CODE)
      setCodeDirty(false)
      setValidationHints([])
      reloadIndicators()
    } catch {
      /* ignore */
    }
  }, [selectedIndicatorId, reloadIndicators])

  const handleSaveAs = useCallback(async () => {
    try {
      await indicatorApi.saveAs({
        name: parsed.name + ' (副本)',
        description: parsed.description,
        code,
      })
      setCodeDirty(false)
      reloadIndicators()
    } catch {
      /* ignore */
    }
  }, [code, parsed, reloadIndicators])

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
      toast('success', '发布成功！')
    } catch (e: unknown) {
      toast('error', '发布失败: ' + (e instanceof Error ? e.message : '未知错误'))
    }
  }, [selectedIndicatorId, handleSave])

  /* ── AI Chat (SSE Streaming) — 丁格 candidate 流程 ── */
  const patchLastBot = useCallback((patch: Partial<IdeChatMessage>) => {
    setMessages((prev) => {
      const next = [...prev]
      for (let i = next.length - 1; i >= 0; i--) {
        if (next[i].role === 'bot') {
          next[i] = { ...next[i], ...patch }
          break
        }
      }
      return next
    })
  }, [])

  const handleSend = useCallback(
    async (presetPrompt?: string) => {
      const prompt = (presetPrompt ?? aiPrompt).trim()
      if (!prompt || aiGenerating) return
      setMessages((prev) => [...prev, { role: 'user', content: prompt }])
      setAiPrompt('')
      setMessages((prev) => [...prev, { role: 'bot', content: '', status: 'generating' }])
      setAiGenerating(true)
      setAiPanelExpanded(true)
      setAiStreamedCode('')
      setValidationHints([])
      let streamed = ''
      let replaced: string | null = null
      try {
        await indicatorApi.aiGenerateStream(
          { prompt, existingCode: code !== DEFAULT_INDICATOR_CODE ? code : '' },
          {
            onCodeChunk: (chunk) => {
              streamed += chunk
              setAiStreamedCode(streamed)
            },
            onStatus: (status) => {
              patchLastBot({
                status: ['auto_fixing_round_1', 'auto_fixing_round_2', 'auto_fixing_round_3'].includes(status)
                  ? 'auto_fixing'
                  : status,
              })
            },
            onValidation: (result) => {
              const r = result as unknown as { hints?: ValidationHint[] }
              if (r?.hints) {
                setValidationHints(r.hints)
              }
            },
            onCodeReplace: (newCode) => {
              // 丁格式：不直接覆盖编辑器，先进入 candidate 卡片
              replaced = newCode
              setAiStreamedCode(newCode)
            },
            onDebug: (info) => {
              console.warn('[AI Debug]', info)
            },
            onDone: () => {
              const cleaned = (replaced ?? streamed)
                .replace(/```python\n?/g, '')
                .replace(/```\n?/g, '')
                .trim()
              patchLastBot({
                status: 'completed',
                content: cleaned ? '指标代码已生成，请预览后应用。' : '已完成。',
                candidate: cleaned ? { code: cleaned, applied: false, dismissed: false } : undefined,
              })
            },
            onError: (err) => {
              setValidationHints([{ severity: 'error', code: 'AI_GENERATE_ERROR', params: { msg: err } }])
              patchLastBot({ status: 'error', content: `生成失败：${err}` })
            },
          }
        )
      } catch (e: unknown) {
        const msg = e instanceof Error ? e.message : '生成失败'
        setValidationHints([{ severity: 'error', code: 'AI_GENERATE_ERROR', params: { msg } }])
        patchLastBot({ status: 'error', content: `生成失败：${msg}` })
      } finally {
        setAiGenerating(false)
      }
    },
    [aiPrompt, code, aiGenerating, patchLastBot]
  )

  const applyCandidate = useCallback((idx: number) => {
    setMessages((prev) => {
      const msg = prev[idx]
      if (!msg?.candidate || msg.candidate.applied) return prev
      setCode(msg.candidate.code)
      setCodeDirty(true)
      setValidationHints([])
      toast('success', 'AI 代码已应用到编辑器')
      return prev.map((m, i) =>
        i === idx && m.candidate ? { ...m, candidate: { ...m.candidate, applied: true } } : m
      )
    })
  }, [])

  const dismissCandidate = useCallback((idx: number) => {
    setMessages((prev) =>
      prev.map((m, i) => (i === idx && m.candidate ? { ...m, candidate: { ...m.candidate, dismissed: true } } : m))
    )
  }, [])

  const clearConversation = useCallback(() => {
    if (aiGenerating) return
    setMessages([AI_GREETING])
  }, [aiGenerating])

  /* ── Chat auto scroll ── */
  useEffect(() => {
    chatScrollRef.current?.scrollTo({ top: chatScrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages, aiStreamedCode, aiGenerating])

  /* ── Split resizer ── */
  const startResize = useCallback(
    (e: React.PointerEvent) => {
      e.preventDefault()
      const startY = e.clientY
      const startPct = splitPct
      document.body.style.cursor = 'row-resize'
      const move = (ev: PointerEvent) => {
        const container = leftColRef.current
        if (!container) return
        const h = container.clientHeight || 1
        const pct = startPct + ((ev.clientY - startY) / h) * 100
        setSplitPct(Math.min(75, Math.max(30, pct)))
      }
      const up = () => {
        window.removeEventListener('pointermove', move)
        window.removeEventListener('pointerup', up)
        document.body.style.cursor = ''
      }
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
    },
    [splitPct]
  )

  /* ── Experiment (Auto-tune) ── */
  const handleRunExperiment = useCallback(async () => {
    if (parsed.params.length === 0) return
    setExperimentRunning(true)
    setExperimentResult(null)
    try {
      const payload: Record<string, unknown> = {
        code,
        symbol,
        interval,
        optimizer,
        oos_ratio: 0.3,
        backtest_config: {
          initial_balance: 10000,
          commission: 0.0005,
          slippage: 0.0001,
        },
      }
      const res = await indicatorApi.experiment.run(payload)
      setExperimentResult(res?.data || res)
      const best = res?.data?.best_params || res?.best_params
      if (best) {
        setParamValues((prev) => ({ ...prev, ...best }))
      }
    } catch (e: unknown) {
      setExperimentResult({ error: e instanceof Error ? e.message : '实验失败' })
    } finally {
      setExperimentRunning(false)
    }
  }, [code, symbol, interval, optimizer, parsed.params.length])

  /* ── Create Strategy from Indicator ── */
  const [showCreateStrategy, setShowCreateStrategy] = useState(false)
  const [stratBase, setStratBase] = useState({
    name: '',
    symbol: 'BTCUSDT',
    interval: '1h',
    leverage: 5,
    direction: 'long' as 'long' | 'short' | 'dual',
  })
  const [stratCra, setStratCra] = useState<CRAParams>({
    ...DEFAULT_CRA_PARAMS,
    tradeCountMode: 'cycle',
  })

  const handleCreateStrategyFromIndicator = useCallback(async () => {
    try {
      await strategyApi.create({
        name: stratBase.name || `${parsed.name}策略`,
        symbol: stratBase.symbol,
        timeframe: stratBase.interval,
        trade_direction: stratBase.direction,
        market_type: 'swap',
        strategy_type: 'custom_indicator',
        strategy_code: code,
        status: 'stopped',
        ...craParamsToApiPayload(stratCra),
      })
      setShowCreateStrategy(false)
      toast('success', '策略创建成功！请到策略管理页面启动。')
    } catch (e: unknown) {
      toast('error', '创建策略失败: ' + (e instanceof Error ? e.message : String(e)))
    }
  }, [stratBase, stratCra, code, parsed.name])

  /* ═══════════════════════════════════════════════════════════════ */
  /*  Render — 左栏：代码 + AI ｜ 右栏：图表                            */
  /* ═══════════════════════════════════════════════════════════════ */

  const isUp = change24h >= 0

  return (
    <>
      <div className="h-full flex bg-quant-bg relative">
        {/* ═══════════════════════════════════════════════════════════
          LEFT: Code editor (top) + AI panel (bottom)
      ═══════════════════════════════════════════════════════════ */}
        <div
          ref={leftColRef}
          className="w-[480px] shrink-0 flex flex-col border-r border-quant-border bg-quant-bg-secondary min-h-0"
        >
          {/* ── Code section ── */}
          <div className="flex flex-col min-h-0 overflow-hidden" style={{ flexBasis: `${splitPct}%` }}>
            {/* Toolbar */}
            <div className="flex items-center justify-between gap-1 px-2 py-1.5 border-b border-quant-border shrink-0 flex-wrap">
              <div className="flex items-center gap-1.5">
                <Code className="w-3.5 h-3.5 text-muted-foreground" />
                {codeDirty && (
                  <span className="px-1.5 py-0.5 rounded text-[9px] font-medium bg-amber-500/10 text-amber-400">
                    已修改
                  </span>
                )}
              </div>
              <div className="flex items-center gap-0.5">
                <select
                  value=""
                  onChange={(e) => {
                    const tmpl = INDICATOR_TEMPLATES.find((t) => t.key === e.target.value)
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
                  {INDICATOR_TEMPLATES.map((t) => (
                    <option key={t.key} value={t.key}>
                      {t.label}
                    </option>
                  ))}
                </select>
                <button
                  onClick={() => {
                    setCode(DEFAULT_INDICATOR_CODE)
                    setSelectedIndicatorId(null)
                    setCodeDirty(false)
                    setValidationHints([])
                  }}
                  className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
                  title="新建"
                  aria-label="新建"
                >
                  <Plus className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={handleSaveAs}
                  className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
                  title="另存为"
                  aria-label="另存为"
                >
                  <Copy className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={handleDelete}
                  disabled={!selectedIndicatorId}
                  className="p-1.5 rounded text-muted-foreground hover:text-quant-red hover:bg-white/5 disabled:opacity-30"
                  title="删除"
                  aria-label="删除"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={handleValidate}
                  disabled={validating}
                  className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-white/5 disabled:opacity-30"
                  title="验证代码"
                  aria-label="验证代码"
                >
                  {validating ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    <AlertCircle className="w-3.5 h-3.5" />
                  )}
                </button>
                <button
                  onClick={handlePublish}
                  className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
                  title="发布到社区"
                  aria-label="发布到社区"
                >
                  <Upload className="w-3.5 h-3.5" />
                </button>
                <button
                  onClick={() => setChartIndicatorRunning(!chartIndicatorRunning)}
                  className={cn(
                    'p-1.5 rounded',
                    chartIndicatorRunning
                      ? 'text-quant-green bg-quant-green/10'
                      : 'text-muted-foreground hover:text-foreground hover:bg-white/5'
                  )}
                  title={chartIndicatorRunning ? '停止图表运行' : '在图表上运行'}
                  aria-label={chartIndicatorRunning ? '停止图表运行' : '在图表上运行'}
                >
                  {chartIndicatorRunning ? <PauseCircle className="w-3.5 h-3.5" /> : <Play className="w-3.5 h-3.5" />}
                </button>
                <button
                  onClick={handleSave}
                  disabled={!codeDirty}
                  className="ml-1 px-2.5 h-7 rounded-lg bg-quant-gold text-black text-[11px] font-medium hover:opacity-90 disabled:opacity-30 transition-opacity"
                >
                  保存
                </button>
              </div>
            </div>

            {/* Guide bar */}
            <div className="flex items-center gap-1.5 px-2 py-1 text-[10px] text-muted-foreground border-b border-quant-border bg-quant-bg-tertiary shrink-0">
              <BookOpen className="w-3 h-3" />
              <span>开发指南</span>
              <span className="ml-auto flex items-center gap-2">
                <span className={validationHints.length > 0 ? 'text-amber-400' : 'text-quant-green'}>
                  {validationHints.length > 0 ? `${validationHints.length} 条提示` : '检查通过'}
                </span>
                <button onClick={handleValidate} className="text-muted-foreground hover:text-foreground">
                  重新检查
                </button>
                <button
                  onClick={() => window.open('/docs/strategy-guide', '_blank')}
                  className="text-quant-gold hover:underline"
                >
                  查看文档 →
                </button>
              </span>
            </div>

            {/* Validation hints */}
            {validationHints.length > 0 && (
              <div className="px-2 py-1.5 border-b border-quant-border max-h-20 overflow-y-auto shrink-0">
                <ValidationBanner hints={validationHints} />
              </div>
            )}

            {/* Editor + AI overlay */}
            <div className="relative flex-1 min-h-0">
              <CodeEditor
                value={code}
                onChange={(v) => {
                  setCode(v)
                  setCodeDirty(true)
                  setValidationHints([])
                }}
                theme="dark"
                placeholder="输入 Python 指标代码..."
              />
              {aiGenerating && (
                <div className="absolute inset-0 z-10 bg-quant-bg/80 backdrop-blur-sm flex flex-col items-center justify-center gap-3">
                  <Loader2 className="w-6 h-6 text-quant-gold animate-spin" />
                  <span className="text-xs text-muted-foreground">AI 生成中</span>
                  <div className="flex gap-1">
                    {[0, 1, 2].map((i) => (
                      <span
                        key={i}
                        className="w-1.5 h-1.5 rounded-full bg-quant-gold animate-bounce"
                        style={{ animationDelay: `${i * 0.15}s` }}
                      />
                    ))}
                  </div>
                </div>
              )}
            </div>

            {/* Params + auto-tune */}
            {parsed.params.length > 0 && (
              <div className="px-2 py-1.5 border-t border-quant-border max-h-28 overflow-y-auto shrink-0">
                <ParamPanel
                  params={parsed.params}
                  values={paramValues}
                  onChange={(name, value) => setParamValues((prev) => ({ ...prev, [name]: value }))}
                />
              </div>
            )}
            {parsed.params.length > 0 && (
              <div className="px-2 py-1.5 border-t border-quant-border shrink-0">
                <button
                  onClick={() => setExperimentPanelExpanded(!experimentPanelExpanded)}
                  className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground transition-colors w-full"
                >
                  <Wand2 className="w-3 h-3 text-quant-gold" />
                  自动调参 (DE / TPE)
                  {experimentPanelExpanded ? (
                    <ChevronUp className="w-3 h-3 ml-auto" />
                  ) : (
                    <ChevronDown className="w-3 h-3 ml-auto" />
                  )}
                </button>
                {experimentPanelExpanded && (
                  <div className="mt-2 space-y-2 pb-1">
                    <div className="flex items-center gap-2">
                      <select
                        value={optimizer}
                        onChange={(e) => setOptimizer(e.target.value as 'de' | 'tpe')}
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
                        {experimentRunning ? (
                          <Loader2 className="w-3 h-3 animate-spin" />
                        ) : (
                          <Wand2 className="w-3 h-3" />
                        )}
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
          </div>

          {/* ── Resizer ── */}
          <div
            className="h-2 shrink-0 bg-quant-border/40 hover:bg-quant-gold/40 cursor-row-resize flex items-center justify-center transition-colors"
            onPointerDown={startResize}
            title="拖拽调整分栏"
          >
            <span className="w-8 h-0.5 rounded bg-muted-foreground/40" />
          </div>

          {/* ── AI 协作 panel ── */}
          <div className="flex-1 min-h-[220px] flex flex-col bg-quant-bg min-h-0">
            <button
              onClick={() => setAiPanelExpanded(!aiPanelExpanded)}
              className="flex items-center gap-2 px-3 py-2 border-b border-quant-border bg-quant-bg-secondary shrink-0 w-full"
            >
              <span className="w-6 h-6 rounded-lg bg-quant-gold/20 flex items-center justify-center shrink-0">
                <Bot className="w-3.5 h-3.5 text-quant-gold" />
              </span>
              <span className="text-xs font-semibold">AI 协作</span>
              {messages.length > 1 && (
                <span
                  role="button"
                  tabIndex={0}
                  onClick={(e) => {
                    e.stopPropagation()
                    clearConversation()
                  }}
                  className="text-[10px] text-muted-foreground hover:text-foreground ml-2"
                >
                  清空对话
                </span>
              )}
              {aiPanelExpanded ? (
                <ChevronUp className="w-3.5 h-3.5 ml-auto text-muted-foreground" />
              ) : (
                <ChevronDown className="w-3.5 h-3.5 ml-auto text-muted-foreground" />
              )}
            </button>
            {aiPanelExpanded && (
              <>
                {/* Conversation */}
                <div ref={chatScrollRef} className="flex-1 overflow-y-auto p-3 space-y-3 min-h-0">
                  {messages.length <= 1 && !aiGenerating ? (
                    <div className="h-full flex flex-col items-center justify-center gap-3 py-6">
                      <Bot className="w-8 h-8 text-muted-foreground/40" />
                      <div className="text-xs font-medium">AI 指标助手</div>
                      <div className="text-[10px] text-muted-foreground">描述需求生成指标代码，或从快捷指令开始</div>
                      <div className="grid grid-cols-2 gap-2 w-full max-w-[280px]">
                        {QUICK_PROMPTS.map((q) => (
                          <button
                            key={q.label}
                            onClick={() => handleSend(q.prompt)}
                            className="px-2 py-1.5 rounded-full text-[10px] border border-quant-border text-muted-foreground hover:text-quant-gold hover:border-quant-gold/40 transition-colors"
                          >
                            {q.label}
                          </button>
                        ))}
                      </div>
                    </div>
                  ) : (
                    messages.map((msg, i) => (
                      <div key={i} className={cn('flex flex-col', msg.role === 'user' ? 'items-end' : 'items-start')}>
                        <div className="text-[9px] text-muted-foreground mb-0.5">{msg.role === 'user' ? '你' : 'AI'}</div>
                        <div
                          className={cn(
                            'max-w-[90%] rounded-lg px-3 py-2 text-[11px] leading-relaxed whitespace-pre-wrap border',
                            msg.role === 'user'
                              ? 'bg-quant-gold/10 border-quant-gold/25 text-foreground'
                              : 'bg-quant-bg-secondary border-quant-border'
                          )}
                        >
                          {msg.content ||
                            (msg.status ? AI_STATUS_TEXT[msg.status] ?? '处理中...' : '')}
                          {msg.role === 'bot' && msg.status === 'generating' && aiStreamedCode && (
                            <pre className="mt-2 pt-2 border-t border-quant-border/50 text-[10px] text-quant-green/80 font-mono whitespace-pre-wrap max-h-36 overflow-y-auto">
                              {aiStreamedCode.slice(-400)}
                            </pre>
                          )}
                        </div>
                        {/* AI candidate 卡片 */}
                        {msg.role === 'bot' && msg.candidate && !msg.candidate.dismissed && (
                          <div
                            className={cn(
                              'mt-1.5 w-[90%] rounded-lg border p-2.5',
                              msg.candidate.applied
                                ? 'border-quant-green/30 bg-quant-green/5'
                                : 'border-quant-gold/30 bg-quant-gold/5'
                            )}
                          >
                            <div className="flex items-center justify-between text-[10px] mb-2">
                              <span
                                className={cn(
                                  'flex items-center gap-1',
                                  msg.candidate.applied ? 'text-quant-green' : 'text-quant-gold'
                                )}
                              >
                                {msg.candidate.applied && <Check className="w-3 h-3" />}
                                {msg.candidate.applied ? '已应用' : '待确认代码'}
                              </span>
                              <span className="text-muted-foreground">
                                {msg.candidate.code.split('\n').length} 行 ·{' '}
                                {validationHints.length > 0 ? `${validationHints.length} 条校验提示` : '校验通过'}
                              </span>
                            </div>
                            {!msg.candidate.applied && (
                              <div className="flex items-center gap-2">
                                <button
                                  onClick={() => setPreview({ code: msg.candidate!.code, msgIdx: i })}
                                  className="flex items-center gap-1 px-2 py-1 rounded border border-quant-border text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                                >
                                  <Eye className="w-3 h-3" /> 预览
                                </button>
                                <button
                                  onClick={() => applyCandidate(i)}
                                  className="flex items-center gap-1 px-2 py-1 rounded bg-quant-gold text-black text-[10px] font-medium hover:opacity-90 transition-opacity"
                                >
                                  <Check className="w-3 h-3" /> 应用
                                </button>
                                <button
                                  onClick={() => dismissCandidate(i)}
                                  className="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                                >
                                  放弃
                                </button>
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    ))
                  )}
                  {aiGenerating && (
                    <div className="flex flex-col items-start">
                      <div className="text-[9px] text-muted-foreground mb-0.5">AI</div>
                      <div className="rounded-lg px-3 py-2 text-[11px] bg-quant-bg-secondary border border-quant-border flex items-center gap-2">
                        <Loader2 className="w-3 h-3 animate-spin text-quant-gold" /> AI 思考中
                      </div>
                    </div>
                  )}
                </div>
                {/* Composer */}
                <div className="p-3 border-t border-quant-border shrink-0">
                  <textarea
                    value={aiPrompt}
                    onChange={(e) => setAiPrompt(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) {
                        e.preventDefault()
                        handleSend()
                      }
                    }}
                    rows={3}
                    placeholder="描述你想要的指标，例如：MACD 金叉死叉带成交量过滤..."
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold resize-none"
                  />
                  <div className="flex items-center justify-between mt-2">
                    <span className="text-[9px] text-muted-foreground">Enter 发送 · Shift+Enter 换行</span>
                    <button
                      onClick={() => handleSend()}
                      disabled={aiGenerating || !aiPrompt.trim()}
                      className="flex items-center gap-1.5 px-4 h-8 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 disabled:opacity-50 transition-opacity"
                    >
                      {aiGenerating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Send className="w-3.5 h-3.5" />}
                      发送
                    </button>
                  </div>
                </div>
              </>
            )}
          </div>
        </div>

        {/* ═══════════════════════════════════════════════════════════
          RIGHT: Chart window
      ═══════════════════════════════════════════════════════════ */}
        <div className="flex-1 flex flex-col min-w-0">
          <div className="flex items-center justify-between px-3 py-2 border-b border-quant-border bg-quant-bg-secondary shrink-0">
            <span className="text-xs font-semibold">图表窗口</span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setShowCreateStrategy(true)}
                className="flex items-center gap-1.5 px-3 h-7 rounded-lg bg-quant-gold text-black text-[11px] font-medium hover:opacity-90 transition-opacity"
              >
                <GitBranch className="w-3.5 h-3.5" /> 转换为策略
              </button>
              <button
                onClick={() => setChartFullscreen(!chartFullscreen)}
                className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
                title={chartFullscreen ? '退出全屏' : '图表全屏'}
                aria-label={chartFullscreen ? '退出全屏' : '图表全屏'}
              >
                {chartFullscreen ? <Minimize2 className="w-3.5 h-3.5" /> : <Maximize2 className="w-3.5 h-3.5" />}
              </button>
            </div>
          </div>
          <div className="flex items-center gap-2 px-3 py-2 border-b border-quant-border bg-quant-bg-secondary shrink-0 flex-wrap">
            {/* Saved indicator selector */}
            <select
              value={selectedIndicatorId ?? ''}
              onChange={(e) => {
                const id = Number(e.target.value)
                const ind = indicators.find((i) => i.id === id)
                if (ind) selectIndicator(ind)
              }}
              className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs text-white outline-none focus:border-quant-gold max-w-[150px] truncate"
            >
              <option value="">选择指标...</option>
              {indicators.map((ind) => (
                <option key={ind.id} value={ind.id}>
                  {ind.name}
                </option>
              ))}
            </select>
            {/* Symbol */}
            <select
              value={symbol}
              onChange={(e) => setSymbol(e.target.value)}
              className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs text-white outline-none focus:border-quant-gold"
            >
              {['BTCUSDT', 'ETHUSDT', 'BNBUSDT', 'SOLUSDT', 'ADAUSDT', 'DOGEUSDT'].map((s) => (
                <option key={s} value={s}>
                  {s.replace('USDT', '/USDT')}
                </option>
              ))}
            </select>
            {/* Timeframe */}
            <div className="flex rounded bg-quant-bg-tertiary p-0.5">
              {TRADING_INTERVALS.map((int) => (
                <button
                  key={int}
                  onClick={() => setInterval(int)}
                  className={cn(
                    'px-1.5 py-0.5 rounded text-[10px] font-medium transition-colors',
                    interval === int
                      ? 'bg-quant-gold/10 text-quant-gold'
                      : 'text-muted-foreground hover:text-foreground'
                  )}
                >
                  {int}
                </button>
              ))}
            </div>
            {/* Price */}
            <span className="ml-auto flex items-center gap-2 text-xs">
              <span className="font-mono font-bold">{symbol.replace('USDT', '/USDT')}</span>
              <span className={cn('font-mono font-bold', isUp ? 'text-quant-green' : 'text-quant-red')}>
                ${lastPrice.toFixed(2)}
              </span>
              <span className={cn('font-mono', isUp ? 'text-quant-green' : 'text-quant-red')}>
                {change24h >= 0 ? '+' : ''}
                {change24h.toFixed(2)}%
              </span>
              {chartIndicatorRunning && (
                <span className="text-[10px] text-quant-gold bg-quant-gold/10 px-1.5 py-0 rounded">指标运行中</span>
              )}
            </span>
          </div>
          <div className="flex-1 min-h-0 p-2">
            <div className="h-full rounded-lg overflow-hidden border border-quant-border">
              <KlineChart
                data={klines}
                loading={klLoading}
                signals={chartSignals}
                activeIndicators={activeIndicators}
                onActiveIndicatorsChange={setActiveIndicators}
                theme="dark"
              />
            </div>
          </div>
        </div>

        {/* ── Chart fullscreen overlay ── */}
        {chartFullscreen && (
          <div className="absolute inset-0 z-20 bg-quant-bg flex flex-col">
            <div className="flex items-center justify-between px-3 py-2 border-b border-quant-border bg-quant-bg-secondary shrink-0">
              <div className="flex items-center gap-4">
                <span className="font-bold text-sm">{symbol.replace('USDT', '/USDT')}</span>
                <span className={cn('font-mono text-sm font-bold', isUp ? 'text-quant-green' : 'text-quant-red')}>
                  ${lastPrice.toFixed(2)}
                </span>
                <span className={cn('text-xs font-mono', isUp ? 'text-quant-green' : 'text-quant-red')}>
                  {change24h >= 0 ? '+' : ''}
                  {change24h.toFixed(2)}%
                </span>
                {chartIndicatorRunning && (
                  <span className="text-[10px] text-quant-gold bg-quant-gold/10 px-1.5 py-0 rounded">指标运行中</span>
                )}
              </div>
              <button
                onClick={() => setChartFullscreen(false)}
                className="p-1.5 rounded text-muted-foreground hover:text-foreground hover:bg-white/5"
                title="退出全屏"
                aria-label="退出全屏"
              >
                <Minimize2 className="w-4 h-4" />
              </button>
            </div>
            <div className="flex-1 min-h-0 p-2">
              <div className="h-full rounded-lg overflow-hidden border border-quant-border">
                <KlineChart
                  data={klines}
                  loading={klLoading}
                  signals={chartSignals}
                  activeIndicators={activeIndicators}
                  onActiveIndicatorsChange={setActiveIndicators}
                  theme="dark"
                />
              </div>
            </div>
          </div>
        )}
      </div>

      {/* ── AI code preview modal ── */}
      {preview && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
        >
          <div className="w-full max-w-3xl max-h-[80vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
            <div className="flex items-center justify-between px-5 py-3 border-b border-quant-border shrink-0">
              <h3 className="text-sm font-bold flex items-center gap-2">
                <Eye className="w-4 h-4 text-quant-gold" />
                预览 AI 代码
              </h3>
              <button
                onClick={() => setPreview(null)}
                aria-label="关闭"
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <pre className="flex-1 overflow-auto p-4 text-[11px] font-mono text-quant-green/90 bg-quant-bg m-3 rounded-xl border border-quant-border">
              {preview.code}
            </pre>
            <div className="flex items-center justify-end gap-2 px-5 py-3 border-t border-quant-border shrink-0">
              <button
                onClick={() => setPreview(null)}
                className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
              >
                关闭
              </button>
              <button
                onClick={() => {
                  applyCandidate(preview.msgIdx)
                  setPreview(null)
                }}
                className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity flex items-center gap-1.5"
              >
                <Check className="w-3.5 h-3.5" /> 应用
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ── Create Strategy from Indicator Modal ── */}
      {showCreateStrategy && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
        >
          <div className="w-full max-w-2xl max-h-[85vh] flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
            <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
              <h3 className="text-sm font-bold flex items-center gap-2">
                <GitBranch className="w-4 h-4 text-quant-gold" />
                从指标创建策略
              </h3>
              <button
                onClick={() => setShowCreateStrategy(false)}
                aria-label="关闭"
                className="text-muted-foreground hover:text-foreground"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto p-6 space-y-4">
              <div className="text-xs text-muted-foreground mb-2">
                基于当前指标 <span className="text-quant-gold font-medium">{parsed.name}</span> 创建量化策略
              </div>

              {/* 基础配置 */}
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">策略名称</label>
                  <input
                    value={stratBase.name}
                    onChange={(e) => setStratBase({ ...stratBase, name: e.target.value })}
                    placeholder={`${parsed.name}策略`}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
                  />
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">交易对</label>
                  <input
                    value={stratBase.symbol}
                    onChange={(e) => setStratBase({ ...stratBase, symbol: e.target.value.toUpperCase() })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
                  />
                </div>
              </div>
              <div className="grid grid-cols-3 gap-4">
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">K线周期</label>
                  <select
                    value={stratBase.interval}
                    onChange={(e) => setStratBase({ ...stratBase, interval: e.target.value })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
                  >
                    {TRADING_INTERVALS.map((i) => (
                      <option key={i} value={i}>
                        {i}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">杠杆</label>
                  <input
                    type="number"
                    min={1}
                    max={150}
                    value={stratBase.leverage}
                    onChange={(e) => setStratBase({ ...stratBase, leverage: Number(e.target.value) })}
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
                  />
                </div>
                <div>
                  <label className="text-[11px] text-muted-foreground mb-1.5 block">方向</label>
                  <select
                    value={stratBase.direction}
                    onChange={(e) =>
                      setStratBase({ ...stratBase, direction: e.target.value as typeof stratBase.direction })
                    }
                    className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
                  >
                    <option value="long">做多</option>
                    <option value="short">做空</option>
                    <option value="dual">双向</option>
                  </select>
                </div>
              </div>

              {/* CRA 参数 */}
              <CRAParamForm value={stratCra} onChange={setStratCra} market="contract" />
            </div>
            <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-quant-border shrink-0">
              <button
                onClick={() => setShowCreateStrategy(false)}
                className="px-4 py-2 rounded-lg border border-quant-border text-xs hover:bg-quant-hover transition-colors"
              >
                取消
              </button>
              <button
                onClick={handleCreateStrategyFromIndicator}
                className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity flex items-center gap-1.5"
              >
                <Zap className="w-3.5 h-3.5" /> 创建策略
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
