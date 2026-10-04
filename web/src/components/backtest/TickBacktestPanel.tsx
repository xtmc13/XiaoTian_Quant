import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { EmptyState } from '@/components/ui/EmptyState'
import { tickBacktestApi, type TickBacktestJob } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'
import { Activity, Loader2, FlaskConical } from 'lucide-react'

function fmtMs(ms: number): string {
  if (!ms) return '-'
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function fmtNum(v: number | undefined, d = 2): string {
  if (v == null || !isFinite(v)) return '-'
  return v.toLocaleString('en-US', { minimumFractionDigits: d, maximumFractionDigits: d })
}

/** Tick 级回测面板（回测中心）：提交任务 → 轮询任务列表 → 查看结果指标。 */
export function TickBacktestPanel() {
  const { t } = useI18n()
  const [strategy, setStrategy] = useState('sma_cross')
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [selectedJob, setSelectedJob] = useState<string | null>(null)

  const jobsQuery = useQuery({
    queryKey: ['tick-backtest-jobs'],
    queryFn: () => tickBacktestApi.jobs(),
    refetchInterval: (q) => {
      const jobs = q.state.data ?? []
      return jobs.some((j) => j.status === 'running') ? 3000 : 15000
    },
    retry: false,
  })

  const runMutation = useMutation({
    mutationFn: () => {
      const start = startDate ? new Date(startDate + 'T00:00:00').getTime() : 0
      const end = endDate ? new Date(endDate + 'T23:59:59').getTime() : Date.now()
      return tickBacktestApi.run({ strategy, symbol: symbol.trim().toUpperCase(), start, end })
    },
    onSuccess: (d) => {
      toast('success', t('tick.bt.started').replace('{id}', d.job_id))
      setSelectedJob(d.job_id)
      jobsQuery.refetch()
    },
    onError: (err: unknown) => toast('error', err instanceof Error ? err.message : t('tick.bt.startFailed')),
  })

  const jobs = [...(jobsQuery.data ?? [])].sort((a, b) => b.started_at - a.started_at)
  const detail: TickBacktestJob | undefined = jobs.find((j) => j.id === selectedJob)

  const statusLabel = (s: string) =>
    s === 'running'
      ? t('tick.bt.statusRunning')
      : s === 'done'
        ? t('tick.bt.statusDone')
        : s === 'failed'
          ? t('tick.bt.statusFailed')
          : s

  return (
    <SectionCard title={t('tick.bt.title')}>
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">{t('tick.bt.desc')}</p>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3">
          <Select
            label={t('tick.bt.strategy')}
            value={strategy}
            onChange={(e) => setStrategy(e.target.value)}
            options={[
              { value: 'sma_cross', label: t('tick.bt.strategySma') },
              { value: 'breakout', label: t('tick.bt.strategyBreakout') },
            ]}
          />
          <Input label={t('tick.bt.symbol')} value={symbol} onChange={(e) => setSymbol(e.target.value)} placeholder="BTCUSDT" />
          <Input label={t('tick.bt.start')} type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
          <Input label={t('tick.bt.end')} type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
          <div className="flex items-end">
            <Button
              variant="primary"
              onClick={() => runMutation.mutate()}
              isLoading={runMutation.isPending}
              leftIcon={runMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <FlaskConical className="w-4 h-4" />}
              className="w-full"
            >
              {t('tick.bt.run')}
            </Button>
          </div>
        </div>

        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">{t('tick.bt.jobs')}</div>
          {jobs.length === 0 ? (
            <EmptyState icon={<Activity className="w-8 h-8 text-muted-foreground" />} title={t('tick.bt.noJobs')} />
          ) : (
            <div className="space-y-2">
              {jobs.map((job) => (
                <button
                  key={job.id}
                  onClick={() => setSelectedJob(selectedJob === job.id ? null : job.id)}
                  className="w-full text-left rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-2 hover:border-quant-gold/30 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    <span
                      className={`w-2 h-2 rounded-full shrink-0 ${
                        job.status === 'running'
                          ? 'bg-green-400 animate-pulse'
                          : job.status === 'done'
                            ? 'bg-quant-gold'
                            : job.status === 'failed'
                              ? 'bg-red-400'
                              : 'bg-muted-foreground'
                      }`}
                    />
                    <span className="text-xs font-mono">{job.id}</span>
                    <span className="text-[10px] text-muted-foreground">{statusLabel(job.status)}</span>
                    <span className="ml-auto text-[10px] text-muted-foreground">{fmtMs(job.started_at)}</span>
                  </div>
                  {selectedJob === job.id && (
                    <div className="mt-3 pt-3 border-t border-quant-border">
                      {job.status === 'failed' ? (
                        <div className="text-xs text-red-400">
                          {t('tick.bt.error')}: {job.error || '-'}
                        </div>
                      ) : job.result ? (
                        <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                          {[
                            {
                              label: t('tick.bt.totalReturn'),
                              value: `${job.result.total_return_pct >= 0 ? '+' : ''}${fmtNum(job.result.total_return_pct)}%`,
                            },
                            { label: t('tick.bt.maxDrawdown'), value: `${fmtNum(job.result.max_drawdown_pct)}%` },
                            { label: t('tick.bt.sharpe'), value: fmtNum(job.result.sharpe_ratio) },
                            {
                              label: t('tick.bt.winRate'),
                              value: `${fmtNum(job.result.win_rate * (job.result.win_rate <= 1 ? 100 : 1))}%`,
                            },
                            { label: t('tick.bt.totalTrades'), value: String(job.result.total_trades) },
                            { label: t('tick.bt.profitFactor'), value: fmtNum(job.result.profit_factor) },
                            { label: t('tick.bt.ticksProcessed'), value: job.result.ticks_processed.toLocaleString() },
                            { label: t('tick.bt.duration'), value: `${(job.result.duration_ms / 1000).toFixed(1)}s` },
                          ].map((m) => (
                            <div key={m.label} className="rounded-md bg-quant-bg p-2">
                              <div className="text-[10px] text-muted-foreground">{m.label}</div>
                              <div className="text-sm font-mono font-medium">{m.value}</div>
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-xs text-muted-foreground">{t('tick.bt.running')}</div>
                      )}
                    </div>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>
    </SectionCard>
  )
}
