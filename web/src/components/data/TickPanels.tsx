import { useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { DataTable } from '@/components/DataTable'
import { EmptyState } from '@/components/ui/EmptyState'
import { dataApi, type TickItem } from '@/lib/api'
import type { BarDataResponse } from '@/types'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'
import { Download, Loader2, Search } from 'lucide-react'

const BAR_INTERVALS = ['1m', '5m', '15m', '1h', '4h', '1d']

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

/** Tick 数据区块：下载（后端网络下载已下线，按钮会如实透出后端说明）/ 信息 / 查询。 */
export function TickDataPanel() {
  const { t } = useI18n()
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [limit, setLimit] = useState(200)
  const [ticks, setTicks] = useState<TickItem[] | null>(null)
  const [tickCount, setTickCount] = useState(0)

  const norm = () => symbol.trim().toUpperCase()

  const infoQuery = useQuery({
    queryKey: ['tick-info', norm()],
    queryFn: () => dataApi.tickInfo(norm()),
    enabled: false,
    retry: false,
  })

  const downloadMutation = useMutation({
    mutationFn: () =>
      dataApi.tickDownload({
        symbol: norm(),
        start_date: startDate || undefined,
        end_date: endDate || undefined,
      }),
    onSuccess: (d) => toast('success', t('tick.data.downloadStarted').replace('{id}', d.job_id)),
    onError: (err: unknown) => toast('error', err instanceof Error ? err.message : t('tick.data.downloadFailed')),
  })

  const queryMutation = useMutation({
    mutationFn: () => dataApi.ticks(norm(), { limit }),
    onSuccess: (d) => {
      setTicks(d.ticks ?? [])
      setTickCount(d.count ?? 0)
    },
    onError: (err: unknown) => toast('error', err instanceof Error ? err.message : t('tick.data.downloadFailed')),
  })

  const info = infoQuery.data

  return (
    <SectionCard title={t('tick.data.title')}>
      <div className="space-y-5">
        {/* 下载 */}
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">{t('tick.data.downloadTitle')}</div>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <Input label={t('tick.data.symbol')} value={symbol} onChange={(e) => setSymbol(e.target.value)} placeholder="BTCUSDT" />
            <Input label={t('tick.data.startDate')} type="date" value={startDate} onChange={(e) => setStartDate(e.target.value)} />
            <Input label={t('tick.data.endDate')} type="date" value={endDate} onChange={(e) => setEndDate(e.target.value)} />
            <div className="flex items-end">
              <Button
                variant="primary"
                onClick={() => downloadMutation.mutate()}
                isLoading={downloadMutation.isPending}
                leftIcon={downloadMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Download className="w-4 h-4" />}
                className="w-full"
              >
                {downloadMutation.isPending ? t('tick.data.downloading') : t('tick.data.download')}
              </Button>
            </div>
          </div>
        </div>

        {/* 信息 */}
        <div>
          <div className="mb-2 flex items-center gap-3">
            <span className="text-xs font-medium text-muted-foreground">{t('tick.data.infoTitle')}</span>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => infoQuery.refetch()}
              isLoading={infoQuery.isFetching}
            >
              {t('tick.data.queryInfo')}
            </Button>
          </div>
          {info ? (
            info.count > 0 ? (
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                <div className="rounded-lg bg-quant-bg-secondary p-3">
                  <div className="text-[10px] text-muted-foreground">{t('tick.data.infoCount')}</div>
                  <div className="text-sm font-mono font-medium">{info.count.toLocaleString()}</div>
                </div>
                <div className="rounded-lg bg-quant-bg-secondary p-3 sm:col-span-2">
                  <div className="text-[10px] text-muted-foreground">{t('tick.data.infoRange')}</div>
                  <div className="text-sm font-mono font-medium">
                    {fmtMs(info.earliest)} ~ {fmtMs(info.latest)}
                  </div>
                </div>
              </div>
            ) : (
              <div className="text-xs text-muted-foreground">{t('tick.data.infoEmpty')}</div>
            )
          ) : (
            <div className="text-xs text-muted-foreground">—</div>
          )}
        </div>

        {/* 查询 */}
        <div>
          <div className="mb-2 flex items-end gap-3">
            <span className="text-xs font-medium text-muted-foreground pb-2">{t('tick.data.ticksTitle')}</span>
            <div className="w-32">
              <Input label={t('tick.data.limit')} type="number" value={String(limit)} onChange={(e) => setLimit(Number(e.target.value))} />
            </div>
            <Button
              variant="secondary"
              onClick={() => queryMutation.mutate()}
              isLoading={queryMutation.isPending}
              leftIcon={<Search className="w-4 h-4" />}
            >
              {t('tick.data.query')}
            </Button>
            {ticks && <span className="text-[10px] text-muted-foreground pb-2">{t('tick.data.resultCount').replace('{n}', String(tickCount))}</span>}
          </div>
          {ticks &&
            (ticks.length === 0 ? (
              <EmptyState title={t('tick.data.noTicks')} />
            ) : (
              <DataTable
                data={ticks.slice(0, 100)}
                keyExtractor={(item, idx) => `${item.timestamp}-${idx}`}
                columns={[
                  { key: 'time', title: t('tick.data.time'), render: (item) => <span className="text-xs font-mono">{fmtMs(item.timestamp)}</span> },
                  { key: 'last', title: t('tick.data.last'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.last)}</span> },
                  { key: 'bid', title: t('tick.data.bid'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.bid)}</span> },
                  { key: 'ask', title: t('tick.data.ask'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.ask)}</span> },
                  { key: 'volume', title: t('tick.data.volume'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.volume, 4)}</span> },
                ]}
              />
            ))}
        </div>
      </div>
    </SectionCard>
  )
}

/** 本地 K 线预览（/data/bars）：校验已下载数据落库情况。 */
export function LocalBarsPanel() {
  const { t } = useI18n()
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setIntervalVal] = useState('1h')
  const [days, setDays] = useState(7)
  const [bars, setBars] = useState<BarDataResponse['bars'] | null>(null)
  const [barCount, setBarCount] = useState(0)

  const queryMutation = useMutation({
    mutationFn: () => {
      const to = Date.now()
      const from = to - days * 86400000
      return dataApi.bars(symbol.trim().toUpperCase(), interval, from, to)
    },
    onSuccess: (d) => {
      const list = d?.bars ?? []
      setBars(list)
      setBarCount(d?.count ?? list.length)
    },
    onError: (err: unknown) => {
      setBars([])
      setBarCount(0)
      toast('warning', err instanceof Error ? err.message : t('tick.bars.empty'))
    },
  })

  const shown = (bars ?? []).slice(-10)

  return (
    <SectionCard title={t('tick.bars.title')}>
      <div className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3">
          <Input label={t('tick.data.symbol')} value={symbol} onChange={(e) => setSymbol(e.target.value)} placeholder="BTCUSDT" />
          <Select
            label={t('tick.bars.interval')}
            value={interval}
            onChange={(e) => setIntervalVal(e.target.value)}
            options={BAR_INTERVALS.map((i) => ({ value: i, label: i }))}
          />
          <Input label={t('tick.bars.days')} type="number" value={String(days)} onChange={(e) => setDays(Number(e.target.value))} />
          <div className="flex items-end">
            <Button variant="primary" onClick={() => queryMutation.mutate()} isLoading={queryMutation.isPending} leftIcon={<Search className="w-4 h-4" />} className="w-full">
              {t('tick.bars.query')}
            </Button>
          </div>
          {bars && (
            <div className="flex items-end pb-2 text-[10px] text-muted-foreground">
              {t('tick.bars.count').replace('{n}', String(barCount))} · {t('tick.bars.showingLast').replace('{n}', String(shown.length))}
            </div>
          )}
        </div>
        {bars &&
          (bars.length === 0 ? (
            <EmptyState title={t('tick.bars.empty')} />
          ) : (
            <DataTable
              data={shown}
              keyExtractor={(item) => String(item.time)}
              columns={[
                {
                  key: 'time',
                  title: t('tick.bars.time'),
                  render: (item) => <span className="text-xs font-mono">{fmtMs(item.time)}</span>,
                },
                { key: 'open', title: t('tick.bars.open'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.open)}</span> },
                { key: 'high', title: t('tick.bars.high'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.high)}</span> },
                { key: 'low', title: t('tick.bars.low'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.low)}</span> },
                { key: 'close', title: t('tick.bars.close'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.close)}</span> },
                { key: 'volume', title: t('tick.bars.volume'), render: (item) => <span className="text-xs font-mono">{fmtNum(item.volume, 4)}</span> },
              ]}
            />
          ))}
      </div>
    </SectionCard>
  )
}
