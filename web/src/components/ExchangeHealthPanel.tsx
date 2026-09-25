import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  exchangeHealthApi,
  type ExchangeHealthLevel,
  type ExchangeHealthOverall,
  type ExchangeHealthRecord,
} from '@/lib/api'
import { Badge } from '@/components/ui/Badge'
import { SectionCard } from '@/components/ui/SectionCard'
import { cn } from '@/lib/utils'
import { Loader2, Play, Stethoscope } from 'lucide-react'

const EXCHANGES = ['binance', 'okx', 'bybit', 'kraken', 'coinbase', 'gate', 'mexc', 'bitget', 'alpaca', 'ibkr']

const OVERALL_TEXT: Record<ExchangeHealthOverall, string> = {
  healthy: '健康',
  degraded: '降级',
  unhealthy: '异常',
  not_configured: '未配置凭证',
}

function overallVariant(overall?: string): 'success' | 'warning' | 'error' | 'neutral' {
  switch (overall) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'unhealthy': return 'error'
    default: return 'neutral'
  }
}

function levelTooltip(level: ExchangeHealthLevel): string {
  const lines = level.items.map((it) => {
    const mark = it.status === 'pass' ? '✓' : it.status === 'fail' ? '✗' : '–'
    const tail = it.error ? ` ${it.error}` : it.detail ? ` ${it.detail}` : ''
    return `${mark} ${it.name} (${it.duration_ms}ms)${tail}`
  })
  return `${level.name} · ${level.status} · ${level.duration_ms}ms\n${lines.join('\n')}`
}

function LevelLights({ levels }: { levels?: ExchangeHealthLevel[] }) {
  return (
    <div className="flex items-center gap-2">
      {[1, 2, 3, 4].map((n) => {
        const level = levels?.find((l) => l.level === n)
        const status = level?.status
        return (
          <div
            key={n}
            title={level ? levelTooltip(level) : '未体检'}
            className="flex items-center gap-1 cursor-default"
          >
            <span
              className={cn(
                'w-2 h-2 rounded-full',
                status === 'pass' && 'bg-[#52c41a]',
                status === 'fail' && 'bg-[#f5222d]',
                status === 'skip' && 'bg-[#666]',
                !status && 'bg-[#333]',
              )}
            />
            <span className="text-[10px] text-muted-foreground">L{n}</span>
          </div>
        )
      })}
    </div>
  )
}

export function ExchangeHealthPanel() {
  const queryClient = useQueryClient()
  const [activeJobId, setActiveJobId] = useState<string | null>(null)

  const { data: latest } = useQuery({
    queryKey: ['exchange-health-latest'],
    queryFn: () => exchangeHealthApi.latest(),
    refetchInterval: 60000,
  })

  const { data: job } = useQuery({
    queryKey: ['exchange-health-job', activeJobId],
    queryFn: () => exchangeHealthApi.job(activeJobId!),
    enabled: !!activeJobId,
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 1500 : false),
  })

  useEffect(() => {
    if (job && job.status !== 'running') {
      // job 结束：刷新落库结果并停止跟踪
      queryClient.invalidateQueries({ queryKey: ['exchange-health-latest'] })
      setActiveJobId(null)
    }
  }, [job, queryClient])

  const startMut = useMutation({
    mutationFn: (exchange?: string) => exchangeHealthApi.start(exchange),
    onSuccess: (data) => setActiveJobId(data.job_id),
  })

  const byExchange = new Map<string, ExchangeHealthRecord>()
  latest?.results?.forEach((rec) => byExchange.set(rec.exchange, rec))

  const runningExchanges = new Set(job?.status === 'running' ? job.exchanges : [])

  return (
    <SectionCard
      title="交易所体检"
      headerAction={
        <button
          onClick={() => startMut.mutate('all')}
          disabled={startMut.isPending || !!activeJobId}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-quant-gold/10 text-quant-gold border border-quant-gold/20 hover:bg-quant-gold/20 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
        >
          {activeJobId ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Stethoscope className="w-3.5 h-3.5" />}
          全部体检
        </button>
      }
    >
      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-5 gap-3">
        {EXCHANGES.map((name) => {
          const rec = byExchange.get(name)
          const running = runningExchanges.has(name)
          return (
            <div key={name} className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3 space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium text-foreground font-mono">{name}</span>
                {running ? (
                  <Badge variant="info">
                    <Loader2 className="w-3 h-3 animate-spin" />
                    体检中
                  </Badge>
                ) : rec ? (
                  <Badge variant={overallVariant(rec.overall)}>{OVERALL_TEXT[rec.overall] || rec.overall}</Badge>
                ) : (
                  <Badge variant="neutral">未体检</Badge>
                )}
              </div>

              <div className="flex items-center justify-between">
                <LevelLights levels={rec?.report?.levels} />
                <span className="text-[10px] text-muted-foreground font-mono">
                  {rec ? `${rec.duration_ms}ms` : '-'}
                </span>
              </div>

              <div className="flex items-center justify-between">
                <span className="text-[10px] text-muted-foreground">
                  {rec?.checked_at ? new Date(rec.checked_at).toLocaleString('zh-CN') : '从未体检'}
                </span>
                <button
                  onClick={() => startMut.mutate(name)}
                  disabled={startMut.isPending || !!activeJobId}
                  title="立即体检"
                  className="inline-flex items-center gap-1 px-2 py-1 rounded text-[10px] text-quant-gold border border-quant-gold/20 hover:bg-quant-gold/10 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                >
                  <Play className="w-3 h-3" />
                  立即体检
                </button>
              </div>
            </div>
          )
        })}
      </div>
      <p className="mt-3 text-[10px] text-muted-foreground">
        L1 公共连通 · L2 凭证有效 · L3 交易能力 · L4 WebSocket；灰灯 = 适配器未实现该项（skip，不计入异常）。体检只读探测，不会下单。
      </p>
    </SectionCard>
  )
}
