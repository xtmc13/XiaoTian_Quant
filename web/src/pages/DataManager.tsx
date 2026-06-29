import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Badge } from '@/components/ui/Badge'
import { DataTable } from '@/components/DataTable'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { dataApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { Database, Download, Loader2 } from 'lucide-react'

const INTERVAL_OPTIONS = [
  { value: '1m', label: '1 分钟' },
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '4h', label: '4 小时' },
  { value: '1d', label: '1 天' },
]

export function DataManager() {
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
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="数据下载" subtitle="历史 K 线 / Tick 数据管理" icon={<Database className="w-5 h-5" />} />

        <SectionCard title="新建下载任务">
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3">
            <Input label="交易对" value={symbol} onChange={(e) => setSymbol(e.target.value)} placeholder="BTCUSDT" />
            <Select
              label="周期"
              value={interval}
              onChange={(e) => setInterval(e.target.value)}
              options={INTERVAL_OPTIONS}
            />
            <Input
              label="交易所"
              value={exchange}
              onChange={(e) => setExchange(e.target.value)}
              placeholder="binance"
            />
            <Input label="天数" type="number" value={String(days)} onChange={(e) => setDays(Number(e.target.value))} />
            <div className="flex items-end">
              <Button
                variant="primary"
                onClick={handleDownload}
                isLoading={downloadMutation.isPending}
                leftIcon={
                  downloadMutation.isPending ? (
                    <Loader2 className="w-4 h-4 animate-spin" />
                  ) : (
                    <Download className="w-4 h-4" />
                  )
                }
                className="w-full"
              >
                开始下载
              </Button>
            </div>
          </div>
        </SectionCard>

        <SectionCard title="已覆盖数据">
          {coverageLoading ? (
            <Skeleton variant="text" lines={5} />
          ) : coverage && coverage.symbols.length > 0 ? (
            <DataTable
              data={coverage.symbols}
              keyExtractor={(item) => item.symbol}
              columns={[
                {
                  key: 'symbol',
                  title: '交易对',
                  render: (item) => <span className="text-sm text-white font-mono">{item.symbol}</span>,
                },
                {
                  key: 'intervals',
                  title: '周期',
                  render: (item) => (
                    <div className="flex flex-wrap gap-1">
                      {item.intervals.map((i) => (
                        <Badge key={i} variant="info">
                          {i}
                        </Badge>
                      ))}
                    </div>
                  ),
                },
                {
                  key: 'range',
                  title: '时间范围',
                  render: (item) => (
                    <span className="text-xs text-muted-foreground">
                      {item.from && item.to
                        ? `${new Date(item.from).toLocaleDateString('zh-CN')} ~ ${new Date(item.to).toLocaleDateString('zh-CN')}`
                        : '-'}
                    </span>
                  ),
                },
              ]}
            />
          ) : (
            <EmptyState title="暂无覆盖数据" description="尚未下载任何历史数据" />
          )}
        </SectionCard>
      </div>
    </div>
  )
}
