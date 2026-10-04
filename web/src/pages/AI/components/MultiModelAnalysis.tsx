import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { aiApi, aiRobotApi, type AIAsyncAnalysisResult } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'
import type { AIAnalysisResult } from '@/types'
import { BrainCircuit, Loader2, X } from 'lucide-react'

const INTERVALS = ['15m', '1h', '4h', '1d']

/** 异步多模型共识分析结果 → 页面现有 AIAnalysisResult 渲染契约。 */
export function mapAsyncToAnalysisResult(data: AIAsyncAnalysisResult): AIAnalysisResult {
  const consensus = data.consensus
  const signal = (consensus?.signal || 'neutral') as AIAnalysisResult['consensus']
  const analyses = (data.results ?? []).map((r) => ({
    model: r.model,
    name: r.model,
    sentiment: (r.signal === 'bullish' || r.signal === 'bearish' ? r.signal : 'neutral') as 'bullish' | 'bearish' | 'neutral',
    analysis: r.reasoning || '',
    content: r.reasoning || '',
    confidence: r.confidence,
  }))
  return { symbol: data.symbol, consensus: signal, analyses }
}

/**
 * 多模型共识分析弹层（POST /analysis/start → GET /analysis/result 2s 轮询）。
 * 完成后把结果映射进页面主渲染区（AnalysisResultView 复用），并 toast 共识/一致度。
 */
export function MultiModelAnalysis({
  open,
  symbol,
  onClose,
  onResult,
}: {
  open: boolean
  symbol: string
  onClose: () => void
  onResult: (result: AIAnalysisResult) => void
}) {
  const { t } = useI18n()
  const [selected, setSelected] = useState<string[]>([])
  const [interval, setIntervalVal] = useState('1h')
  const [taskId, setTaskId] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [timedOut, setTimedOut] = useState(false)

  const modelsQuery = useQuery({
    queryKey: ['ai-robot-models'],
    queryFn: () => aiRobotApi.getModels(),
    enabled: open,
    retry: false,
  })
  const models = useMemo(() => modelsQuery.data ?? [], [modelsQuery.data])

  // 默认全选已配置模型
  useEffect(() => {
    if (open && models.length > 0) setSelected((prev) => (prev.length > 0 ? prev : models))
  }, [open, models])

  const resultQuery = useQuery({
    queryKey: ['ai-async-analysis', taskId],
    queryFn: () => aiApi.analysisResult(taskId!),
    enabled: !!taskId,
    refetchInterval: (q) => (q.state.data?.status === 'completed' ? false : 2000),
    retry: false,
  })

  // 60s 超时保护
  useEffect(() => {
    if (!taskId) return
    setTimedOut(false)
    const timer = setTimeout(() => setTimedOut(true), 60000)
    return () => clearTimeout(timer)
  }, [taskId])

  const status = resultQuery.data?.status

  useEffect(() => {
    if (status !== 'completed' || !resultQuery.data) return
    const mapped = mapAsyncToAnalysisResult(resultQuery.data)
    onResult(mapped)
    const c = resultQuery.data.consensus
    if (c) {
      const signalKey = c.signal === 'bullish' ? 'ai.async.bullish' : c.signal === 'bearish' ? 'ai.async.bearish' : 'ai.async.neutral'
      toast(
        'success',
        `${t('ai.async.consensus')}: ${t(signalKey)} · ${t('ai.async.agreement').replace('{pct}', c.agreement.toFixed(0))}`
      )
    }
    setTaskId(null)
    onClose()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status])

  const toggleModel = (m: string) =>
    setSelected((prev) => (prev.includes(m) ? prev.filter((x) => x !== m) : [...prev, m]))

  const handleStart = async () => {
    if (selected.length === 0) {
      toast('warning', t('ai.async.selectOne'))
      return
    }
    setStarting(true)
    try {
      const d = await aiApi.analysisStart({ symbol, interval, enabled_models: selected })
      setTaskId(d.task_id)
    } catch (e: unknown) {
      toast('error', e instanceof Error ? e.message : t('ai.async.startFailed'))
    } finally {
      setStarting(false)
    }
  }

  if (!open) return null
  const processing = !!taskId && !timedOut

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={onClose}>
      <div
        className="w-full max-w-md rounded-xl border border-quant-border bg-quant-card p-5 space-y-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <BrainCircuit className="w-4 h-4 text-quant-gold" />
            <span className="text-sm font-semibold">{t('ai.async.title')}</span>
          </div>
          <button onClick={onClose} aria-label="close" className="text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>
        <p className="text-xs text-muted-foreground">{t('ai.async.desc')}</p>

        <div>
          <div className="mb-1.5 text-[11px] text-muted-foreground">{t('ai.async.models')}</div>
          {modelsQuery.isLoading ? (
            <div className="text-xs text-muted-foreground">...</div>
          ) : models.length === 0 ? (
            <div className="text-xs text-muted-foreground">{t('ai.async.noModels')}</div>
          ) : (
            <div className="flex flex-wrap gap-2">
              {models.map((m) => (
                <button
                  key={m}
                  onClick={() => toggleModel(m)}
                  className={`rounded-md border px-2.5 py-1 text-xs font-medium transition-colors ${
                    selected.includes(m)
                      ? 'border-quant-gold/50 bg-quant-gold/10 text-quant-gold'
                      : 'border-quant-border text-muted-foreground hover:text-foreground'
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="flex items-center gap-3">
          <span className="text-[11px] text-muted-foreground">{t('ai.async.interval')}</span>
          <select
            value={interval}
            onChange={(e) => setIntervalVal(e.target.value)}
            className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-xs text-foreground outline-none focus:border-quant-gold"
          >
            {INTERVALS.map((i) => (
              <option key={i} value={i}>
                {i}
              </option>
            ))}
          </select>
          <span className="text-[11px] font-mono text-muted-foreground ml-auto">{symbol}</span>
        </div>

        <button
          onClick={handleStart}
          disabled={starting || processing || models.length === 0}
          className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-quant-gold px-4 py-2 text-xs font-semibold text-white hover:opacity-90 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {starting || processing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <BrainCircuit className="w-3.5 h-3.5" />}
          {processing ? t('ai.async.processing') : starting ? t('ai.async.starting') : t('ai.async.start')}
        </button>
        {timedOut && <div className="text-xs text-amber-400">{t('ai.async.timeout')}</div>}
      </div>
    </div>
  )
}
