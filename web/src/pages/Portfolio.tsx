import React, { useState, useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { portfolioApi, accountApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import { getEcharts } from '@/lib/echarts'
import { DataTable } from '@/components/DataTable'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import {
  Wallet,
  TrendingUp,
  TrendingDown,
  Activity,
  ShieldAlert,
  Layers,
  ChevronLeft,
  ChevronRight,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

interface PositionItem {
  symbol: string
  quantity: number
  avg_entry_price: number
  current_price?: number
  unrealized_pnl: number
  realized_pnl?: number
}

interface SnapshotItem {
  timestamp: number
  total_equity: number
  drawdown?: number
}

interface CalendarMonth {
  month_key: string
  year: number
  month: number
  days_in_month?: number
  first_weekday?: number
  days: Record<string, number>
  total: number
  win_days: number
  lose_days: number
}

/* ── Equity Chart Component ──────────────────────────────────────── */

function EquityChart({ data, isLoading }: { data?: SnapshotItem[]; isLoading?: boolean }) {
  const chartRef = React.useRef<HTMLDivElement>(null)
  const chartInstance = React.useRef<any>(null)

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !chartRef.current) return
      chartInstance.current = echarts.init(chartRef.current, 'dark')
      chartInstance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 48, right: 16, top: 16, bottom: 24 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          formatter: (params: any[]) => {
            const p = params[0]
            return `<div style="font-size:10px;color:#888;margin-bottom:4px">${new Date(p.value[0]).toLocaleDateString()}</div>
                    <div style="font-weight:600;color:#fff">$${formatCurrency(p.value[1])}</div>`
          },
        },
        xAxis: {
          type: 'time',
          axisLabel: { fontSize: 10, color: '#555555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          axisLabel: { fontSize: 10, color: '#555555', formatter: (v: number) => `$${formatCurrency(v)}` },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
        },
        series: [
          {
            type: 'line',
            data: [],
            smooth: true,
            symbol: 'none',
            lineStyle: { color: '#03A66D', width: 2 },
            areaStyle: {
              color: {
                type: 'linear',
                x: 0, y: 0, x2: 0, y2: 1,
                colorStops: [
                  { offset: 0, color: 'rgba(3,166,109,0.12)' },
                  { offset: 1, color: 'rgba(3,166,109,0)' },
                ],
              },
            },
          },
        ],
      })
      const ro = new ResizeObserver(() => chartInstance.current?.resize())
      ro.observe(chartRef.current)
    })
    return () => { disposed = true; chartInstance.current?.dispose() }
  }, [])

  useEffect(() => {
    if (chartInstance.current && data) {
      chartInstance.current.setOption({
        series: [{ data: data.map((d) => [d.timestamp * 1000, d.total_equity]) }],
      })
    }
  }, [data])

  if (isLoading) return <div className="h-64 animate-pulse rounded-lg bg-quant-bg-secondary" />
  return <div ref={chartRef} className="h-64 w-full" />
}

/* ── Asset Pie Chart Component ───────────────────────────────────── */

function AssetPieChart({ balances, isLoading }: { balances?: { asset: string; total: number }[]; isLoading?: boolean }) {
  const ref = React.useRef<HTMLDivElement>(null)
  const chartRef = React.useRef<any>(null)

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        tooltip: {
          trigger: 'item',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          formatter: '{b}: ${c} ({d}%)',
        },
        series: [
          {
            type: 'pie',
            radius: ['40%', '70%'],
            center: ['50%', '50%'],
            itemStyle: { borderRadius: 4, borderColor: '#0a0a0a', borderWidth: 2 },
            label: { show: false },
            data: [],
          },
        ],
      })
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => { disposed = true; chartRef.current?.dispose() }
  }, [])

  useEffect(() => {
    if (chartRef.current && balances) {
      const data = balances
        .filter((b) => b.total > 0)
        .map((b, i) => ({
          name: b.asset,
          value: b.total,
          itemStyle: {
            color: ['#03A66D', '#f5af19', '#f12711', '#667eea', '#764ba2', '#43e97b'][i % 6],
          },
        }))
      chartRef.current.setOption({ series: [{ data }] })
    }
  }, [balances])

  if (isLoading) return <div className="h-64 animate-pulse rounded-lg bg-quant-bg-secondary" />
  return <div ref={ref} className="h-64 w-full" />
}

/* ── Calendar Component ──────────────────────────────────────────── */

function ProfitCalendar({ months, isLoading }: { months?: CalendarMonth[]; isLoading?: boolean }) {
  const [monthIdx, setMonthIdx] = useState(0)
  const current = months?.[monthIdx]

  if (isLoading) {
    return (
      <div className="space-y-3">
        <Skeleton variant="text" lines={2} />
        <div className="grid grid-cols-7 gap-1">
          {Array.from({ length: 35 }).map((_, i) => (
            <Skeleton key={`sk-${i}`} variant="rect" height={32} />
          ))}
        </div>
      </div>
    )
  }

  if (!current) {
    return <EmptyState title="暂无日历数据" description="等待盈亏数据..." />
  }

  const daysInMonth = current.days_in_month || 28
  const firstDay = current.first_weekday || 0
  const weeks = ['日', '一', '二', '三', '四', '五', '六']

  const maxAbs = Math.max(
    1,
    ...Object.values(current.days || {}).map((v) => Math.abs(v))
  )

  const getDayStyle = (val: number) => {
    if (val === 0) return undefined
    const intensity = Math.min(Math.abs(val) / maxAbs, 1)
    const alpha = Math.max(0.08, intensity * 0.5)
    if (val > 0) {
      return { backgroundColor: `rgba(16,185,129,${alpha})`, color: '#34d399' }
    }
    return { backgroundColor: `rgba(239,68,68,${alpha})`, color: '#f87171' }
  }

  return (
    <div>
      {/* Header */}
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={() => setMonthIdx((i) => Math.max(0, i - 1))}
            disabled={monthIdx === 0}
            aria-label="上个月"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-white/5 disabled:opacity-30"
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
          <span className="min-w-[80px] text-center text-xs font-medium tabular-nums text-white">
            {current.year}年{current.month}月
          </span>
          <button
            onClick={() => setMonthIdx((i) => Math.min((months?.length || 1) - 1, i + 1))}
            disabled={monthIdx >= (months?.length || 1) - 1}
            aria-label="下个月"
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-white/5 disabled:opacity-30"
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
        <div className="flex items-center gap-3 text-[10px]">
          <span className="flex items-center gap-1 text-quant-green">
            <span className="inline-block h-2 w-2 rounded-sm bg-quant-green/20" />
            赢 {current.win_days}天
          </span>
          <span className="flex items-center gap-1 text-quant-red">
            <span className="inline-block h-2 w-2 rounded-sm bg-quant-red/20" />
            亏 {current.lose_days}天
          </span>
          <span className={cn('font-mono font-semibold', current.total >= 0 ? 'text-quant-green' : 'text-quant-red')}>
            {current.total >= 0 ? '+' : ''}${formatCurrency(current.total)}
          </span>
        </div>
      </div>

      {/* Weekday headers */}
      <div className="mb-1 grid grid-cols-7 gap-0.5">
        {weeks.map((w) => (
          <div key={w} className="py-1 text-center text-[10px] font-medium text-[#757575]">
            {w}
          </div>
        ))}
      </div>

      {/* Day grid */}
      <div className="grid grid-cols-7 gap-0.5">
        {Array.from({ length: firstDay }).map((_, i) => (
          <div key={`pad-${i}`} className="aspect-square" />
        ))}
        {Array.from({ length: daysInMonth }).map((_, i) => {
          const day = i + 1
          const key = String(day).padStart(2, '0')
          const val = current.days?.[key] ?? 0
          const style = getDayStyle(val)
          return (
            <button
              key={day}
              className={cn(
                'group relative flex aspect-square flex-col items-center justify-center rounded text-[10px] font-mono transition-all',
                'hover:ring-1 hover:ring-white/10'
              )}
              style={style}
              title={`${current.month_key}-${key}: ${val >= 0 ? '+' : ''}$${formatCurrency(val)}`}
            >
              <span className="font-medium text-[#555555] group-hover:text-white">{day}</span>
              {val !== 0 && (
                <span className="mt-0.5 text-[8px] opacity-70">
                  {Math.abs(val) >= 1000 ? `${(val / 1000).toFixed(1)}k` : Math.round(val)}
                </span>
              )}
            </button>
          )
        })}
      </div>
    </div>
  )
}

/* ── Main Page ───────────────────────────────────────────────────── */

export function Portfolio() {
  const { data: portfolio, isLoading: portfolioLoading } = useQuery({
    queryKey: ['portfolio-summary'],
    queryFn: () => portfolioApi.summary(),
    refetchInterval: 10000,
  })

  const { data: positionsData, isLoading: posLoading } = useQuery({
    queryKey: ['portfolio-positions'],
    queryFn: () => portfolioApi.positions(),
    refetchInterval: 5000,
  })

  const { data: snapshotsData, isLoading: snapLoading } = useQuery({
    queryKey: ['portfolio-snapshots'],
    queryFn: () => portfolioApi.snapshots(30),
    refetchInterval: 30000,
  })

  const { data: calendarData, isLoading: calLoading } = useQuery({
    queryKey: ['portfolio-calendar'],
    queryFn: () => portfolioApi.calendar(),
  })

  const { data: balanceData, isLoading: balanceLoading } = useQuery({
    queryKey: ['account-balance'],
    queryFn: () => accountApi.balance(),
    refetchInterval: 10000,
  })

  const positions: PositionItem[] = useMemo(() => {
    const raw = positionsData?.positions || positionsData || []
    return (Array.isArray(raw) ? raw : []).map((p: any) => ({
      symbol: p.symbol || '',
      quantity: p.quantity || 0,
      avg_entry_price: p.avg_entry_price || p.entry_price || 0,
      current_price: p.current_price || 0,
      unrealized_pnl: p.unrealized_pnl || 0,
      realized_pnl: p.realized_pnl || 0,
    }))
  }, [positionsData])

  const snapshots: SnapshotItem[] = useMemo(() => {
    const raw = snapshotsData?.snapshots || snapshotsData || []
    return (Array.isArray(raw) ? raw : []).map((s: any) => ({
      timestamp: s.timestamp || Date.now() / 1000,
      total_equity: s.total_equity || 0,
      drawdown: s.drawdown || 0,
    }))
  }, [snapshotsData])

  const calendarMonths: CalendarMonth[] = useMemo(() => {
    return calendarData?.months || []
  }, [calendarData])

  const balances = useMemo(() => {
    const raw = balanceData?.balances || balanceData?.currencies || []
    return (raw || []).map((b: any) => ({
      asset: b.asset || b.currency || '',
      free: b.free || b.available || 0,
      total: b.total || 0,
    }))
  }, [balanceData])

  const isLoading = portfolioLoading || balanceLoading
  const totalEquity = portfolio?.total_equity ?? 0
  const availableBalance = portfolio?.available_balance ?? 0
  const totalPnl = portfolio?.total_pnl ?? 0
  const marginUsed = portfolio?.margin_used ?? 0
  const drawdownPct = portfolio?.drawdown_pct ?? 0
  const positionCount = portfolio?.position_count ?? positions.length

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="space-y-5">
        <p className="text-xs text-muted-foreground">实时持仓、权益曲线与盈亏日历</p>

        {/* KPI Row */}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
          {isLoading ? (
            Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} variant="card" height="108px" />
            ))
          ) : (
            <>
              <KPICard
                icon={<Wallet className="h-4 w-4 text-amber-400" />}
                label="总资产估值"
                value={`$${formatCurrency(totalEquity)}`}
                trend="neutral"
                primary
              />
              <KPICard
                icon={<Wallet className="h-4 w-4 text-[#888888]" />}
                label="可用余额"
                value={`$${formatCurrency(availableBalance)}`}
                subValue={`${((availableBalance / Math.max(totalEquity, 1)) * 100).toFixed(1)}%`}
                subLabel="占比"
                trend="neutral"
              />
              <KPICard
                icon={totalPnl >= 0 ? <TrendingUp className="h-4 w-4 text-emerald-400" /> : <TrendingDown className="h-4 w-4 text-red-400" />}
                label="总盈亏"
                value={`${totalPnl >= 0 ? '+' : ''}$${formatCurrency(totalPnl)}`}
                trend={totalPnl >= 0 ? 'up' : 'down'}
                primary
              />
              <KPICard
                icon={<Activity className="h-4 w-4 text-[#888888]" />}
                label="已用保证金"
                value={`$${formatCurrency(marginUsed)}`}
                subValue={`${((marginUsed / Math.max(totalEquity, 1)) * 100).toFixed(1)}%`}
                subLabel="占比"
                trend="neutral"
              />
              <KPICard
                icon={<ShieldAlert className="h-4 w-4 text-red-400" />}
                label="当前回撤"
                value={`${drawdownPct.toFixed(2)}%`}
                subLabel="历史最大"
                trend="down"
              />
              <KPICard
                icon={<Layers className="h-4 w-4 text-[#888888]" />}
                label="持仓数量"
                value={String(positionCount)}
                trend="neutral"
              />
            </>
          )}
        </div>

        {/* Charts Row */}
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
          <SectionCard title="权益曲线">
            <EquityChart data={snapshots} isLoading={snapLoading} />
          </SectionCard>
          <SectionCard title="资产分布">
            <AssetPieChart balances={balances} isLoading={balanceLoading} />
          </SectionCard>
        </div>

        {/* Bottom Row: Positions + Calendar */}
        <div className="grid grid-cols-1 gap-5 xl:grid-cols-3">
          {/* Positions Table */}
          <SectionCard title="持仓明细" className="xl:col-span-2">
            {posLoading ? (
              <div className="space-y-2">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} variant="rect" height={40} />
                ))}
              </div>
            ) : positions.length > 0 ? (
              <div className="overflow-x-auto">
                <DataTable<PositionItem>
                  data={positions}
                  columns={[
                    { key: 'symbol', title: '币种', render: (p) => <span className="font-semibold text-white">{p.symbol}</span> },
                    { key: 'quantity', title: '持仓量', render: (p) => <span className="font-mono">{p.quantity.toFixed(4)}</span> },
                    { key: 'entry', title: '开仓价', render: (p) => <span className="font-mono text-muted-foreground">${formatCurrency(p.avg_entry_price)}</span> },
                    { key: 'current', title: '当前价', render: (p) => <span className="font-mono text-white">${formatCurrency(p.current_price || 0)}</span> },
                    { key: 'unrealized', title: '未实现盈亏', render: (p) => {
                      const pnl = p.unrealized_pnl || 0
                      const pnlPct = p.avg_entry_price && p.avg_entry_price > 0 ? (pnl / (p.avg_entry_price * p.quantity)) * 100 : 0
                      return (
                        <span className={cn('font-mono font-bold', pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                          {pnl >= 0 ? '+' : ''}${formatCurrency(pnl)} ({pnlPct >= 0 ? '+' : ''}{pnlPct.toFixed(2)}%)
                        </span>
                      )
                    }},
                    { key: 'realized', title: '已实现盈亏', render: (p) => (
                      <span className={cn('font-mono', (p.realized_pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                        {(p.realized_pnl || 0) >= 0 ? '+' : ''}${formatCurrency(p.realized_pnl || 0)}
                      </span>
                    )},
                  ]}
                  keyExtractor={(p) => p.symbol}
                />
              </div>
            ) : (
              <EmptyState title="暂无持仓" description="当前没有持仓数据" />
            )}
          </SectionCard>

          {/* Calendar */}
          <SectionCard title="盈亏日历">
            <ProfitCalendar months={calendarMonths} isLoading={calLoading} />
          </SectionCard>
        </div>
      </div>
    </div>
  )
}
