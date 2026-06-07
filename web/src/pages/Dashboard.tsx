import { useEffect, useRef, useState, useMemo, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  Wallet,
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
  ZapOff,
  ArrowUpRight,
  BarChart3,
  Target,
  ChevronRight as ChevronRightIcon,
  Brain,
} from 'lucide-react'
import { cn, formatCurrency, formatPercent, formatConverted, setConversion } from '@/lib/utils'
import { dashboardApi, portfolioApi, strategyApi, protectionApi, mlApi } from '@/lib/api'
import { getEcharts } from '@/lib/echarts'
import { KPICard } from '@/components/ui/KPICard'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { DataTable } from '@/components/DataTable'
import type { ECharts } from 'echarts'
import type { StrategyRanking } from '@/types'
import type { DashboardSummary, PortfolioSummary, StrategyItem } from '@/types'

/* ── Types ── */
interface CalendarDay {
  date: string
  day: number
  value: number
}

interface ProtectionStatus {
  global_blocked: boolean
  global_reason?: string
  global_resume_in?: string
  pair_blocks: Record<string, {
    reason: string
    resume_in: string
    permanent: boolean
  }>
}

interface ModelInfo {
  model_id: string
  model_type: string
  task_type: string
  trained_at: string
  metrics: Record<string, number>
  feature_count: number
}

/* ── Risk Control Card ── */
function RiskControlCard({ status, isLoading }: { status?: ProtectionStatus; isLoading: boolean }) {
  const navigate = useNavigate()
  if (isLoading) {
    return (
      <SectionCard title="风控状态">
        <div className="space-y-3">
          <Skeleton variant="text" lines={3} />
          <Skeleton variant="rect" height={48} />
        </div>
      </SectionCard>
    )
  }

  const isGloballyBlocked = status?.global_blocked ?? false
  const pairBlocks = status?.pair_blocks ? Object.entries(status.pair_blocks) : []
  const blockedCount = pairBlocks.length + (isGloballyBlocked ? 1 : 0)
  const lastReason = status?.global_reason || (pairBlocks[0]?.[1]?.reason) || '-'

  const ruleItems = [
    {
      name: '全局交易保护',
      status: isGloballyBlocked ? ('blocked' as const) : ('normal' as const),
      detail: isGloballyBlocked ? (status?.global_reason || '交易暂停') : '正常运行',
    },
    ...pairBlocks.map(([pair, info]) => ({
      name: pair,
      status: 'blocked' as const,
      detail: info.reason,
    })),
  ]

  return (
    <SectionCard
      title="风控状态"
      headerAction={
        <button
          onClick={() => { navigate('/risk-control') }}
          className="flex items-center gap-0.5 text-[10px] text-[#8a8a8a] transition-colors hover:text-white"
        >
          查看风控中心 <ChevronRightIcon className="h-3 w-3" />
        </button>
      }
    >
      <div className="mb-3 grid grid-cols-3 gap-2">
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2.5 text-center">
          <div className="text-[10px] text-[#8a8a8a]">活跃规则</div>
          <div className={cn('mt-1 text-sm font-semibold', blockedCount > 0 ? 'text-quant-red' : 'text-quant-green')}>
            {ruleItems.length}
          </div>
        </div>
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2.5 text-center">
          <div className="text-[10px] text-[#8a8a8a]">阻断交易对</div>
          <div className={cn('mt-1 text-sm font-semibold', pairBlocks.length > 0 ? 'text-quant-red' : 'text-quant-green')}>
            {pairBlocks.length}
          </div>
        </div>
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2.5 text-center">
          <div className="text-[10px] text-[#8a8a8a]">最近触发</div>
          <div className="mt-1 truncate text-sm font-semibold text-[#aaaaaa]" title={lastReason}>
            {lastReason}
          </div>
        </div>
      </div>

      <div className="space-y-1.5">
        {ruleItems.slice(0, 5).map((rule) => (
          <div
            key={rule.name}
            className="flex items-center justify-between rounded-md border border-[#1c1c1c] bg-[#0a0a0a] px-2.5 py-1.5"
          >
            <div className="flex items-center gap-2 min-w-0">
              <span
                className={cn(
                  'h-2 w-2 shrink-0 rounded-full',
                  rule.status === 'normal' ? 'bg-quant-green' : 'bg-quant-red'
                )}
              />
              <span className="truncate text-xs text-white">{rule.name}</span>
            </div>
            <span className={cn('shrink-0 text-[10px]', rule.status === 'normal' ? 'text-quant-green' : 'text-quant-red')}>
              {rule.detail}
            </span>
          </div>
        ))}
        {ruleItems.length === 0 && (
          <div className="py-4 text-center text-[11px] text-[#8a8a8a]">
            暂无风控数据
          </div>
        )}
      </div>
    </SectionCard>
  )
}

/* ── ML Status Card ── */
function MLStatusCard({ health, models, isLoading }: { health?: { status: string }; models?: ModelInfo[]; isLoading: boolean }) {
  const navigate = useNavigate()
  if (isLoading) {
    return (
      <SectionCard title="ML 模型状态">
        <div className="space-y-3">
          <Skeleton variant="text" lines={3} />
          <Skeleton variant="rect" height={48} />
        </div>
      </SectionCard>
    )
  }

  const isHealthy = health?.status === 'healthy'
  const modelCount = models?.length ?? 0
  const latestModel = models && models.length > 0
    ? [...models].sort((a, b) => new Date(b.trained_at).getTime() - new Date(a.trained_at).getTime())[0]
    : undefined

  const pipelineHealth = isHealthy ? 'normal' : 'error'
  const modelStatus = !isHealthy ? 'error' : latestModel ? 'deployed' : 'idle'

  const statusColor = (s: string) => {
    if (s === 'normal' || s === 'deployed' || s === 'healthy') return 'text-quant-green bg-quant-green/10 border-quant-green/20'
    if (s === 'warning' || s === 'idle') return 'text-quant-gold bg-quant-gold/10 border-quant-gold/20'
    return 'text-quant-red bg-quant-red/10 border-quant-red/20'
  }

  return (
    <SectionCard
      title="ML 模型状态"
      headerAction={
        <button
          onClick={() => { navigate('/model-management') }}
          className="flex items-center gap-0.5 text-[10px] text-[#8a8a8a] transition-colors hover:text-white"
        >
          查看模型管理 <ChevronRightIcon className="h-3 w-3" />
        </button>
      }
    >
      <div className="mb-3 grid grid-cols-3 gap-2">
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2.5 text-center">
          <div className="text-[10px] text-[#8a8a8a]">模型数量</div>
          <div className="mt-1 text-sm font-semibold text-white">{modelCount}</div>
        </div>
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2.5 text-center">
          <div className="text-[10px] text-[#8a8a8a]">最新状态</div>
          <div className={cn('mt-1 text-sm font-semibold', modelStatus === 'deployed' ? 'text-quant-green' : modelStatus === 'idle' ? 'text-quant-gold' : 'text-quant-red')}>
            {modelStatus === 'deployed' ? '已部署' : modelStatus === 'idle' ? '空闲' : '异常'}
          </div>
        </div>
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2.5 text-center">
          <div className="text-[10px] text-[#8a8a8a]">特征管道</div>
          <div className={cn('mt-1 text-sm font-semibold', pipelineHealth === 'normal' ? 'text-quant-green' : 'text-quant-red')}>
            {pipelineHealth === 'normal' ? '正常' : '异常'}
          </div>
        </div>
      </div>

      <div className="space-y-1.5">
        <div className="flex items-center justify-between rounded-md border border-[#1c1c1c] bg-[#0a0a0a] px-2.5 py-1.5">
          <div className="flex items-center gap-2">
            <Brain className="h-3.5 w-3.5 text-[#888888]" />
            <span className="text-xs text-white">ML 服务</span>
          </div>
          <span className={cn('rounded px-1.5 py-0.5 text-[10px] font-medium border', statusColor(pipelineHealth))}>
            {isHealthy ? '在线' : '离线'}
          </span>
        </div>

        {latestModel && (
          <div className="flex items-center justify-between rounded-md border border-[#1c1c1c] bg-[#0a0a0a] px-2.5 py-1.5">
            <div className="flex items-center gap-2 min-w-0">
              <Activity className="h-3.5 w-3.5 text-[#888888]" />
              <span className="truncate text-xs text-white">{latestModel.model_id}</span>
            </div>
            <span className="shrink-0 text-[10px] text-[#8a8a8a]">
              {latestModel.feature_count} 特征
            </span>
          </div>
        )}

        {modelCount === 0 && (
          <div className="py-4 text-center text-[11px] text-[#8a8a8a]">
            暂无训练好的模型
          </div>
        )}
      </div>
    </SectionCard>
  )
}

/* ── Setup Guide Card ── */
function SetupGuideCard({ onDismiss }: { onDismiss: () => void }) {
  const navigate = useNavigate()
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
          <p className="mt-1 text-sm text-[#999999]">
            完成 {completed}/{steps.length} 步即可开始量化交易。选择策略类型、配置参数、一键启动实盘。
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { navigate('/indicator-community') }}
            className="flex items-center gap-1.5 rounded-lg bg-[#1c1c1c] px-4 py-2 text-sm text-white transition-colors hover:bg-[#262626]"
          >
            <LayoutGrid className="inline h-4 w-4" />
            策略市场
          </button>
          <button
            onClick={() => { navigate('/strategy') }}
            className="flex items-center gap-1.5 rounded-lg bg-white px-4 py-2 text-sm font-medium text-[#0a0a0a] transition-opacity hover:opacity-90"
          >
            <Plus className="inline h-4 w-4" />
            创建策略
          </button>
          <button
            onClick={onDismiss}
            aria-label="关闭提示"
            className="rounded-lg p-2 text-[#999999] transition-colors hover:bg-[#1c1c1c] hover:text-white"
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
                    : 'bg-[#1c1c1c] text-[#8a8a8a]'
                )}
              >
                {step.done ? <ArrowUpRight className="h-3 w-3" /> : i + 1}
              </div>
              <span
                className={cn(
                  'text-xs',
                  step.done ? 'text-[#8a8a8a] line-through' : 'text-[#aaaaaa]'
                )}
              >
                {step.label}
              </span>
              {!step.done && (
                <button
                  onClick={() => {
                    if (step.action === '去设置') navigate('/settings')
                    if (step.action === '创建') navigate('/strategy')
                    if (step.action === '启动') navigate('/bots')
                  }}
                  className="rounded bg-white px-2 py-0.5 text-[10px] font-medium text-[#0a0a0a] transition-opacity hover:opacity-90"
                >
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
            aria-label="上个月"
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
            aria-label="下个月"
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
          <div key={w} className="py-1 text-center text-[10px] font-medium text-[#8a8a8a]">
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
                  !style && 'text-[#8a8a8a]',
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
  const chartRef = useRef<ECharts | null>(null)

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
          formatter: (params: Array<{ value: [number, number] }>) => {
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
  const chartRef = useRef<ECharts | null>(null)

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
          formatter: (params: Array<{ value: [number, number] }>) => {
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
              color: (p: { value: [number, number] }) => (p.value[1] >= 0 ? '#03A66D' : '#ef4444'),
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
function StrategyRow({ s }: { s: StrategyItem }) {
  const pnl = s.total_pnl || 0
  const isRunning = s.status === 'running'
  // CRA 参数展示
  const craParams = (s.trading_config || {}) as Record<string, unknown>
  const direction = s.trade_direction === 'long' ? '多' : s.trade_direction === 'short' ? '空' : '双向'
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
        <div className="text-[10px] text-[#8a8a8a]">
          {s.coin} · {s.leverage}x · {s.strategy_type}
          {!!craParams.trade_direction && (
            <span className={cn('ml-1.5 px-1 rounded text-[9px]',
              craParams.trade_direction === 'long' ? 'bg-quant-green/10 text-quant-green' :
              craParams.trade_direction === 'short' ? 'bg-quant-red/10 text-quant-red' :
              'bg-quant-gold/10 text-quant-gold'
            )}>{direction}</span>
          )}
          {tpMethod && <span className="ml-1 text-[#8a8a8a]">· {tpMethod}</span>}
        </div>
        {stratDesc && (
          <div className="text-[9px] text-[#8a8a8a] mt-0.5">{stratDesc} · 补差{String(craParams.add_position_spread || '-')}% · 止盈{String(craParams.take_profit_ratio || '-')}%</div>
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
              : 'border border-[#1c1c1c] bg-[#111111] text-[#8a8a8a]'
          )}
        >
          {isRunning ? '运行中' : s.status}
        </span>
        {!!craParams.open_indicator && (
          <div className="text-[9px] text-[#8a8a8a] mt-0.5">
            {craParams.open_indicator === 'macd_golden' ? 'MACD金叉' :
             craParams.open_indicator === 'macd_death' ? 'MACD死叉' :
             craParams.open_indicator === 'ema' ? 'EMA拐点' : '市价'}
          </div>
        )}
      </div>
    </div>
  )
}

/* ── Main Dashboard ── */
export function Dashboard() {
  const [showGuide, setShowGuide] = useState(() => {
    return localStorage.getItem('xt-dashboard-guide-dismissed') !== '1'
  })

  const navigate = useNavigate()

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

  const { data: strategies, isLoading: stratLoading } = useQuery<StrategyItem[]>({
    queryKey: ['strategies'],
    queryFn: () => strategyApi.list(),
    refetchInterval: 30000,
  })

  const { data: rankingData, isLoading: rankingLoading } = useQuery<StrategyRanking[] | { ranking: StrategyRanking[] }>({
    queryKey: ['ranking'],
    queryFn: () => strategyApi.ranking(),
  })
  const ranking = Array.isArray(rankingData) ? rankingData : rankingData?.ranking || []

  const { data: protectionStatus, isLoading: protectionLoading } = useQuery<ProtectionStatus>({
    queryKey: ['protection-status'],
    queryFn: async () => {
      const res = await protectionApi.status()
      return res as ProtectionStatus
    },
    refetchInterval: 10000,
  })

  const { data: mlHealth, isLoading: mlHealthLoading } = useQuery<{ status: string }>({
    queryKey: ['ml-health'],
    queryFn: async () => {
      try {
        return await mlApi.health()
      } catch {
        return { status: 'unhealthy' }
      }
    },
    refetchInterval: 30000,
  })

  const { data: mlModelsData, isLoading: mlModelsLoading } = useQuery<ModelInfo[]>({
    queryKey: ['ml-models'],
    queryFn: async () => {
      const res = await mlApi.list()
      return res || []
    },
    refetchInterval: 30000,
  })

  const totalEquity = dash?.total_equity ?? portfolio?.total_equity ?? 0
  const totalPnl = dash?.total_pnl ?? portfolio?.total_pnl ?? 0

  // Real metrics from backend (no hardcoded defaults)
  const winRate = dash?.win_rate ?? null
  const profitFactor = dash?.profit_factor ?? null
  const maxDrawdown = dash?.max_drawdown ?? null
  const totalTrades = dash?.total_trades ?? 0

  const runningStrats = useMemo(() => strategies?.filter((s) => s.status === 'running') || [], [strategies])
  const isLoading = dashLoading || portfolioLoading

  // Convert calendar (daily PnL map) into chart series format
  const dailyPnL = useMemo(() => {
    if (!dash?.calendar) return undefined
    return Object.entries(dash.calendar)
      .map(([date, value]) => ({
        time: new Date(date + 'T00:00:00').getTime() / 1000,
        value: Number(value) || 0,
      }))
      .sort((a, b) => a.time - b.time)
  }, [dash?.calendar])

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
                value={winRate !== null ? `${winRate.toFixed(1)}%` : '--'}
                subLabel="近30天"
                trend="up"
                ringProgress={winRate ?? undefined}
              />
              <KPICard
                icon={<BarChart3 className="h-4 w-4 text-[#888888]" />}
                label="盈亏比"
                value={profitFactor !== null ? profitFactor.toFixed(2) : '--'}
                subLabel="Profit Factor"
                trend="neutral"
              />
              <KPICard
                icon={<TrendingDown className="h-4 w-4 text-red-400" />}
                label="最大回撤"
                value={maxDrawdown !== null ? `${maxDrawdown.toFixed(2)}%` : '--'}
                subLabel="历史最大"
                trend="down"
              />
              <KPICard
                icon={<Activity className="h-4 w-4 text-[#888888]" />}
                label="总交易数"
                value={totalTrades > 0 ? totalTrades.toLocaleString() : '--'}
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
                  navigate('/bots')
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
              <div className="flex items-center gap-1 text-[10px] text-[#8a8a8a]">
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
              <div className="flex items-center gap-2 text-[10px] text-[#8a8a8a]">
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
              <PnLBarChart data={dailyPnL} isLoading={dashLoading} />
            </div>
          </SectionCard>
        </div>

        {/* ── Bottom Row: Calendar | AI Agents | Strategies + Ranking | Risk + ML ── */}
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-4">
          {/* Left: Exchange + Calendar */}
          <div className="space-y-4">
            <SectionCard title="资产分布">
              {isLoading ? (
                <div className="space-y-3">
                  <Skeleton variant="text" lines={4} />
                </div>
              ) : (portfolio?.exchanges?.filter((ex) => ex.connected || ex.balance > 0 || ex.configured) || []).length > 0 ? (
                <div className="space-y-3">
                  {(portfolio?.exchanges?.filter((ex) => ex.connected || ex.balance > 0 || ex.configured) || []).map((ex) => {
                    const isBinance = ex.name === 'binance'
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
                              <span className="text-[#8a8a8a] ml-1">
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
                              <span className="text-[#8a8a8a] ml-1">≈ {formatConverted(portfolio?.futures_balance || 0)}</span>
                            </span>
                          </div>
                          {hasFunding && (
                            <div className="flex items-center justify-between text-xs">
                              <span className="text-[#666666]">{lastVisible === 'funding' ? '└' : '├'} 资金</span>
                              <span className="font-mono text-[#aaaaaa]">
                                ${(portfolio?.funding_balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}
                                <span className="text-[#8a8a8a] ml-1">≈ {formatConverted(portfolio?.funding_balance || 0)}</span>
                              </span>
                            </div>
                          )}
                          {hasEarn && (
                            <div className="flex items-center justify-between text-xs">
                              <span className="text-[#666666]">└ 理财</span>
                              <span className="font-mono text-[#aaaaaa]">
                                ${(portfolio?.earn_balance || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}
                                <span className="text-[#8a8a8a] ml-1">≈ {formatConverted(portfolio?.earn_balance || 0)}</span>
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
                              <span className="text-[#8a8a8a] ml-1">
                                ≈ {formatConverted(ex.balance || 0)}
                              </span>
                            </span>
                          </div>
                        </div>
                      )}
                    </div>
                    )})}
                  <div className="mt-2 flex items-center justify-between border-t border-[#1c1c1c] pt-3 text-sm">
                    <span className="text-[#8a8a8a]">合计</span>
                    <span className="font-mono font-semibold text-white">
                      ${totalEquity.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                      <span className="text-[#8a8a8a] ml-1 text-xs">≈ {formatConverted(totalEquity)}</span>
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
                <span className="text-[10px] text-[#8a8a8a]">XiaoTianQuant v3.0</span>
              }
            >
              <div className="mb-3 grid grid-cols-3 gap-3">
                {(dash?.ai_agents || [
                  { name: '市场情报', status: 'running', detail: '-- 条新信号' },
                  { name: '策略生成', status: 'running', detail: '-- 个策略待审' },
                  { name: '风控AI', status: 'normal', detail: '所有指标安全' },
                ]).map((agent: { name: string; status: string; detail: string }) => (
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
                    <div className="mt-1 flex items-center justify-center gap-1 text-[10px] text-[#8a8a8a]">
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
                    <div className="mt-0.5 truncate text-[10px] text-[#8a8a8a]">
                      {agent.detail}
                    </div>
                  </div>
                ))}
              </div>
              <div className="max-h-24 space-y-1 overflow-y-auto rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-2">
                {(dash?.ai_logs || []).slice(0, 10).map((log: { time: string; message: string }, i: number) => (
                  <div key={i} className="font-mono text-[11px]">
                    <span className="text-[#8a8a8a]">[{log.time}]</span>{' '}
                    <span className="text-[#aaaaaa]">{log.message}</span>
                  </div>
                ))}
                {!(dash?.ai_logs?.length) && (
                  <div className="py-6 text-center text-[11px] text-[#8a8a8a]">
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
                    navigate('/strategy')
                  }}
                />
              )}
            </SectionCard>

            <SectionCard
              title="策略排行榜"
              headerAction={
                <button
                  onClick={() => {
                    navigate('/strategy')
                  }}
                  className="flex items-center gap-0.5 text-[10px] text-[#8a8a8a] transition-colors hover:text-white"
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
                <DataTable<StrategyRanking>
                  data={ranking.slice(0, 10)}
                  keyExtractor={(item, i) => item.strategy_id || String(i)}
                  columns={[
                    {
                      key: 'rank',
                      title: '#',
                      width: '40px',
                      render: (_, i) => {
                        const badge =
                          i === 0 ? 'bg-yellow-500/15 text-yellow-400'
                            : i === 1 ? 'bg-slate-300/15 text-slate-300'
                            : i === 2 ? 'bg-amber-600/15 text-amber-500'
                            : 'bg-transparent text-[#8a8a8a]'
                        return (
                          <div className={cn('flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-bold', badge)}>
                            {i + 1}
                          </div>
                        )
                      },
                    },
                    {
                      key: 'name',
                      title: '策略',
                      render: (item) => (
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium text-white">{item.name}</div>
                          <div className="flex gap-2 text-[10px] text-[#8a8a8a]">
                            <span>胜率 {(item.win_rate || 0).toFixed(1)}%</span>
                            <span>夏普 {(item.sharpe || 0).toFixed(2)}</span>
                          </div>
                        </div>
                      ),
                    },
                    {
                      key: 'return',
                      title: '收益',
                      width: '80px',
                      render: (item) => (
                        <div className={cn('shrink-0 font-mono text-sm font-semibold', (item.total_return || 0) >= 0 ? 'text-emerald-400' : 'text-red-400')}>
                          {formatPercent(item.total_return || 0)}
                        </div>
                      ),
                    },
                  ]}
                />
              ) : (
                <EmptyState
                  title="暂无排行数据"
                  description="运行策略后将显示排行榜"
                />
              )}
            </SectionCard>
          </div>

          {/* Far Right: Risk Control + ML Status */}
          <div className="space-y-4">
            <RiskControlCard status={protectionStatus} isLoading={protectionLoading} />
            <MLStatusCard health={mlHealth} models={mlModelsData} isLoading={mlHealthLoading || mlModelsLoading} />
          </div>
        </div>
      </div>
    </div>
  )
}
