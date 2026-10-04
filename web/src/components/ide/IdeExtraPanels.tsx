import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { indicatorApi, type PythonRunSignal } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'
import { ChevronDown, ChevronUp, FlaskConical, Loader2, Play, RefreshCw, Terminal } from 'lucide-react'

function fmtTime(ms: number): string {
  if (!ms) return '-'
  const d = new Date(ms > 1e12 ? ms : ms * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/**
 * Python 沙箱运行（POST /strategies-python/run）：把 IDE 当前代码 + K 线送入
 * Python 策略引擎执行，展示返回信号。引擎未部署时后端返回 503，错误如实透出。
 */
export function PythonSandboxPanel({
  code,
  symbol,
  interval,
  params,
  bars,
}: {
  code: string
  symbol: string
  interval: string
  params: Record<string, unknown>
  bars: { time: number; open: number; high: number; low: number; close: number; volume: number }[]
}) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState(false)
  const [mode, setMode] = useState<'indicator' | 'script'>('indicator')
  const [running, setRunning] = useState(false)
  const [signals, setSignals] = useState<PythonRunSignal[] | null>(null)

  const handleRun = async () => {
    setRunning(true)
    try {
      const res = await indicatorApi.runPython({
        mode,
        code,
        symbol,
        interval,
        params,
        bars: bars.slice(-500),
      })
      setSignals(res?.signals ?? [])
    } catch (e: unknown) {
      toast('error', `${t('ide.sandbox.failed')}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="px-2 py-1.5 border-t border-quant-border shrink-0">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground transition-colors w-full"
      >
        <Terminal className="w-3 h-3 text-quant-gold" />
        {t('ide.sandbox.title')}
        {expanded ? <ChevronUp className="w-3 h-3 ml-auto" /> : <ChevronDown className="w-3 h-3 ml-auto" />}
      </button>
      {expanded && (
        <div className="mt-2 space-y-2 pb-1">
          <p className="text-[10px] text-muted-foreground">{t('ide.sandbox.desc')}</p>
          <div className="flex items-center gap-2">
            <select
              value={mode}
              onChange={(e) => setMode(e.target.value as 'indicator' | 'script')}
              className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-[11px] text-foreground outline-none focus:border-quant-gold"
              aria-label={t('ide.sandbox.mode')}
            >
              <option value="indicator">{t('ide.sandbox.modeIndicator')}</option>
              <option value="script">{t('ide.sandbox.modeScript')}</option>
            </select>
            <button
              onClick={handleRun}
              disabled={running || !code.trim()}
              className="flex items-center gap-1 rounded bg-quant-gold/20 px-2 py-1 text-[11px] font-medium text-quant-gold hover:bg-quant-gold/30 disabled:opacity-50"
            >
              {running ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3" />}
              {running ? t('ide.sandbox.running') : t('ide.sandbox.run')}
            </button>
            {signals && (
              <span className="text-[10px] text-muted-foreground">
                {t('ide.sandbox.signals').replace('{n}', String(signals.length))}
              </span>
            )}
          </div>
          {signals &&
            (signals.length === 0 ? (
              <div className="text-[10px] text-muted-foreground">{t('ide.sandbox.noSignals')}</div>
            ) : (
              <div className="max-h-32 overflow-y-auto rounded border border-quant-border">
                <table className="w-full text-[10px]">
                  <thead>
                    <tr className="text-muted-foreground text-left bg-quant-bg-secondary">
                      <th className="py-1 px-2 font-normal">{t('ide.sandbox.time')}</th>
                      <th className="py-1 px-2 font-normal">{t('ide.sandbox.action')}</th>
                      <th className="py-1 px-2 font-normal text-right">{t('ide.sandbox.price')}</th>
                      <th className="py-1 px-2 font-normal">{t('ide.sandbox.reason')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {signals.slice(0, 100).map((s, i) => (
                      <tr key={`${s.time}-${i}`} className="border-t border-quant-border/50">
                        <td className="py-1 px-2 font-mono text-muted-foreground whitespace-nowrap">{fmtTime(s.time)}</td>
                        <td className="py-1 px-2">
                          <span
                            className={`inline-flex px-1 py-0.5 rounded text-[9px] font-medium ${
                              s.action === 'buy'
                                ? 'bg-emerald-500/10 text-emerald-400'
                                : s.action === 'sell'
                                  ? 'bg-red-500/10 text-red-400'
                                  : 'bg-quant-gold/10 text-quant-gold'
                            }`}
                          >
                            {s.action}
                          </span>
                        </td>
                        <td className="py-1 px-2 text-right font-mono">{s.price?.toLocaleString()}</td>
                        <td className="py-1 px-2 text-muted-foreground truncate max-w-[160px]" title={s.reason}>
                          {s.reason || '-'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ))}
        </div>
      )}
    </div>
  )
}

/**
 * 实验记录（GET /experiments 列表 + 运行中轮询）：IDE 自动调参/敏感性分析
 * 生成的实验在此可见状态与样本内/外收益。
 */
export function ExperimentsPanel() {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState(false)

  const listQuery = useQuery({
    queryKey: ['experiments-list'],
    queryFn: () => indicatorApi.experiment.list(),
    enabled: expanded,
    refetchInterval: (q) => ((q.state.data ?? []).some((e) => e.status === 'running') ? 5000 : false),
    retry: false,
  })
  const items = [...(listQuery.data ?? [])].sort((a, b) => String(b.created_at ?? '').localeCompare(String(a.created_at ?? '')))

  const fmtPct = (v?: number) => (v == null || !isFinite(v) ? '-' : `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`)

  return (
    <div className="px-2 py-1.5 border-t border-quant-border shrink-0">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground transition-colors w-full"
      >
        <FlaskConical className="w-3 h-3 text-quant-gold" />
        {t('ide.experiments.title')}
        {expanded && (
          <span
            role="button"
            tabIndex={0}
            onClick={(e) => {
              e.stopPropagation()
              listQuery.refetch()
            }}
            className="ml-2 text-muted-foreground hover:text-foreground"
            aria-label={t('ide.experiments.refresh')}
          >
            <RefreshCw className={`w-3 h-3 ${listQuery.isFetching ? 'animate-spin' : ''}`} />
          </span>
        )}
        {expanded ? <ChevronUp className="w-3 h-3 ml-auto" /> : <ChevronDown className="w-3 h-3 ml-auto" />}
      </button>
      {expanded && (
        <div className="mt-2 pb-1">
          {listQuery.isLoading ? (
            <div className="text-[10px] text-muted-foreground">...</div>
          ) : items.length === 0 ? (
            <div className="text-[10px] text-muted-foreground">{t('ide.experiments.empty')}</div>
          ) : (
            <div className="max-h-40 overflow-y-auto rounded border border-quant-border">
              <table className="w-full text-[10px]">
                <thead>
                  <tr className="text-muted-foreground text-left bg-quant-bg-secondary">
                    <th className="py-1 px-2 font-normal">{t('ide.experiments.name')}</th>
                    <th className="py-1 px-2 font-normal">{t('ide.experiments.status')}</th>
                    <th className="py-1 px-2 font-normal text-right">{t('ide.experiments.bestScore')}</th>
                    <th className="py-1 px-2 font-normal text-right">{t('ide.experiments.isReturn')}</th>
                    <th className="py-1 px-2 font-normal text-right">{t('ide.experiments.oosReturn')}</th>
                    <th className="py-1 px-2 font-normal text-right">{t('ide.experiments.duration')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((e) => (
                    <tr key={e.id} className="border-t border-quant-border/50">
                      <td className="py-1 px-2 font-mono truncate max-w-[140px]" title={e.name || e.id}>
                        {e.name || e.id}
                      </td>
                      <td className="py-1 px-2">
                        <span
                          className={`inline-flex items-center gap-1 ${
                            e.status === 'running'
                              ? 'text-emerald-400'
                              : e.status === 'completed'
                                ? 'text-quant-gold'
                                : 'text-muted-foreground'
                          }`}
                        >
                          {e.status === 'running' && <Loader2 className="w-2.5 h-2.5 animate-spin" />}
                          {e.status}
                        </span>
                      </td>
                      <td className="py-1 px-2 text-right font-mono">{e.best_score != null ? e.best_score.toFixed(2) : '-'}</td>
                      <td className={`py-1 px-2 text-right font-mono ${(e.is_return_pct ?? 0) >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                        {fmtPct(e.is_return_pct)}
                      </td>
                      <td className={`py-1 px-2 text-right font-mono ${(e.oos_return_pct ?? 0) >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                        {fmtPct(e.oos_return_pct)}
                      </td>
                      <td className="py-1 px-2 text-right font-mono text-muted-foreground">
                        {e.duration_ms ? `${(e.duration_ms / 1000).toFixed(0)}s` : '-'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
