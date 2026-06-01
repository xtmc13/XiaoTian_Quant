import { useState, useEffect, useRef, useMemo } from 'react'
import { useMutation } from '@tanstack/react-query'
import {
  Play,
  Download,
  TrendingUp,
  TrendingDown,
  BarChart3,
  Target,
  Percent,
  DollarSign,
  Activity,
  Clock,
  Loader2,
  AlertCircle,
  CheckCircle2,
  XCircle,
  Zap,
  Calendar,
  Database,
  Info,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { backtestApi } from '@/lib/api'
import { KPICard } from '@/components/ui/KPICard'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { BacktestAssumptions } from '@/components/BacktestAssumptions'

/* ── Types ───────────────────────────────────────────────────────── */

interface TradeRecord {
  side: 'buy' | 'sell'
  entry_price?: number
  exit_price?: number
  qty: number
  pnl?: number
  time: number
  bar: number
}

interface BacktestReport {
  initial_balance: number
  final_equity: number
  total_return_pct: number
  max_drawdown_pct: number
  sharpe_ratio: number
  win_rate_pct: number
  total_trades: number
  profit_factor: number
}

interface EquityPoint {
  time: number
  equity: number
}

interface BacktestParams {
  symbol: string
  interval: string
  strategy_type: string
  initial_balance: number
  bars_used: number
  from: string
  to: string
}

/* ── Constants ───────────────────────────────────────────────────── */

const INTERVALS = [
  { value: '1m', label: '1分钟' },
  { value: '5m', label: '5分钟' },
  { value: '15m', label: '15分钟' },
  { value: '30m', label: '30分钟' },
  { value: '1h', label: '1小时' },
  { value: '4h', label: '4小时' },
  { value: '1d', label: '日线' },
  { value: '1w', label: '周线' },
]

const STRATEGIES = [
  { value: 'sma_cross', label: '均线交叉 (SMA Cross)', desc: '快慢均线金叉做多，死叉平仓' },
  { value: 'breakout', label: '突破策略 (Breakout)', desc: '突破N日最高/最低价入场，止损止盈出场' },
]

const PRESET_DATES = [
  { label: '最近1个月', days: 30 },
  { label: '最近3个月', days: 90 },
  { label: '最近6个月', days: 180 },
  { label: '最近1年', days: 365 },
  { label: '最近2年', days: 730 },
]

function dateToISO(d: Date): string {
  return d.toISOString().slice(0, 10)
}

function daysAgo(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return dateToISO(d)
}

/* ── ECharts lazy load ───────────────────────────────────────────── */

let echartsLib: any = null
async function getEcharts() {
  if (!echartsLib) echartsLib = await import('echarts')
  return echartsLib
}

/* ── Helpers ─────────────────────────────────────────────────────── */

function formatPct(n: number): string {
  return `${n >= 0 ? '+' : ''}${n.toFixed(2)}%`
}

function computeMonthlyReturns(curve: EquityPoint[]): { month: string; returnPct: number }[] {
  if (!curve.length) return []
  const monthly: Record<string, { start: number; end: number }> = {}
  curve.forEach((p) => {
    const d = new Date(p.time)
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
    if (!monthly[key]) monthly[key] = { start: p.equity, end: p.equity }
    monthly[key].end = p.equity
  })
  return Object.entries(monthly).map(([month, { start, end }]) => ({
    month,
    returnPct: start ? ((end - start) / start) * 100 : 0,
  }))
}

function parseTrades(raw: any[]): TradeRecord[] {
  return (raw || []).map((t) => ({
    side: t.side || 'buy',
    entry_price: t.entry_price,
    exit_price: t.exit_price,
    qty: t.qty || 0,
    pnl: t.pnl,
    time: t.time || Date.now(),
    bar: t.bar || 0,
  }))
}

/* ── Equity Chart Component ──────────────────────────────────────── */

function EquityChart({ data, isLoading }: { data?: EquityPoint[]; isLoading?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<any>(null)

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
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
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => { disposed = true; chartRef.current?.dispose() }
  }, [])

  useEffect(() => {
    if (chartRef.current && data) {
      chartRef.current.setOption({
        series: [{ data: data.map((d) => [d.time, d.equity]) }],
      })
    }
  }, [data])

  if (isLoading) return <div className="h-64 animate-pulse rounded-lg bg-quant-bg-secondary" />
  return <div ref={ref} className="h-64 w-full" />
}

/* ── Monthly Return Chart Component ──────────────────────────────── */

function MonthlyReturnChart({ data, isLoading }: { data?: { month: string; returnPct: number }[]; isLoading?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<any>(null)

  useEffect(() => {
    let disposed = false
    getEcharts().then((echarts) => {
      if (disposed || !ref.current) return
      chartRef.current = echarts.init(ref.current, 'dark')
      chartRef.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 48, right: 16, top: 16, bottom: 40 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(17,17,17,0.95)',
          borderColor: '#2a2a2a',
          textStyle: { color: '#cccccc', fontSize: 11 },
          formatter: (params: any[]) => {
            const p = params[0]
            const color = p.value[1] >= 0 ? '#34d399' : '#f87171'
            return `<div style="font-size:10px;color:#888;margin-bottom:4px">${p.value[0]}</div>
                    <div style="font-weight:600;color:${color}">${p.value[1] >= 0 ? '+' : ''}${p.value[1].toFixed(2)}%</div>`
          },
        },
        xAxis: {
          type: 'category',
          axisLabel: { fontSize: 10, color: '#555555', rotate: 45 },
          axisLine: { lineStyle: { color: '#1c1c1c' } },
          splitLine: { show: false },
        },
        yAxis: {
          type: 'value',
          axisLabel: { fontSize: 10, color: '#555555', formatter: '{value}%' },
          splitLine: { lineStyle: { color: '#1c1c1c' } },
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
      const ro = new ResizeObserver(() => chartRef.current?.resize())
      ro.observe(ref.current)
    })
    return () => { disposed = true; chartRef.current?.dispose() }
  }, [])

  useEffect(() => {
    if (chartRef.current && data) {
      chartRef.current.setOption({
        xAxis: { data: data.map((d) => d.month) },
        series: [{ data: data.map((d) => [d.month, d.returnPct]) }],
      })
    }
  }, [data])

  if (isLoading) return <div className="h-64 animate-pulse rounded-lg bg-quant-bg-secondary" />
  return <div ref={ref} className="h-64 w-full" />
}

/* ── Main Page ───────────────────────────────────────────────────── */

export function Backtest() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setIntervalVal] = useState('1h')
  const [strategyType, setStrategyType] = useState('sma_cross')
  const [initialBalance, setInitialBalance] = useState(10000)
  const [fromDate, setFromDate] = useState(daysAgo(180))
  const [toDate, setToDate] = useState(dateToISO(new Date()))
  const [activePreset, setActivePreset] = useState<string | null>('最近6个月')

  const [report, setReport] = useState<BacktestReport | null>(null)
  const [params, setParams] = useState<BacktestParams | null>(null)
  const [equityCurve, setEquityCurve] = useState<EquityPoint[]>([])
  const [trades, setTrades] = useState<TradeRecord[]>([])

  const runMut = useMutation({
    mutationFn: () =>
      backtestApi.run({
        symbol,
        interval,
        strategy_type: strategyType,
        initial_balance: { USDT: initialBalance },
        from: fromDate,
        to: toDate,
      }),
    onSuccess: (data: any) => {
      setReport(data?.report || null)
      setParams(data?.params || null)
      const curve = (data?.equity_curve || []).map((p: any) => ({ time: p.time, equity: p.equity }))
      setEquityCurve(curve)
      setTrades(parseTrades(data?.trades || []))
    },
  })

  const isRunning = runMut.isPending
  const monthlyReturns = useMemo(() => computeMonthlyReturns(equityCurve), [equityCurve])

  const handlePreset = (label: string, days: number) => {
    setActivePreset(label)
    setFromDate(daysAgo(days))
    setToDate(dateToISO(new Date()))
  }

  const handleRun = () => {
    setReport(null)
    setParams(null)
    setEquityCurve([])
    setTrades([])
    runMut.mutate()
  }

  const handleExportCSV = () => {
    if (!trades.length) return
    const headers = ['time', 'side', 'entry_price', 'exit_price', 'qty', 'pnl']
    const rows = trades.map((t) => [
      new Date(t.time).toISOString(),
      t.side,
      t.entry_price ?? '',
      t.exit_price ?? '',
      t.qty,
      t.pnl ?? '',
    ])
    const csv = [headers.join(','), ...rows.map((r) => r.join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `backtest-${symbol}-${interval}-${strategyType}-${Date.now()}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  const metrics = report
    ? [
        { label: '总收益率', value: formatPct(report.total_return_pct), icon: TrendingUp, color: report.total_return_pct >= 0 ? 'green' : 'red' },
        { label: '最终权益', value: `$${formatCurrency(report.final_equity)}`, icon: DollarSign, color: 'gold' },
        { label: '最大回撤', value: `${report.max_drawdown_pct.toFixed(2)}%`, icon: TrendingDown, color: 'red' },
        { label: '夏普比率', value: report.sharpe_ratio.toFixed(2), icon: Target, color: 'gold' },
        { label: '胜率', value: `${report.win_rate_pct.toFixed(1)}%`, icon: Percent, color: 'green' },
        { label: '总交易数', value: String(report.total_trades), icon: BarChart3, color: 'neutral' },
        { label: '盈亏比', value: `${report.profit_factor.toFixed(2)}:1`, icon: Activity, color: 'gold' },
        { label: '初始资金', value: `$${formatCurrency(report.initial_balance)}`, icon: DollarSign, color: 'neutral' },
      ]
    : []

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-5"
      >
        <PageHeader
          subtitle="使用真实历史数据验证策略表现"
          actions={
            <>
              {report && (
                <button
                  onClick={handleExportCSV}
                  className="flex items-center gap-1.5 rounded-lg border border-quant-border bg-quant-card px-3 py-2 text-sm text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground"
                >
                  <Download className="h-3.5 w-3.5" />
                  导出 CSV
                </button>
              )}
              <button
                onClick={handleRun}
                disabled={isRunning}
                className={cn(
                  'flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-opacity',
                  isRunning ? 'cursor-not-allowed bg-quant-gold/50' : 'bg-quant-gold text-white hover:opacity-90'
                )}
              >
                {isRunning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                {isRunning ? '回测中...' : '开始回测'}
              </button>
            </>
          }
        />

        {/* ── Config Form ── */}
        <SectionCard title="回测参数" bodyClassName="space-y-4">
          {/* Row 1: Symbol + Interval + Strategy */}
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">交易对</label>
              <input
                value={symbol}
                onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
                placeholder="例如: BTCUSDT"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">K线周期</label>
              <select
                value={interval}
                onChange={(e) => setIntervalVal(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
              >
                {INTERVALS.map((opt) => (
                  <option key={opt.value} value={opt.value}>{opt.label} ({opt.value})</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">策略</label>
              <select
                value={strategyType}
                onChange={(e) => setStrategyType(e.target.value)}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
              >
                {STRATEGIES.map((s) => (
                  <option key={s.value} value={s.value}>{s.label}</option>
                ))}
              </select>
              <p className="mt-1 text-[10px] text-muted-foreground">
                {STRATEGIES.find((s) => s.value === strategyType)?.desc || ''}
              </p>
            </div>
          </div>

          {/* Row 2: Date range + Initial balance */}
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">初始资金 (USDT)</label>
              <input
                type="number"
                min={100}
                value={initialBalance}
                onChange={(e) => setInitialBalance(Number(e.target.value))}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">开始日期</label>
              <input
                type="date"
                value={fromDate}
                onChange={(e) => { setFromDate(e.target.value); setActivePreset(null) }}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">结束日期</label>
              <input
                type="date"
                value={toDate}
                onChange={(e) => { setToDate(e.target.value); setActivePreset(null) }}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
              />
            </div>
          </div>

          {/* Preset buttons */}
          <div className="flex flex-wrap gap-1.5">
            <span className="text-[10px] text-muted-foreground mr-1 self-center">快捷:</span>
            {PRESET_DATES.map((p) => (
              <button
                key={p.label}
                onClick={() => handlePreset(p.label, p.days)}
                className={cn(
                  'px-2.5 py-1 rounded text-[11px] transition-colors',
                  activePreset === p.label
                    ? 'bg-quant-gold/15 text-quant-gold border border-quant-gold/30'
                    : 'bg-quant-bg-tertiary text-muted-foreground border border-quant-border hover:border-quant-gold/30'
                )}
              >
                {p.label}
              </button>
            ))}
          </div>
        </SectionCard>

        {/* ── Edge Analysis ── */}
        {trades.length >= 3 && (() => {
          const wins = trades.filter(t => (t.pnl || 0) > 0)
          const losses = trades.filter(t => (t.pnl || 0) < 0)
          const wr = trades.length > 0 ? (wins.length / trades.length * 100) : 0
          const avgW = wins.length > 0 ? wins.reduce((s, t) => s + (t.pnl || 0), 0) / wins.length : 0
          const avgL = losses.length > 0 ? Math.abs(losses.reduce((s, t) => s + (t.pnl || 0), 0) / losses.length) : 0
          const pf = (avgL > 0 && avgW > 0) ? (avgW * wins.length) / (avgL * losses.length) : 0
          const expectancy = (wr / 100) * avgW - ((100 - wr) / 100) * avgL
          return (
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-quant-border/50 bg-quant-bg-secondary/50 px-4 py-2 text-[11px] text-muted-foreground">
              <span className="font-medium text-xs text-foreground">Edge 分析</span>
              <span className="text-quant-border">|</span>
              <span>胜率 <b className="text-foreground">{wr.toFixed(1)}%</b></span>
              <span>均盈 <b className="text-quant-green">${avgW.toFixed(0)}</b></span>
              <span>均亏 <b className="text-quant-red">${avgL.toFixed(0)}</b></span>
              {pf > 0 && <span>盈亏比 <b className="text-foreground">{pf.toFixed(2)}</b></span>}
              <span>期望值 <b className={expectancy >= 0 ? 'text-quant-green' : 'text-quant-red'}>${expectancy.toFixed(2)}</b></span>
            </div>
          )
        })()}

        {/* ── Data source info bar ── */}
        <div className="flex items-center gap-2 rounded-lg border border-quant-border bg-quant-bg-secondary px-4 py-2 text-xs text-muted-foreground">
          <Database className="h-3.5 w-3.5 text-quant-gold/70" />
          <span>数据来源: <strong className="text-foreground">Binance 真实历史数据</strong></span>
          <span className="text-quant-border">|</span>
          <span>交易对、周期、日期范围可在上方配置</span>
        </div>

        {/* ── Backtest Assumptions ── */}
        <BacktestAssumptions />

        {/* ── Error state ── */}
        {runMut.isError && (
          <div className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            <AlertCircle className="h-4 w-4 mt-0.5 shrink-0" />
            <div>
              <p className="font-medium">回测执行失败</p>
              <p className="mt-0.5 text-xs text-red-400/70">{(runMut.error as Error)?.message || '未知错误'}</p>
            </div>
          </div>
        )}

        {/* ── Results ── */}
        {report && (
          <>
            {/* Params summary */}
            {params && (
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-quant-border/50 bg-quant-bg-secondary/50 px-4 py-2 text-[11px] text-muted-foreground">
                <Info className="h-3 w-3" />
                <span>{params.symbol} · {params.interval} · {params.strategy_type}</span>
                <span className="text-quant-border">|</span>
                <span>{params.from} → {params.to} ({params.bars_used} 根K线)</span>
                <span className="text-quant-border">|</span>
                <span className="text-quant-gold/80">数据: Binance</span>
              </div>
            )}

            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              {metrics.map((m) => (
                <KPICard
                  key={m.label}
                  icon={<m.icon className="h-4 w-4 text-muted-foreground" />}
                  label={m.label}
                  value={m.value}
                  trend={m.color === 'green' ? 'up' : m.color === 'red' ? 'down' : 'neutral'}
                />
              ))}
            </div>

            <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
              <SectionCard title="权益曲线">
                <EquityChart data={equityCurve} isLoading={isRunning} />
              </SectionCard>
              <SectionCard title="月度收益分布">
                <MonthlyReturnChart data={monthlyReturns} isLoading={isRunning} />
              </SectionCard>
            </div>

            <SectionCard
              title="交易记录"
              headerAction={
                <span className="text-xs text-muted-foreground">共 {trades.length} 笔</span>
              }
            >
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-left text-muted-foreground border-b border-quant-border">
                      <th className="pb-3 font-medium px-2">时间</th>
                      <th className="pb-3 font-medium px-2">方向</th>
                      <th className="pb-3 font-medium px-2">价格</th>
                      <th className="pb-3 font-medium px-2">数量</th>
                      <th className="pb-3 font-medium px-2">盈亏</th>
                    </tr>
                  </thead>
                  <tbody>
                    {trades.map((t, i) => (
                      <tr key={i} className="border-b border-quant-border/50 hover:bg-white/[0.02] transition-colors">
                        <td className="py-3 px-2 text-muted-foreground">
                          {new Date(t.time).toLocaleString()}
                        </td>
                        <td className="py-3 px-2">
                          <span
                            className={cn(
                              'px-1.5 py-0.5 rounded text-[10px]',
                              t.side === 'buy'
                                ? 'bg-quant-green/10 text-quant-green'
                                : 'bg-quant-red/10 text-quant-red'
                            )}
                          >
                            {t.side === 'buy' ? '买入' : '卖出'}
                          </span>
                        </td>
                        <td className="py-3 px-2 font-mono">
                          ${formatCurrency(t.exit_price ?? t.entry_price ?? 0)}
                        </td>
                        <td className="py-3 px-2 font-mono">
                          {t.qty}
                        </td>
                        <td className={cn('py-3 px-2 font-mono', (t.pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                          {t.pnl != null ? `${t.pnl >= 0 ? '+' : ''}$${t.pnl.toFixed(2)}` : '-'}
                        </td>
                      </tr>
                    ))}
                    {trades.length === 0 && (
                      <tr>
                        <td colSpan={5} className="py-8 text-center text-muted-foreground">
                          暂无交易记录
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </SectionCard>
          </>
        )}

        {!report && !isRunning && !runMut.isError && (
          <EmptyState
            icon={<Activity className="h-6 w-6" />}
            title="开始回测"
            description="选择交易对、周期、策略和日期范围，使用 Binance 真实历史数据验证策略表现"
            actionLabel="开始回测"
            onAction={handleRun}
          />
        )}
      </div>
    </div>
  )
}
