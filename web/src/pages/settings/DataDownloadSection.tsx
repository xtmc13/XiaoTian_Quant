import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { SectionCard } from '@/components/ui/SectionCard'
import { dataApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { Download, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

const INTERVAL_OPTIONS = [
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '4h', label: '4 小时' },
  { value: '1d', label: '1 天' },
]

const inputCls =
  'w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-xs text-white placeholder-muted-foreground outline-none focus:border-quant-gold'

export function DataDownloadSection() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setInterval] = useState('1h')
  const [exchange, setExchange] = useState('binance')
  const [days, setDays] = useState(30)

  const { data: coverage, isLoading: coverageLoading } = useQuery({
    queryKey: ['data-coverage'],
    queryFn: () => dataApi.coverage(),
  })

  const downloadMutation = useMutation({
    mutationFn: (config: { symbol: string; interval: string; from: number; to: number; exchange?: string }) =>
      dataApi.download(config),
    onSuccess: (data) => {
      toast('success', `下载任务已启动: ${data.job_id}`)
    },
    onError: (err: unknown) => {
      toast('error', err instanceof Error ? err.message : '启动下载失败')
    },
  })

  const handleDownload = () => {
    const to = Date.now()
    const from = to - days * 24 * 60 * 60 * 1000
    downloadMutation.mutate({ symbol: symbol.toUpperCase(), interval, from, to, exchange })
  }

  return (
    <SectionCard title="数据下载" bodyClassName="space-y-5">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-5">
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">交易对</label>
          <input
            type="text"
            value={symbol}
            onChange={(e) => setSymbol(e.target.value)}
            placeholder="BTCUSDT"
            className={inputCls}
          />
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">周期</label>
          <select value={interval} onChange={(e) => setInterval(e.target.value)} className={inputCls}>
            {INTERVAL_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">交易所</label>
          <input
            type="text"
            value={exchange}
            onChange={(e) => setExchange(e.target.value)}
            placeholder="binance"
            className={inputCls}
          />
        </div>
        <div>
          <label className="mb-1.5 block text-xs text-muted-foreground">天数</label>
          <input
            type="number"
            min={1}
            value={String(days)}
            onChange={(e) => setDays(Number(e.target.value))}
            className={inputCls}
          />
        </div>
        <div className="flex items-end">
          <button
            onClick={handleDownload}
            disabled={downloadMutation.isPending}
            className={cn(
              'w-full flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-xs font-medium transition-opacity',
              downloadMutation.isPending
                ? 'bg-quant-gold/50 text-white cursor-wait'
                : 'bg-quant-gold text-black hover:opacity-90'
            )}
          >
            {downloadMutation.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Download className="h-3.5 w-3.5" />
            )}
            {downloadMutation.isPending ? '下载中...' : '开始下载'}
          </button>
        </div>
      </div>

      <div>
        <div className="text-xs text-muted-foreground mb-2">已覆盖数据</div>
        {coverageLoading ? (
          <div className="text-xs text-muted-foreground py-4">加载中...</div>
        ) : coverage && coverage.symbols.length > 0 ? (
          <div className="rounded-lg border border-quant-border overflow-hidden">
            <table className="w-full text-xs">
              <thead className="bg-quant-bg-secondary">
                <tr>
                  <th className="px-3 py-2 text-left text-muted-foreground font-medium">交易对</th>
                  <th className="px-3 py-2 text-left text-muted-foreground font-medium">周期</th>
                  <th className="px-3 py-2 text-left text-muted-foreground font-medium">时间范围</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-quant-border">
                {coverage.symbols
                  .slice(0, 10)
                  .map((item: { symbol: string; intervals: string[]; from?: number; to?: number }) => (
                    <tr key={item.symbol}>
                      <td className="px-3 py-2 font-mono">{item.symbol}</td>
                      <td className="px-3 py-2">
                        <div className="flex flex-wrap gap-1">
                          {item.intervals.map((i) => (
                            <span
                              key={i}
                              className="rounded bg-quant-gold/10 px-1.5 py-0.5 text-[10px] text-quant-gold"
                            >
                              {i}
                            </span>
                          ))}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">
                        {item.from && item.to
                          ? `${new Date(item.from).toLocaleDateString('zh-CN')} ~ ${new Date(item.to).toLocaleDateString('zh-CN')}`
                          : '-'}
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="text-xs text-muted-foreground py-4">暂无覆盖数据</div>
        )}
      </div>
    </SectionCard>
  )
}
