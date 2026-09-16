import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  LayoutGrid,
  Play,
  Square,
  Pencil,
  Trash2,
  ChevronRight,
  Wallet,
  Activity,
  PauseCircle,
  TrendingUp,
  TrendingDown,
  Plus,
  Grid3x3,
  ListOrdered,
  LineChart,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { gridApi, marketApi } from '@/lib/api'
import type { GridBot, GridBotDetail, GridBotPayload, GridSnapshot, GridTrade } from '@/lib/api'
import type { TickerSnapshot } from '@/types'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { DataTable } from '@/components/DataTable'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { toast } from '@/lib/useToast'
import { getEcharts } from '@/lib/echarts'
import type { EChartsType } from 'echarts'

const QUICK_SYMBOLS = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT']
const POLL_LIST_MS = 8000
const POLL_DETAIL_MS = 8000
const POLL_PRICE_MS = 5000

function fmtPrice(v: number): string {
  if (!isFinite(v)) return '-'
  const digits = v >= 1000 ? 2 : v >= 10 ? 3 : v >= 1 ? 4 : 6
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: digits })
}

function fmtTime(ts: number): string {
  if (!ts) return '-'
  return new Date(ts).toLocaleString('zh-CN', { hour12: false })
}

function errMsg(e: unknown, fallback: string): string {
  return e instanceof Error && e.message ? e.message : fallback
}

function isRunning(bot: Pick<GridBot, 'status' | 'is_running'>): boolean {
  return bot.status === 'running' || bot.is_running === true
}

/* ── Equity Curve (echarts) ── */
function EquityChart({ snapshots }: { snapshots: GridSnapshot[] }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)
  const sorted = useMemo(() => [...snapshots].sort((a, b) => a.ts - b.ts), [snapshots])

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 64, right: 16, top: 16, bottom: 28 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          valueFormatter: (v: unknown) => `$${formatCurrency(Number(v))}`,
        },
        xAxis: {
          type: 'time',
          axisLabel: { fontSize: 10, color: '#555555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          scale: true,
          axisLabel: { fontSize: 10, color: '#555555', formatter: (v: number) => `$${formatCurrency(v)}` },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
        },
        series: [
          {
            name: '权益',
            type: 'line',
            data: [],
            smooth: true,
            symbol: 'none',
            lineStyle: { color: '#03A66D', width: 2 },
            areaStyle: {
              color: {
                type: 'linear',
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: 'rgba(3,166,109,0.15)' },
                  { offset: 1, color: 'rgba(3,166,109,0)' },
                ],
              },
            },
          },
        ],
      })
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => {
      disposed = true
      chartRef.current?.dispose()
      chartRef.current = null
    }
  }, [])

  useEffect(() => {
    if (!chartRef.current) return
    chartRef.current.setOption({
      series: [{ data: sorted.map((s) => [s.ts, s.equity]) }],
    })
  }, [sorted])

  if (sorted.length === 0) {
    return (
      <EmptyState
        icon={<LineChart className="w-6 h-6" />}
        title="暂无权益数据"
        description="启动机器人后，系统将周期性记录权益曲线"
      />
    )
  }
  return <div ref={ref} className="w-full" style={{ height: 280 }} />
}

/* ── Grid Ladder Visualization ── */
interface GridLadderProps {
  lower: number
  upper: number
  gridCount: number
  price: number
  openOrders: number
  running: boolean
}

function GridLadder({ lower, upper, gridCount, price, openOrders, running }: GridLadderProps) {
  const step = gridCount > 0 ? (upper - lower) / gridCount : 0
  const inRange = price > 0 && price >= lower && price <= upper
  const pct = inRange ? ((price - lower) / (upper - lower)) * 100 : price > upper ? 100 : 0
  const tickEvery = Math.max(1, Math.ceil(gridCount / 40))
  const ticks = []
  for (let i = 0; i <= gridCount; i += tickEvery) {
    ticks.push((i / gridCount) * 100)
  }

  return (
    <div>
      <div className="flex items-center justify-between text-xs text-[#888] mb-2">
        <span>
          下限 <span className="text-[#ccc]">{fmtPrice(lower)}</span>
        </span>
        <span>
          格数 <span className="text-[#ccc]">{gridCount}</span> · 步长{' '}
          <span className="text-[#ccc]">{fmtPrice(step)}</span>
        </span>
        <span>
          上限 <span className="text-[#ccc]">{fmtPrice(upper)}</span>
        </span>
      </div>

      <div className="relative h-16 rounded-lg border border-[#2a2a2a] bg-[#0a0a0a] overflow-hidden">
        {/* Buy zone (below current price) / Sell zone (above) */}
        <div className="absolute inset-y-0 left-0 bg-[#52c41a]/15" style={{ width: `${pct}%` }} />
        <div className="absolute inset-y-0 right-0 bg-[#f5222d]/15" style={{ width: `${100 - pct}%` }} />
        {/* Grid level ticks */}
        {ticks.map((t) => (
          <div
            key={t}
            className="absolute inset-y-0 w-px bg-white/10"
            style={{ left: `${t}%` }}
          />
        ))}
        {/* Zone labels */}
        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-[10px] font-medium text-[#52c41a]/80">
          买区
        </span>
        <span className="absolute right-3 top-1/2 -translate-y-1/2 text-[10px] font-medium text-[#f5222d]/80">
          卖区
        </span>
        {/* Current price marker */}
        {price > 0 && (
          <div className="absolute inset-y-0 w-[2px] bg-[#faad14]" style={{ left: `${pct}%` }}>
            <div className="absolute -top-0 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-b bg-[#faad14] px-1.5 py-0.5 text-[10px] font-semibold text-black">
              {fmtPrice(price)}
            </div>
          </div>
        )}
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-[#888]">
        <span className="inline-flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-sm bg-[#52c41a]/60" /> 当前价下方为买入网格
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="w-2 h-2 rounded-sm bg-[#f5222d]/60" /> 当前价上方为卖出网格
        </span>
        <span className="ml-auto">
          当前价 {price > 0 ? <span className="text-[#faad14] font-medium">{fmtPrice(price)}</span> : '获取中…'}
          {!inRange && price > 0 && <span className="text-[#f5222d] ml-1">(超出区间)</span>}
        </span>
        <span>
          挂单数{' '}
          <span className="text-[#ccc]">{running ? openOrders : '—'}</span>
        </span>
      </div>
    </div>
  )
}

/* ── Create / Edit Form ── */
interface GridBotFormProps {
  editing: GridBot | null
  isPending: boolean
  onSubmit: (payload: GridBotPayload) => void
  onCancel: () => void
}

function GridBotForm({ editing, isPending, onSubmit, onCancel }: GridBotFormProps) {
  const [name, setName] = useState(editing?.name ?? '')
  const [symbol, setSymbol] = useState(editing?.symbol ?? 'BTCUSDT')
  const [lowerPrice, setLowerPrice] = useState(editing ? String(editing.lower_price) : '')
  const [upperPrice, setUpperPrice] = useState(editing ? String(editing.upper_price) : '')
  const [gridCount, setGridCount] = useState(editing ? String(editing.grid_count) : '20')
  const [investment, setInvestment] = useState(editing ? String(editing.investment) : '')
  const [feeRate, setFeeRate] = useState(editing ? String(editing.fee_rate) : '0.001')
  const [priceLoading, setPriceLoading] = useState(false)

  const fetchPrice = async (): Promise<number> => {
    const sym = symbol.trim().toUpperCase()
    if (!sym) {
      toast('warning', '请先输入交易对')
      return 0
    }
    setPriceLoading(true)
    try {
      const d = await marketApi.snapshot(sym)
      const p = Number((d as TickerSnapshot).price) || 0
      if (p <= 0) toast('warning', '行情未就绪，请稍后再试')
      return p
    } catch {
      return 0
    } finally {
      setPriceLoading(false)
    }
  }

  const applyRange = async (pct: number) => {
    const p = await fetchPrice()
    if (p <= 0) return
    setLowerPrice((p * (1 - pct)).toFixed(4))
    setUpperPrice((p * (1 + pct)).toFixed(4))
  }

  const handleSubmit = () => {
    const lower = parseFloat(lowerPrice)
    const upper = parseFloat(upperPrice)
    const grids = parseInt(gridCount, 10)
    const amount = parseFloat(investment)
    const fee = parseFloat(feeRate)

    if (!name.trim()) return toast('warning', '请填写机器人名称')
    if (!symbol.trim()) return toast('warning', '请填写交易对')
    if (!isFinite(lower) || !isFinite(upper) || lower <= 0 || upper <= 0 || lower >= upper)
      return toast('warning', '区间下限必须小于上限且均为正数')
    if (!isFinite(grids) || grids < 2 || grids > 200) return toast('warning', '网格数必须在 2-200 之间')
    if (!isFinite(amount) || amount <= 0) return toast('warning', '投入金额必须大于 0')
    if (!isFinite(fee) || fee < 0) return toast('warning', '费率不能为负数')

    onSubmit({
      name: name.trim(),
      symbol: symbol.trim().toUpperCase(),
      lower_price: lower,
      upper_price: upper,
      grid_count: grids,
      investment: amount,
      fee_rate: fee,
    })
  }

  return (
    <SectionCard title={editing ? `编辑: ${editing.name}` : '创建网格机器人'}>
      <div className="space-y-4">
        <Input
          label="名称"
          required
          placeholder="例如：BTC 震荡网格"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />

        <div>
          <Input
            label="交易对"
            required
            placeholder="如 BTCUSDT"
            value={symbol}
            onChange={(e) => setSymbol(e.target.value.toUpperCase())}
          />
          <div className="flex gap-1.5 mt-2">
            {QUICK_SYMBOLS.map((s) => (
              <button
                key={s}
                type="button"
                onClick={() => setSymbol(s)}
                className={cn(
                  'px-2.5 py-1 rounded-md text-[11px] font-medium border transition-colors',
                  symbol === s
                    ? 'bg-[#1890ff]/10 text-[#1890ff] border-[#1890ff]/40'
                    : 'bg-[#0a0a0a] text-[#888] border-[#2a2a2a] hover:text-[#ccc] hover:border-[#333]'
                )}
              >
                {s}
              </button>
            ))}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <Input
            label="区间下限"
            required
            type="number"
            min="0"
            step="any"
            placeholder="0.0000"
            value={lowerPrice}
            onChange={(e) => setLowerPrice(e.target.value)}
          />
          <Input
            label="区间上限"
            required
            type="number"
            min="0"
            step="any"
            placeholder="0.0000"
            value={upperPrice}
            onChange={(e) => setUpperPrice(e.target.value)}
          />
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-[11px] text-[#666] mr-1">取现价</span>
          {[
            { pct: 0.03, label: '±3%' },
            { pct: 0.05, label: '±5%' },
          ].map(({ pct, label }) => (
            <Button
              key={label}
              variant="outline"
              size="sm"
              isLoading={priceLoading}
              onClick={() => applyRange(pct)}
            >
              {label}
            </Button>
          ))}
          {priceLoading && <span className="text-[11px] text-[#666]">获取行情中…</span>}
        </div>

        <div className="grid grid-cols-2 gap-3">
          <Input
            label="网格数"
            required
            type="number"
            min={2}
            max={200}
            helperText="2 - 200 格"
            value={gridCount}
            onChange={(e) => setGridCount(e.target.value)}
          />
          <Input
            label="费率"
            type="number"
            min="0"
            step="any"
            helperText="默认 0.001（千一）"
            value={feeRate}
            onChange={(e) => setFeeRate(e.target.value)}
          />
        </div>

        <Input
          label="投入金额 (USDT)"
          required
          type="number"
          min="0"
          step="any"
          placeholder="0.00"
          value={investment}
          onChange={(e) => setInvestment(e.target.value)}
        />

        <div className="flex items-center gap-2 pt-1">
          <Button variant="primary" className="flex-1" isLoading={isPending} onClick={handleSubmit}>
            {editing ? '保存修改' : '创建机器人'}
          </Button>
          {editing && (
            <Button variant="ghost" onClick={onCancel}>
              取消
            </Button>
          )}
        </div>
        {editing && (
          <p className="text-[11px] text-[#666]">运行中的机器人不能修改参数，请先停止。</p>
        )}
      </div>
    </SectionCard>
  )
}

/* ── Bot Card ── */
interface BotCardProps {
  bot: GridBot
  selected: boolean
  actionLoading: boolean
  onSelect: () => void
  onStart: () => void
  onStop: () => void
  onEdit: () => void
  onDelete: () => void
}

function BotCard({ bot, selected, actionLoading, onSelect, onStart, onStop, onEdit, onDelete }: BotCardProps) {
  const running = isRunning(bot)
  const equity = bot.initial_equity > 0 ? bot.initial_equity + bot.realized_pnl : bot.investment

  return (
    <div
      className={cn(
        'rounded-xl border bg-[#111] transition-all hover:border-[#333] cursor-pointer',
        selected ? 'border-[#faad14]/60 ring-1 ring-[#faad14]/30' : 'border-[#1c1c1c]'
      )}
      onClick={onSelect}
    >
      <div className="p-4">
        <div className="flex items-start justify-between mb-3">
          <div className="flex items-center gap-2 min-w-0">
            <span className={cn('w-2 h-2 rounded-full flex-shrink-0', running ? 'bg-[#52c41a] animate-pulse' : 'bg-[#555]')} />
            <span className="text-sm font-medium text-[#e0e0e0] truncate">{bot.name || '未命名'}</span>
          </div>
          <Badge variant={running ? 'success' : 'neutral'} dot>
            {running ? '运行中' : '已停止'}
          </Badge>
        </div>

        <div className="flex items-center justify-between mb-3">
          <Badge variant="info" className="text-[10px]">
            {bot.symbol}
          </Badge>
          <span
            className={cn(
              'text-sm font-semibold',
              bot.realized_pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'
            )}
          >
            {bot.realized_pnl >= 0 ? '+' : ''}
            {formatCurrency(bot.realized_pnl)}
          </span>
        </div>

        <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5 mb-3">
          <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-[10px]">
            <div className="text-[#888]">
              区间:{' '}
              <span className="text-[#ccc]">
                {fmtPrice(bot.lower_price)} ~ {fmtPrice(bot.upper_price)}
              </span>
            </div>
            <div className="text-[#888]">
              格数: <span className="text-[#ccc]">{bot.grid_count}</span>
            </div>
            <div className="text-[#888]">
              成交: <span className="text-[#ccc]">{bot.total_trades} 笔</span>
            </div>
            <div className="text-[#888]">
              权益: <span className="text-[#ccc]">${formatCurrency(equity)}</span>
            </div>
            <div className="text-[#888]">
              持仓: <span className="text-[#ccc]">{bot.base_qty.toFixed(6)}</span>
            </div>
            <div className="text-[#888]">
              可用: <span className="text-[#ccc]">${formatCurrency(bot.quote_balance)}</span>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
          {running ? (
            <Button
              variant="ghost"
              size="sm"
              isLoading={actionLoading}
              onClick={onStop}
              leftIcon={<Square className="w-3 h-3 text-[#f5222d]" />}
              className="text-[#f5222d] hover:bg-[#f5222d]/10"
            >
              停止
            </Button>
          ) : (
            <Button
              variant="ghost"
              size="sm"
              isLoading={actionLoading}
              onClick={onStart}
              leftIcon={<Play className="w-3 h-3 text-[#52c41a]" />}
              className="text-[#52c41a] hover:bg-[#52c41a]/10"
            >
              启动
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={onEdit} leftIcon={<Pencil className="w-3 h-3" />}>
            编辑
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={onDelete}
            leftIcon={<Trash2 className="w-3 h-3 text-[#f5222d]" />}
            className="text-[#f5222d] hover:bg-[#f5222d]/10"
          >
            删除
          </Button>
          <Button variant="ghost" size="sm" onClick={onSelect} className="ml-auto">
            <ChevronRight className="w-3 h-3" />
          </Button>
        </div>
      </div>
    </div>
  )
}

/* ── Detail Dashboard ── */
interface BotDetailProps {
  detail: GridBotDetail
  livePrice: number
  actionLoading: boolean
  onStart: () => void
  onStop: () => void
}

function BotDetailDashboard({ detail, livePrice, actionLoading, onStart, onStop }: BotDetailProps) {
  const running = isRunning(detail)
  const price = livePrice > 0 ? livePrice : detail.snapshots[detail.snapshots.length - 1]?.price ?? 0
  const currentEquity = detail.snapshots[detail.snapshots.length - 1]?.equity ?? detail.initial_equity
  const trades = useMemo(() => [...detail.trades].sort((a, b) => b.ts - a.ts), [detail.trades])

  const tradeColumns = [
    {
      key: 'time',
      title: '时间',
      render: (t: GridTrade) => <span className="text-[#aaa]">{fmtTime(t.ts)}</span>,
    },
    {
      key: 'side',
      title: '方向',
      render: (t: GridTrade) => (
        <Badge variant={t.side.toUpperCase() === 'BUY' ? 'success' : 'error'}>
          {t.side.toUpperCase() === 'BUY' ? '买入' : '卖出'}
        </Badge>
      ),
    },
    {
      key: 'level',
      title: '档位',
      render: (t: GridTrade) => <span className="text-[#ccc]">#{t.level_index}</span>,
    },
    {
      key: 'price',
      title: '价格',
      render: (t: GridTrade) => <span className="text-[#ccc]">{fmtPrice(t.price)}</span>,
    },
    {
      key: 'quantity',
      title: '数量',
      render: (t: GridTrade) => <span className="text-[#ccc]">{t.quantity.toFixed(6)}</span>,
    },
    {
      key: 'quote_qty',
      title: '成交额',
      render: (t: GridTrade) => <span className="text-[#ccc]">${formatCurrency(t.quote_qty)}</span>,
    },
    {
      key: 'pnl',
      title: '盈亏',
      render: (t: GridTrade) => (
        <span className={cn(t.pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
          {t.pnl >= 0 ? '+' : ''}
          {formatCurrency(t.pnl)}
        </span>
      ),
    },
  ]

  return (
    <div className="space-y-4">
      {/* Control bar */}
      <SectionCard noPadding>
        <div className="flex flex-wrap items-center gap-3 px-5 py-4">
          <div className="flex items-center gap-2 min-w-0">
            <span className={cn('w-2.5 h-2.5 rounded-full flex-shrink-0', running ? 'bg-[#52c41a] animate-pulse' : 'bg-[#555]')} />
            <span className="text-base font-semibold text-[#e0e0e0] truncate">{detail.name}</span>
            <Badge variant="info">{detail.symbol}</Badge>
            <Badge variant={running ? 'success' : 'neutral'} dot>
              {running ? '运行中' : '已停止'}
            </Badge>
          </div>
          <div className="flex items-center gap-4 text-xs text-[#888] ml-auto">
            <span>
              当前权益{' '}
              <span className="text-[#e0e0e0] font-medium">${formatCurrency(currentEquity)}</span>
            </span>
            <span>
              已实现盈亏{' '}
              <span className={cn('font-medium', detail.realized_pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                {detail.realized_pnl >= 0 ? '+' : ''}${formatCurrency(detail.realized_pnl)}
              </span>
            </span>
            <span>
              挂单数 <span className="text-[#e0e0e0] font-medium">{running ? detail.open_orders : '—'}</span>
            </span>
          </div>
          {running ? (
            <Button variant="danger" size="lg" isLoading={actionLoading} onClick={onStop} leftIcon={<Square className="w-4 h-4" />}>
              停止机器人
            </Button>
          ) : (
            <Button
              variant="secondary"
              size="lg"
              isLoading={actionLoading}
              onClick={onStart}
              leftIcon={<Play className="w-4 h-4" />}
            >
              启动机器人
            </Button>
          )}
        </div>
      </SectionCard>

      <div className="grid grid-cols-1 xl:grid-cols-5 gap-4">
        {/* Equity curve */}
        <SectionCard title="权益曲线" className="xl:col-span-3">
          <EquityChart snapshots={detail.snapshots} />
        </SectionCard>

        {/* Grid ladder */}
        <SectionCard
          title="网格阶梯"
          className="xl:col-span-2"
          headerAction={
            <span className="text-[11px] text-[#666]">
              区间 {fmtPrice(detail.lower_price)} ~ {fmtPrice(detail.upper_price)} · {detail.grid_count} 格
            </span>
          }
        >
          <GridLadder
            lower={detail.lower_price}
            upper={detail.upper_price}
            gridCount={detail.grid_count}
            price={price}
            openOrders={detail.open_orders}
            running={running}
          />
        </SectionCard>
      </div>

      {/* Trades */}
      <SectionCard
        title="成交记录"
        headerAction={<span className="text-[11px] text-[#666]">最近 {trades.length} 笔</span>}
      >
        {trades.length === 0 ? (
          <EmptyState
            icon={<ListOrdered className="w-6 h-6" />}
            title="暂无成交"
            description="机器人运行后，网格成交将记录在这里"
          />
        ) : (
          <DataTable
            data={trades}
            keyExtractor={(t) => String(t.id)}
            columns={tradeColumns}
            emptyText="暂无成交"
          />
        )}
      </SectionCard>
    </div>
  )
}

/* ── Page ── */
export function BotsGrid() {
  const queryClient = useQueryClient()
  const { confirm, Dialog } = useConfirmDialog()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [editing, setEditing] = useState<GridBot | null>(null)
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)

  const { data: bots = [], isLoading } = useQuery({
    queryKey: ['grid-bots'],
    queryFn: gridApi.list,
    refetchInterval: POLL_LIST_MS,
  })

  const { data: detail } = useQuery({
    queryKey: ['grid-bot', selectedId],
    queryFn: () => gridApi.get(selectedId as string),
    enabled: !!selectedId,
    refetchInterval: POLL_DETAIL_MS,
  })

  const detailSymbol = detail?.symbol ?? ''
  const { data: livePrice = 0 } = useQuery({
    queryKey: ['grid-price', detailSymbol],
    queryFn: () => marketApi.snapshot(detailSymbol).then((d) => Number((d as TickerSnapshot).price) || 0),
    enabled: !!detailSymbol,
    refetchInterval: POLL_PRICE_MS,
  })

  const invalidateBots = () => queryClient.invalidateQueries({ queryKey: ['grid-bots'] })
  const invalidateDetail = (id: string) => queryClient.invalidateQueries({ queryKey: ['grid-bot', id] })

  const createMutation = useMutation({
    mutationFn: gridApi.create,
    onSuccess: async (bot) => {
      await invalidateBots()
      toast('success', '网格机器人创建成功')
      setEditing(null)
      setSelectedId(bot.id)
    },
    onError: (e) => toast('error', errMsg(e, '创建失败')),
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: GridBotPayload }) => gridApi.update(id, payload),
    onSuccess: async (_d, vars) => {
      await Promise.all([invalidateBots(), invalidateDetail(vars.id)])
      toast('success', '参数已更新')
      setEditing(null)
    },
    onError: (e) => toast('error', errMsg(e, '更新失败')),
  })

  const handleSubmit = (payload: GridBotPayload) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, payload })
    } else {
      createMutation.mutate(payload)
    }
  }

  const runAction = async (id: string, action: () => Promise<unknown>, successMsg: string, fallback: string) => {
    setActionLoadingId(id)
    try {
      await action()
      toast('success', successMsg)
      await Promise.all([invalidateBots(), invalidateDetail(id)])
    } catch (e) {
      toast('error', errMsg(e, fallback))
    } finally {
      setActionLoadingId(null)
    }
  }

  const handleStart = (bot: GridBot) => runAction(bot.id, () => gridApi.start(bot.id), '机器人已启动', '启动失败')
  const handleStop = (bot: GridBot) => runAction(bot.id, () => gridApi.stop(bot.id), '机器人已停止', '停止失败')

  const handleEdit = (bot: GridBot) => {
    if (isRunning(bot)) {
      toast('info', '请先停止机器人再编辑参数')
      return
    }
    setEditing(bot)
  }

  const handleDelete = async (bot: GridBot) => {
    const ok = await confirm({
      title: '删除网格机器人',
      message: `确定删除「${bot.name}」吗？运行中的机器人会先被停止。`,
      confirmText: '删除',
      variant: 'danger',
    })
    if (!ok) return
    try {
      await gridApi.remove(bot.id)
      toast('success', '机器人已删除')
      if (selectedId === bot.id) setSelectedId(null)
      if (editing?.id === bot.id) setEditing(null)
      await invalidateBots()
    } catch (e) {
      toast('error', errMsg(e, '删除失败'))
    }
  }

  const runningCount = bots.filter(isRunning).length
  const totalInvestment = bots.reduce((s, b) => s + b.investment, 0)
  const totalPnl = bots.reduce((s, b) => s + b.realized_pnl, 0)

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title="网格机器人"
          subtitle="在震荡行情中自动低买高卖，赚取网格利润"
          icon={<LayoutGrid className="w-5 h-5" />}
        />

        {/* KPI Row */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <KPICard
            icon={<Activity className="h-4 w-4 text-[#52c41a]" />}
            label="运行中"
            value={String(runningCount)}
            subValue={`共 ${bots.length} 个`}
          />
          <KPICard
            icon={<PauseCircle className="h-4 w-4 text-[#faad14]" />}
            label="已停止"
            value={String(bots.length - runningCount)}
          />
          <KPICard
            icon={<Wallet className="h-4 w-4 text-[#1890ff]" />}
            label="总投入"
            value={`$${formatCurrency(totalInvestment)}`}
          />
          <KPICard
            icon={
              totalPnl >= 0 ? (
                <TrendingUp className="h-4 w-4 text-[#52c41a]" />
              ) : (
                <TrendingDown className="h-4 w-4 text-[#f5222d]" />
              )
            }
            label="累计已实现盈亏"
            value={`${totalPnl >= 0 ? '+' : ''}${formatCurrency(totalPnl)}`}
            trend={totalPnl >= 0 ? 'up' : 'down'}
            variant={totalPnl >= 0 ? 'success' : 'error'}
            primary
          />
        </div>

        {/* Create + List */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 items-start">
          <GridBotForm
            key={editing?.id ?? 'new'}
            editing={editing}
            isPending={createMutation.isPending || updateMutation.isPending}
            onSubmit={handleSubmit}
            onCancel={() => setEditing(null)}
          />

          <SectionCard
            title="机器人列表"
            className="lg:col-span-2"
            headerAction={
              <div className="flex items-center gap-2">
                <span className="text-xs text-[#8a8a8a]">共 {bots.length} 个</span>
                {bots.length > 0 && (
                  <Button
                    variant="ghost"
                    size="sm"
                    leftIcon={<Plus className="w-3 h-3" />}
                    onClick={() => setEditing(null)}
                  >
                    新建
                  </Button>
                )}
              </div>
            }
          >
            {isLoading ? (
              <div className="space-y-3">
                {Array.from({ length: 2 }).map((_, i) => (
                  <Skeleton key={i} className="h-36 rounded-xl" />
                ))}
              </div>
            ) : bots.length === 0 ? (
              <EmptyState
                icon={<Grid3x3 className="w-8 h-8" />}
                title="暂无网格机器人"
                description="在左侧创建你的第一个网格机器人"
              />
            ) : (
              <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
                {bots.map((bot) => (
                  <BotCard
                    key={bot.id}
                    bot={bot}
                    selected={selectedId === bot.id}
                    actionLoading={actionLoadingId === bot.id}
                    onSelect={() => setSelectedId(bot.id)}
                    onStart={() => handleStart(bot)}
                    onStop={() => handleStop(bot)}
                    onEdit={() => handleEdit(bot)}
                    onDelete={() => handleDelete(bot)}
                  />
                ))}
              </div>
            )}
          </SectionCard>
        </div>

        {/* Detail dashboard */}
        {selectedId ? (
          detail ? (
            <BotDetailDashboard
              detail={detail}
              livePrice={livePrice}
              actionLoading={actionLoadingId === selectedId}
              onStart={() => detail && handleStart(detail)}
              onStop={() => detail && handleStop(detail)}
            />
          ) : (
            <div className="space-y-4">
              <Skeleton className="h-20 rounded-xl" />
              <Skeleton className="h-72 rounded-xl" />
            </div>
          )
        ) : (
          !isLoading &&
          bots.length > 0 && (
            <SectionCard>
              <EmptyState
                icon={<LayoutGrid className="w-8 h-8" />}
                title="选择一个机器人查看详情"
                description="点击上方卡片查看权益曲线、网格阶梯与成交记录"
              />
            </SectionCard>
          )
        )}
      </div>
      <Dialog />
    </div>
  )
}

export default BotsGrid
