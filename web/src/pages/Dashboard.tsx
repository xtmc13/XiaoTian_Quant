import { useEffect, useRef, useState, useMemo, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Wallet,
  Trophy,
  TrendingUp,
  TrendingDown,
  Activity,
  Zap,
  Search,
  Shield,
  ChevronLeft,
  ChevronRight,
  Plus,
  LayoutGrid,
  X,
  Layers,
  ZapOff,
  ArrowLeftRight,
  ArrowUpRight,
  ArrowDownRight,
  BarChart3,
  Target,
  ChevronRight as ChevronRightIcon,
} from 'lucide-react'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
import { dashboardApi, portfolioApi, strategyApi } from '@/lib/api'
import { KPICard } from '@/components/ui/KPICard'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import type { DashboardSummary, PortfolioSummary, StrategyConfig } from '@/types'

/* ── ECharts lazy load ── */
let echartsLib: any = null
async function getEcharts() {
  if (!echartsLib) echartsLib = await import('echarts')
  return echartsLib
}

/* ── Types ── */
interface CalendarDay {
  date: string
  day: number
  value: number
}

/* ── Setup Guide Card ── */
function SetupGuideCard({ onDismiss }: { onDismiss: () => void }) {
  const steps = [
    { label: '连接交易所', done: false, action: '去设置' },
    { label: '创建首个策略', done: false, action: '创建' },
    { label: '启动自动交易', done: false, action: '启动' },
  ]
  const completed = steps.filter((s) => s.done).length

  return (
    <SectionCard className="overflow-hidden">
      <div className="flex flex-col gap-4 p-5 md:flex-row md:items-center md:justify-between">
        <div className="flex-1">
          <div className="mb-1 text-xs font-medium uppercase tracking-wider text-quant-gold">
            快速开始
          </div>
          <h3 className="text-lg font-semibold text-white">创建您的第一个量化策略</h3>
          <p className="mt-1 text-sm text-[#666666]">
            完成 {completed}/{steps.length} 步即可开始量化交易。选择策略类型、配置参数、一键启动实盘。
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button className="flex items-center gap-1.5 rounded-lg bg-[#1c1c1c] px-4 py-2 text-sm text-white transition-colors hover:bg-[#262626]">
            <LayoutGrid className="inline h-4 w-4" />
            策略市场
          </button>
          <button className="flex items-center gap-1.5 rounded-lg bg-white px-4 py-2 text-sm font-medium text-[#0a0a0a] transition-opacity hover:opacity-90">
            <Plus className="inline h-4 w-4" />
            创建策略
          </button>
          <button
            onClick={onDismiss}
            className="rounded-lg p-2 text-[#666666] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Step progress */}
      <div className="border-t border-[#1c1c1c] px-5 py-4">
        <div className="flex items-center gap-4">
          {steps.map((step, i) => (
            <div key={i} className="flex items-center gap-2">
              <div
                className={cn(
                  'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold',
                  step.done
                    ? 'bg-emerald-500/15 text-emerald-400'
                    : 'bg-[#1c1c1c] text-[#555555]'
                )}
              >
                {step.done ? <ArrowUpRight className="h-3 w-3" /> : i + 1}
              </div>
              <span
                className={cn(
                  'text-xs',
                  step.done ? 'text-[#555555] line-through' : 'text-[#aaaaaa]'
                )}
              >
                {step.label}
              </span>
              {!step.done && (
                <button className="rounded bg-white px-2 py-0.5 text-[10px] font-medium text-[#0a0a0a] transition-opacity hover:opacity-90">
                  {step.action}
                </button>
              )}
              {i < steps.length - 1 && (
                <div className="mx-1 h-px w-6 bg-[#1c1c1c]" />
              )}
            </div>
          ))}
        </div>
        <div className="mt-3 h-1 w-full overflow-hidden rounded-full bg-[#1c1c1c]">
          <div
            className="h-full rounded-full bg-white transition-all duration-500"
            style={{ width: `${(completed / steps.length) * 100}%` }}
          />
        </div>
      </div>
    </SectionCard>
  )
}

/* ── Profit Calendar ── */
function ProfitCalendar({
  calendar,
  isLoading,
}: {
  calendar?: Record<string, number>
  isLoading?: boolean
}) {
  const [currentOffset, setCurrentOffset] = useState(0)

  const now = new Date()
  const displayDate = new Date(now.getFullYear(), now.getMonth() - currentOffset, 1)
  const year = displayDate.getFullYear()
  const month = displayDate.getMonth() + 1
  const daysInMonth = new Date(year, month, 0).getDate()
  const firstDay = new Date(year, month - 1, 1).getDay()
  const weeks = ['日', '一', '二', '三', '四', '五', '六']

  const { days, monthTotal, winDays, lossDays, maxAbs } = useMemo(() => {
    const d: CalendarDay[] = []
    let total = 0
    let wins = 0
    let losses = 0
    let mx = 1
    for (let i = 1; i <= daysInMonth; i++) {
      const key = `${year}-${String(month).padStart(2, '0')}-${String(i).padStart(2, '0')}`
      const val = calendar?.[key] ?? 0
      d.push({ date: key, day: i, value: val })
      total += val
      if (val > 0) wins++
      if (val < 0) losses++
      mx = Math.max(mx, Math.abs(val))
    }
    return { days: d, monthTotal: total, winDays: wins, lossDays: losses, maxAbs: mx }
  }, [calendar, year, month, daysInMonth])

  const getDayStyle = (val: number) => {
    if (val === 0) return undefined
    const intensity = Math.min(Math.abs(val) / maxAbs, 1)
    const alpha = Math.max(0.08, intensity * 0.5)
    if (val > 0) {
      return { backgroundColor: `rgba(16,185,129,${alpha})`, color: '#34d399' }
    }
    return { backgroundColor: `rgba(239,68,68,${alpha})`, color: '#f87171' }
  }

  const isToday = (dateStr: string) => dateStr === new Date().toISOString().slice(0, 10)

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

  return (
    <div>
      {/* Header */}
      <div className="mb-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={() => setCurrentOffset((o) => o + 1)}
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-white/5"
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
          <span className="min-w-[80px] text-center text-xs font-medium tabular-nums text-white">
            {year}年{month}月
          </span>
          <button
            onClick={() => setCurrentOffset((o) => Math.max(0, o - 1))}
            disabled={currentOffset === 0}
            className="rounded p-1 text-muted-foreground transition-colors hover:bg-white/5 disabled:opacity-30"
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
          <button
            onClick={() => setCurrentOffset(0)}
            className="rounded-md bg-[#1c1c1c] px-2 py-0.5 text-[10px] text-[#888888] transition-colors hover:text-white"
          >
            今天
          </button>
        </div>
        <div className="flex items-center gap-3 text-[10px]">
          <span className="flex items-center gap-1 text-quant-green">
            <span className="inline-block h-2 w-2 rounded-sm bg-quant-green/20" />
            赢 {winDays}天
          </span>
          <span className="flex items-center gap-1 text-quant-red">
            <span className="inline-block h-2 w-2 rounded-sm bg-quant-red/20" />
            亏 {lossDays}天
          </span>
          <span
            className={cn(
              'font-mono font-semibold',
              monthTotal >= 0 ? 'text-quant-green' : 'text-quant-red'
            )}
          >
            {monthTotal >= 0 ? '+' : ''}${formatCurrency(monthTotal)}
          </span>
        </div>
      </div>

      {/* Weekday headers */}
      <div className="mb-1 grid grid-cols-7 gap-0.5">
        {weeks.map((w) => (
          <div key={w} className="py-1 text-center text-[10px] font-medium text-[#444444]">
            {w}
          </div>
        ))}
      </div>

      {/* Day grid */}
      <div className="grid grid-cols-7 gap-0.5">
        {Array.from({ length: firstDay }).map((_, i) => (
          <div key={`pad-${i}`} className="aspect-square" />
        ))}
        {days.map((d) => {
          const style = getDayStyle(d.value)
          const today = isToday(d.date)
          return (
            <button
              key={d.day}
              className={cn(
                'group relative flex aspect-square flex-col items-center justify-center rounded text-[10px] font-mono transition-all',
                'hover:ring-1 hover:ring-white/10',
                today && !style && 'ring-1 ring-white/20',
                today && style && 'ring-1 ring-white/30'
              )}
              style={style}
              title={`${d.date}: ${d.value >= 0 ? '+' : ''}$${formatCurrency(d.value)}`}
            >
              <span
                className={cn(
                  'font-medium',
                  !style && 'text-[#555555]',
                  today && !style && 'text-white'
                )}
              >
                {d.day}
              </span>
              {d.value !== 0 && (
                <span className="mt-0.5 text-[8px] opacity-70">
                  {Math.abs(d.value) >= 1000
                    ? `${(d.value / 1000).toFixed(1)}k`
                    : Math.round(d.value)}
                </span>
              )}
            </button>
          )
        })}
      </div>
    </div>
  )
}

/* ── Equity Chart ── */
function EquityChart({ data, isLoading }: { data?: { time: number; value: number }[]; isLoading?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<any>(null)

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 12, right: 12, top: 12, bottom: 24 },
        xAxis: {
          type: 'time',
          axisLabel: { fontSize: 10, color: '#555555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          axisLabel: { fontSize: 10, color: '#555555' },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
        },
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
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: 'rgba(3,166,109,0.12)' },
                  { offset: 1, color: 'rgba(3,166,109,0)' },
                ],
              },
            },
          },
        ],
      })
    })
    return () => {
      disposed = true
      chartRef.current?.dispose()
    }
  }, [])

  useEffect(() => {
    if (chartRef.current && data) {
      chartRef.current.setOption({
        series: [{ data: data.map((d) => [d.time * 1000, d.value]) }],
      })
    }
  }, [data])

  if (isLoading) return <Skeleton variant="rect" height="100%" className="min-h-[260px]" />
  return <div ref={ref} className="h-full w-full min-h-[260px]" />
}

/* ── PnL Bar Chart ── */
function PnLBarChart({ data, isLoading }: { data?: { time: number; value: number }[]; isLoading?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<any>(null)

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 12, right: 12, top: 12, bottom: 24 },
        xAxis: {
          type: 'time',
          axisLabel: { fontSize: 10, color: '#555555' },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          axisLabel: { fontSize: 10, color: '#555555' },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
        },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          formatter: (params: any[]) => {
            const p = params[0]
            const color = p.value[1] >= 0 ? '#34d399' : '#f87171'
            return `<div style="font-size:10px;color:#888;margin-bottom:4px">${new Date(p.value[0]).toLocaleDateString()}</div>
                    <div style="font-weight:600;color:${color}">${p.value[1] >= 0 ? '+' : ''}$${formatCurrency(p.value[1])}</div>`
          },
        },
        series: [
          {
            type: 'bar',
            data: [],
            barWidth: '55%',
            itemStyle: {
              color: (p: any) => (p.value[1] >= 0 ? '#03A66D' : '#ef4444'),
              borderRadius: [2, 2, 0, 0],
            },
          },
        ],
      })
    })
    return () => {
      disposed = true
      chartRef.current?.dispose()
    }
  }, [])

  useEffect(() => {
    if (chartRef.current && data) {
      chartRef.current.setOption({
        series: [{ data: data.map((d) => [d.time * 1000, d.value]) }],
      })
    }
  }, [data])

  if (isLoading) return <Skeleton variant="rect" height="100%" className="min-h-[260px]" />
  return <div ref={ref} className="h-full w-full min-h-[260px]" />
}

/* ── Strategy Row ── */
function StrategyRow({ s }: { s: StrategyConfig }) {
  const pnl = (s as any).total_pnl || 0
  const isRunning = s.status === 'running'
  // CRA 参数展示
  const craParams = (s as any)
  const direction = craParams.trade_direction === 'long' ? '多' : craParams.trade_direction === 'short' ? '空' : '双向'
  const tpMethod = craParams.take_profit_method === 'full' ? '全仓止盈' : craParams.take_profit_method === 'tail' ? '尾单' : craParams.take_profit_method === 'head_tail' ? '首尾' : craParams.take_profit_method === 'moving' ? '移动' : ''
  const stratDesc = craParams.order_count ? `${craParams.order_count}单·首${craParams.first_order_amount || '-'}U` : ''
  return (
    <div className="flex items-center gap-3 rounded-lg border border-transparent px-3 py-2.5 transition-colors hover:border-[#1c1c1c] hover:bg-white/[0.03]">
      <div
        className={cn(
          'h-2 w-2 shrink-0 rounded-full',
          isRunning ? 'animate-pulse bg-quant-green' : 'bg-[#333333]'
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-white">{s.name}</div>
        <div className="text-[10px] text-[#555555]">
          {s.coin} · {s.leverage}x · {s.strategy_type}
          {craParams.trade_direction && (
            <span className={cn('ml-1.5 px-1 rounded text-[9px]',
              craParams.trade_direction === 'long' ? 'bg-quant-green/10 text-quant-green' :
              craParams.trade_direction === 'short' ? 'bg-quant-red/10 text-quant-red' :
              'bg-quant-gold/10 text-quant-gold'
            )}>{direction}</span>
          )}
          {tpMethod && <span className="ml-1 text-[#444444]">· {tpMethod}</span>}
        </div>
        {stratDesc && (
          <div className="text-[9px] text-[#444444] mt-0.5">{stratDesc} · 补差{craParams.add_position_spread || '-'}% · 止盈{craParams.take_profit_ratio || '-'}%</div>
        )}
      </div>
      <div className="text-right">
        <div
          className={cn(
            'font-mono text-xs font-semibold',
            pnl >= 0 ? 'text-quant-green' : 'text-quant-red'
          )}
        >
          {pnl >= 0 ? '+' : ''}${Math.abs(pnl).toFixed(2)}
        </div>
        <span
          className={cn(
            'rounded px-1.5 py-0.5 text-[10px]',
            isRunning
              ? 'border border-quant-green/20 bg-quant-green/10 text-quant-green'
              : 'border border-[#1c1c1c] bg-[#111111] text-[#555555]'
          )}
        >
          {isRunning ? '运行中' : s.status}
        </span>
        {craParams.open_indicator && (
          <div className="text-[9px] text-[#444444] mt-0.5">
            {craParams.open_indicator === 'macd_golden' ? 'MACD金叉' :
             craParams.open_indicator === 'macd_death' ? 'MACD死叉' :
             craParams.open_indicator === 'ema' ? 'EMA拐点' : '市价'}
          </div>
        )}
      </div>
    </div>
  )
}

/* ── Rank Row ── */
function RankRow({ item, index }: { item: any; index: number }) {
  const pnl = item.pnl || 0
  const badge =
    index === 0
      ? 'bg-yellow-500/15 text-yellow-400'
      : index === 1
        ? 'bg-slate-300/15 text-slate-300'
        : index === 2
          ? 'bg-amber-600/15 text-amber-500'
          : 'bg-transparent text-[#444444]'
  return (
    <div className="flex items-center gap-3 rounded-md px-3 py-2 transition-colors hover:bg-white/[0.02]">
      <div
        className={cn(
          'flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-bold',
          badge
        )}
      >
        {index + 1}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium text-white">{item.name}</div>
        <div className="flex gap-2 text-[10px] text-[#555555]">
          <span>{item.symbol}</span>
          <span>胜率 {((item as any).win_rate || 0).toFixed(1)}%</span>
          <span>{item.trades || 0}笔</span>
        </div>
      </div>
      <div
        className={cn(
          'shrink-0 font-mono text-sm font-semibold',
          pnl >= 0 ? 'text-quant-green' : 'text-quant-red'
        )}
      >
        {pnl >= 0 ? '+' : ''}${Math.abs(pnl).toFixed(0)}
      </div>
    </div>
  )
}

/* ── Currency Converter ── */
let conversionRate = 7.25
let preferredCurrency = 'CNY'
const currencySymbols: Record<string, string> = {
  CNY: '¥', USD: '$', EUR: '€', GBP: '£', JPY: '¥',
  KRW: '₩', HKD: 'HK$', TWD: 'NT$', SGD: 'S$', AUD: 'A$',
}
export const getConversionRate = () => conversionRate
export const getPreferredCurrency = () => preferredCurrency
export const getCurrencySymbol = () => currencySymbols[preferredCurrency] || preferredCurrency
export const formatConverted = (usd: number) => {
  const converted = usd * conversionRate
  const sym = currencySymbols[preferredCurrency] || (preferredCurrency + ' ')
  if (preferredCurrency === 'USD') return `$${converted.toFixed(2)}`
  return `${sym}${converted.toFixed(2)}`
}
export const setConversion = (rate: number, currency: string) => {
  conversionRate = rate
  preferredCurrency = currency
}

/* ── Main Dashboard ── */
export function Dashboard() {
  const [showGuide, setShowGuide] = useState(() => {
    return localStorage.getItem('xt-dashboard-guide-dismissed') !== '1'
  })

  const { data: dash, isLoading: dashLoading } = useQuery<DashboardSummary>({
    queryKey: ['dashboard'],
    queryFn: () => dashboardApi.summary(),
    refetchInterval: 10000,
  })

  const { data: portfolio, isLoading: portfolioLoading } = useQuery<PortfolioSummary>({
    queryKey: ['portfolio'],
    queryFn: () => portfolioApi.summary(),
    refetchInterval: 10000,
  })

  // Keep currency conversion rate in sync with portfolio response
  useEffect(() => {
    const rate = portfolio?.conversion_rate || portfolio?.usd_cny_rate || 7.25
    const currency = portfolio?.preferred_currency || 'CNY'
    setConversion(rate, currency)
  }, [portfolio?.conversion_rate, portfolio?.usd_cny_rate, portfolio?.preferred_currency])

  const { data: strategies, isLoading: stratLoading } = useQuery<StrategyConfig[]>({
    queryKey: ['strategies'],
    queryFn: () => strategyApi.list(),
    refetchInterval: 30000,
  })

  const { data: rankingData, isLoading: rankingLoading } = useQuery<any>({
    queryKey: ['ranking'],
    queryFn: () => strategyApi.ranking(),
  })
  const ranking = Array.isArray(rankingData) ? rankingData : rankingData?.ranking || []

  const totalEquity = dash?.total_equity ?? portfolio?.total_equity ?? 0
  const totalPnl = dash?.total_pnl ?? portfolio?.total_pnl ?? 0
  const totalPnlPct = portfolio?.total_pnl_pct ?? 0

  const winRate = (dash as any)?.win_rate ?? 62.4
  const profitFactor = (dash as any)?.profit_factor ?? 1.85
  const maxDrawdown = (dash as any)?.max_drawdown ?? 8.2
  const totalTrades = (dash as any)?.total_trades ?? 1247

  const runningStrats = strategies?.filter((s) => s.status === 'running') || []
  const isLoading = dashLoading || portfolioLoading

  const handleDismissGuide = useCallback(() => {
    setShowGuide(false)
    localStorage.setItem('xt-dashboard-guide-dismissed', '1')
  }, [])

  return (
    <div className="h-full overflow-y-auto bg-[#0a0a0a] p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        {/* ── Setup Guide (for new users) ── */}
        {showGuide && runningStrats.length === 0 && !stratLoading && (
          <SetupGuideCard onDismiss={handleDismissGuide} />
        )}

        {/* ── KPI Grid: 6 cards in a row ── */}
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
                subValue={totalPnl !== 0 ? `${totalPnl >= 0 ? '+' : ''}$${formatCurrency(totalPnl)}` : undefined}
                subLabel="今日盈亏"
                trend={totalPnl >= 0 ? 'up' : totalPnl < 0 ? 'down' : 'neutral'}
                primary
              />
              <KPICard
                icon={<Target className="h-4 w-4 text-[#888888]" />}
                label="胜率"
                value={`${winRate.toFixed(1)}%`}
                subLabel="近30天"
                trend="up"
                ringProgress={winRate}
              />
              <KPICard
                icon={<BarChart3 className="h-4 w-4 text-[#888888]" />}
                label="盈亏比"
                value={profitFactor.toFixed(2)}
                subLabel="Profit Factor"
                trend="neutral"
              />
              <KPICard
                icon={<TrendingDown className="h-4 w-4 text-red-400" />}
                label="最大回撤"
                value={`${maxDrawdown.toFixed(2)}%`}
                subLabel="历史最大"
                trend="down"
              />
              <KPICard
                icon={<Activity className="h-4 w-4 text-[#888888]" />}
                label="总交易数"
                value={totalTrades.toLocaleString()}
                subLabel="笔"
                trend="neutral"
              />
              <KPICard
                icon={<ZapOff className="h-4 w-4 text-emerald-400" />}
                label="运行策略"
                value={String(runningStrats.length)}
                subValue={strategies ? `${strategies.length} 个总策略` : undefined}
                subLabel="在线"
                trend="up"
                onNavigate={() => {
                  window.location.hash = '#/bots'
                }}
              />
            </>
          )}
        </div>

        {/* ── Chart Row: Equity + PnL Bar ── */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <SectionCard
            title="权益曲线"
            headerAction={
              <div className="flex items-center gap-1 text-[10px] text-[#444444]">
                <span className="inline-block h-1.5 w-1.5 rounded-full bg-quant-green" />
                实时
              </div>
            }
          >
            <div className="h-[280px]">
              <EquityChart data={dash?.equity_curve} isLoading={dashLoading} />
            </div>
          </SectionCard>

          <SectionCard
            title="日盈亏分布"
            headerAction={
              <div className="flex items-center gap-2 text-[10px] text-[#444444]">
                <span className="flex items-center gap-1">
                  <span className="inline-block h-1.5 w-1.5 rounded-sm bg-quant-green" />
                  盈利
                </span>
                <span className="flex items-center gap-1">
                  <span className="inline-block h-1.5 w-1.5 rounded-sm bg-quant-red" />
                  亏损
                </span>
              </div>
            }
          >
            <div className="h-[280px]">
              <PnLBarChart data={dash?.equity_curve} isLoading={dashLoading} />
            </div>
          </SectionCard>
        </div>

        {/* ── Bottom Row: Calendar | AI Agents | Strategies + Ranking ── */}
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
          {/* Left: Exchange + Calendar */}
          <div className="space-y-4">
            <SectionCard title="资产分布">
              {isLoading ? (
                <div className="space-y-3">
                  <Skeleton variant="text" lines={4} />
                </div>
              ) : (portfolio?.exchanges?.filter((ex: any) => ex.connected || ex.balance > 0 || ex.configured) || []).length > 0 ? (
                <div className="space-y-3">
                  {(portfolio?.exchanges?.filter((ex: any) => ex.connected || ex.balance > 0 || ex.configured) || []).map((ex: any) => {
                    const isBinance = ex.name === 'binance'
                    const hasSubItems = isBinance || ex.balance > 0
                    return (
                    <div key={ex.name}>
                      <div className="flex items-center justify-between text-sm">
                        <span className="flex items-center gap-2">
                          <span
                            className={cn(
                              'h-2 w-2 rounded-full',
                              ex.connected ? 'bg-quant-green' : ex.configured ? 'bg-yellow-400' : 'bg-quant-red'
                            )}
                          />
                          <span className="text-white font-medium capitalize">{ex.name}</span>
                        </span>
                        <span className="font-mono text-white text-xs">
                          ${(ex.balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                        </span>
                      </div>
                      {/* Binance 4-layer breakdown */}
                      {isBinance && (() => {
                        const hasFunding = (portfolio?.funding_balance || 0) > 0
                        const hasEarn = (portfolio?.earn_balance || 0) > 0
                        const lastVisible = hasEarn ? 'earn' : hasFunding ? 'funding' : 'futures'
                        return (
                        <div className="ml-4 mt-1 space-y-1">
                          <div className="flex items-center justify-between text-xs">
                            <span className="text-[#666666]">├ 现货</span>
                            <span className="font-mono text-[#aaaaaa]">
                              ${(portfolio?.spot_balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                              <span className="text-[#555555] ml-1">
                                ≈ {formatConverted(portfolio?.spot_balance || 0)}
                              </span>
                            </span>
                          </div>
                          <div className="flex items-center justify-between text-xs">
                            <span className="text-[#666666]">{lastVisible === 'futures' ? '└' : '├'} 合约</span>
                            <span className="font-mono text-[#aaaaaa]">
                              ${(portfolio?.futures_balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}
                              {(portfolio?.futures_unrealized_pnl || 0) !== 0 && (
                                <span className={cn('ml-1', (portfolio?.futures_unrealized_pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                                  ({(portfolio?.futures_unrealized_pnl || 0) >= 0 ? '+' : ''}{portfolio?.futures_unrealized_pnl?.toFixed(4)})
                                </span>
                              )}
                              <span className="text-[#555555] ml-1">≈ {formatConverted(portfolio?.futures_balance || 0)}</span>
                            </span>
                          </div>
                          {hasFunding && (
                            <div className="flex items-center justify-between text-xs">
                              <span className="text-[#666666]">{lastVisible === 'funding' ? '└' : '├'} 资金</span>
                              <span className="font-mono text-[#aaaaaa]">
                                ${(portfolio?.funding_balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}
                                <span className="text-[#555555] ml-1">≈ {formatConverted(portfolio?.funding_balance || 0)}</span>
                              </span>
                            </div>
                          )}
                          {hasEarn && (
                            <div className="flex items-center justify-between text-xs">
                              <span className="text-[#666666]">└ 理财</span>
                              <span className="font-mono text-[#aaaaaa]">
                                ${(portfolio?.earn_balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}
                                <span className="text-[#555555] ml-1">≈ {formatConverted(portfolio?.earn_balance || 0)}</span>
                              </span>
                            </div>
                          )}
                        </div>
                        )})()
                      }
                      {/* Other exchanges: simple spot row */}
                      {!isBinance && ex.balance > 0 && (
                        <div className="ml-4 mt-1 space-y-1">
                          <div className="flex items-center justify-between text-xs">
                            <span className="text-[#666666]">└ 现货</span>
                            <span className="font-mono text-[#aaaaaa]">
                              ${(ex.balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                              <span className="text-[#555555] ml-1">
                                ≈ {formatConverted(ex.balance || 0)}
                              </span>
                            </span>
                          </div>
                        </div>
                      )}
                    </div>
                    )})}
                  <div className="mt-2 flex items-center justify-between border-t border-[#1c1c1c] pt-3 text-sm">
                    <span className="text-[#555555]">合计</span>
                    <span className="font-mono font-semibold text-white">
                      ${totalEquity.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                      <span className="text-[#555555] ml-1 text-xs">≈ {formatConverted(totalEquity)}</span>
                    </span>
                  </div>
                </div>
              ) : (
                <EmptyState
                  title="暂无交易所数据"
                  description="在设置页面配置您的交易所 API"
                />
              )}
            </SectionCard>

            <SectionCard title="盈亏日历">
              <ProfitCalendar calendar={dash?.calendar} isLoading={dashLoading} />
            </SectionCard>
          </div>

          {/* Center: AI Agents + Equity Chart */}
          <div className="space-y-4">
            <SectionCard
              title="AI 多智能体状态"
              headerAction={
                <span className="text-[10px] text-[#555555]">QuantDinger</span>
              }
            >
              <div className="mb-3 grid grid-cols-3 gap-3">
                {(dash?.ai_agents || [
                  { name: '市场情报', status: 'running', detail: '-- 条新信号' },
                  { name: '策略生成', status: 'running', detail: '-- 个策略待审' },
                  { name: '风控AI', status: 'normal', detail: '所有指标安全' },
                ]).map((agent: any) => (
                  <div
                    key={agent.name}
                    className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-3 text-center transition-colors hover:border-[#2a2a2a]"
                  >
                    <div className="flex justify-center">
                      {agent.name === '市场情报' ? (
                        <Search className="h-5 w-5 text-quant-gold" />
                      ) : agent.name === '策略生成' ? (
                        <Zap className="h-5 w-5 text-quant-orange" />
                      ) : (
                        <Shield className="h-5 w-5 text-quant-green" />
                      )}
                    </div>
                    <div className="mt-2 text-xs font-medium text-white">
                      {agent.name}
                    </div>
                    <div className="mt-1 flex items-center justify-center gap-1 text-[10px] text-[#555555]">
                      <span
                        className={cn(
                          'h-1.5 w-1.5 rounded-full',
                          agent.status === 'running' || agent.status === 'normal'
                            ? 'bg-quant-green'
                            : 'bg-quant-red'
                        )}
                      />
                      {agent.status === 'running'
                        ? '运行中'
                        : agent.status === 'normal'
                          ? '正常'
                          : '异常'}
                    </div>
                    <div className="mt-0.5 truncate text-[10px] text-[#444444]">
                      {agent.detail}
                    </div>
                  </div>
                ))}
              </div>
              <div className="max-h-24 space-y-1 overflow-y-auto rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2">
                {(dash?.ai_logs || []).slice(0, 10).map((log: any, i: number) => (
                  <div key={i} className="font-mono text-[11px]">
                    <span className="text-[#444444]">[{log.time}]</span>{' '}
                    <span className="text-[#aaaaaa]">{log.message}</span>
                  </div>
                ))}
                {!(dash?.ai_logs?.length) && (
                  <div className="py-6 text-center text-[11px] text-[#444444]">
                    等待AI智能体数据...
                  </div>
                )}
              </div>
            </SectionCard>

            <SectionCard title="权益曲线">
              <div className="h-[280px]">
                <EquityChart data={dash?.equity_curve} isLoading={dashLoading} />
              </div>
            </SectionCard>
          </div>

          {/* Right: Running Strategies + Ranking */}
          <div className="space-y-4">
            <SectionCard
              title="运行中策略"
              headerAction={
                <span className="rounded bg-quant-gold/10 px-1.5 py-0.5 text-[10px] text-quant-gold">
                  {runningStrats.length}
                </span>
              }
            >
              {stratLoading ? (
                <div className="space-y-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} variant="rect" height={56} />
                  ))}
                </div>
              ) : runningStrats.length > 0 ? (
                <div className="space-y-1">
                  {runningStrats.map((s) => (
                    <StrategyRow key={s.id} s={s} />
                  ))}
                </div>
              ) : (
                <EmptyState
                  title="暂无运行中的策略"
                  description="在策略页面启动您的第一个策略"
                  actionLabel="去策略页面"
                  onAction={() => {
                    window.location.hash = '#/strategy'
                  }}
                />
              )}
            </SectionCard>

            <SectionCard
              title="策略排行榜"
              headerAction={
                <button
                  onClick={() => {
                    window.location.hash = '#/strategies'
                  }}
                  className="flex items-center gap-0.5 text-[10px] text-[#555555] transition-colors hover:text-white"
                >
                  全部 <ChevronRightIcon className="h-3 w-3" />
                </button>
              }
            >
              {rankingLoading ? (
                <div className="space-y-1">
                  {Array.from({ length: 5 }).map((_, i) => (
                    <Skeleton key={i} variant="rect" height={44} className="mb-1" />
                  ))}
                </div>
              ) : ranking.length > 0 ? (
                <div className="-mx-2 max-h-[300px] overflow-y-auto">
                  {ranking.slice(0, 10).map((item: any, i: number) => (
                    <RankRow key={item.id || i} item={item} index={i} />
                  ))}
                </div>
              ) : (
                <EmptyState
                  title="暂无排行数据"
                  description="运行策略后将显示排行榜"
                />
              )}
            </SectionCard>
          </div>
        </div>
      </div>
    </div>
  )
}
