import { useState, useEffect, useRef, useMemo } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import type { EChartsType } from 'echarts'
import { DataTable } from '@/components/DataTable'
import { BacktestResult, BacktestTrade } from '@/types'
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
  SlidersHorizontal,
  GitBranch,
  Beaker,
  CheckSquare,
  Square as SquareIcon,
  BarChart4,
  ArrowUp,
  ArrowDown,
  Maximize2,
  Minimize2,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { backtestApi, indicatorApi } from '@/lib/api'
import { getEcharts } from '@/lib/echarts'
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
  // ── CRA 参数 ──
  order_count?: number
  first_order_amount?: number
  add_position_spread?: number
  add_position_callback?: number
  take_profit_ratio?: number
  profit_callback?: number
  trade_count_mode?: 'single' | 'cycle'
  open_indicator?: string
  add_position_indicator?: string
  waterfall_protection?: number
  open_double?: boolean
  trend_indicator?: boolean
  trend_timeframe?: string
  take_profit_method?: string
  moving_take_profit?: { enabled: boolean; tier1_ratio: number; tier1_drawback: number; tier2_drawback: number }
  reverse_take_profit?: boolean
  reverse_stop_loss?: boolean
  follow_trend?: boolean
  follow_trend_max?: number
  burn_cut?: { enabled: boolean; dual_burn_start: number; global_burn_start: number }
  custom_reduce?: boolean
  online_order_limit?: number
  profit_protection?: boolean
  close_add_position?: boolean
  leverage?: number
  direction?: 'long' | 'short' | 'dual'
  first_order_price?: number
  amplitude?: { '5m': number; '15m': number; '30m': number; '1h': number }
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
  // ── CRA 策略 ──
  { value: 'martin_trend', label: '马丁趋势策略', desc: '倍投补仓(2,4,8,16,32,64) + 趋势指标，浮亏减半' },
  { value: 'wallstreet', label: '华尔街策略', desc: '等比补仓(1,2,3,5,8,13,21,34,55) + 趋势指标' },
  { value: 'macd_golden_long', label: 'MACD金叉开多', desc: 'MACD金叉开多/补多，死叉反向清仓' },
  { value: 'macd_death_short', label: 'MACD死叉开空', desc: 'MACD死叉开空/补空，金叉反向清仓' },
  { value: 'ema_follow_trend', label: 'EMA顺势策略', desc: 'EMA60均线以上做多，EMA10拐点开仓' },
  { value: 'ema_counter_trend', label: 'EMA逆势策略', desc: 'EMA60标准线以上做空，振幅决定开仓' },
  { value: 'dual_burn', label: '双向燃烧斩仓', desc: '逆势单第3仓起开启顺势单，盈利消耗浮亏' },
  { value: 'global_burn', label: '超级全局燃烧', desc: '逆势单第5仓起跨币种燃烧，所有盈利消耗浮亏' },
  { value: 'trend_long', label: '顺势做多', desc: 'EMA金叉做多，适合上涨行情' },
  { value: 'trend_short', label: '顺势做空', desc: 'EMA死叉做空，适合下跌行情' },
  { value: 'counter_stable', label: '逆势稳健', desc: 'EMA60振幅稳健开仓，低风险' },
  { value: 'head_tail_arb', label: '首尾套利', desc: '首单尾单组合套利，降低持仓风险' },
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

function parseTrades(raw: Record<string, unknown>[]): TradeRecord[] {
  return (raw || []).map((t) => ({
    side: (t.side as 'buy' | 'sell') || 'buy',
    entry_price: t.entry_price as number | undefined,
    exit_price: t.exit_price as number | undefined,
    qty: (t.qty as number) || 0,
    pnl: t.pnl as number | undefined,
    time: (t.time as number) || Date.now(),
    bar: (t.bar as number) || 0,
  }))
}

/* ── Equity Chart Component ──────────────────────────────────────── */

function EquityChart({ data, trades, isLoading }: { data?: EquityPoint[]; trades?: TradeRecord[]; isLoading?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)

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
          formatter: (params: Array<{ value: [number, number] }>) => {
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
            name: '权益',
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
          // Buy markers
          {
            name: '买入',
            type: 'scatter',
            data: [],
            symbol: 'pin',
            symbolSize: 24,
            itemStyle: { color: '#03A66D' },
            label: { show: true, formatter: 'B', color: '#fff', fontSize: 10, fontWeight: 'bold' },
            z: 10,
          },
          // Sell markers
          {
            name: '卖出',
            type: 'scatter',
            data: [],
            symbol: 'pin',
            symbolSize: 24,
            itemStyle: { color: '#CF304A' },
            label: { show: true, formatter: 'S', color: '#fff', fontSize: 10, fontWeight: 'bold' },
            z: 10,
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
      const buyData = trades?.filter(t => t.side === 'buy').map(t => [t.time, t.entry_price ?? 0]) ?? []
      const sellData = trades?.filter(t => t.side === 'sell').map(t => [t.time, t.exit_price ?? 0]) ?? []
      chartRef.current.setOption({
        series: [
          { data: data.map((d) => [d.time, d.equity]) },
          { data: buyData },
          { data: sellData },
        ],
      })
    }
  }, [data, trades])

  if (isLoading) return <div className="h-64 animate-pulse rounded-lg bg-quant-bg-secondary" />
  return <div ref={ref} className="h-64 w-full" />
}

/* ── Monthly Return Chart Component ──────────────────────────────── */

function MonthlyReturnChart({ data, isLoading }: { data?: { month: string; returnPct: number }[]; isLoading?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<EChartsType | null>(null)

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
          formatter: (params: Array<{ value: [string, number] }>) => {
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
              color: (p: { value: [string, number] }) => (p.value[1] >= 0 ? '#03A66D' : '#ef4444'),
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

function isBacktestReport(value: unknown): value is BacktestReport {
  const r = value as Record<string, unknown>
  return typeof r?.initial_balance === 'number' && typeof r?.final_equity === 'number'
}

function isBacktestParams(value: unknown): value is BacktestParams {
  const p = value as Record<string, unknown>
  return typeof p?.symbol === 'string' && typeof p?.interval === 'string'
}

export function Backtest() {
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [interval, setIntervalVal] = useState('1h')
  const [strategyType, setStrategyType] = useState('sma_cross')
  const CRA_STRATEGIES = ['martin_trend', 'wallstreet', 'macd_golden_long', 'macd_death_short',
    'ema_follow_trend', 'ema_counter_trend', 'dual_burn', 'global_burn',
    'trend_long', 'trend_short', 'counter_stable', 'head_tail_arb']
  const isCraStrategy = CRA_STRATEGIES.includes(strategyType)
  const [initialBalance, setInitialBalance] = useState(10000)
  const [fromDate, setFromDate] = useState(daysAgo(180))
  const [toDate, setToDate] = useState(dateToISO(new Date()))
  const [activePreset, setActivePreset] = useState<string | null>('最近6个月')
  // ── CRA 回测参数 ──
  const [craOrderCount, setCraOrderCount] = useState(7)
  const [craFirstAmount, setCraFirstAmount] = useState(100)
  const [craAddSpread, setCraAddSpread] = useState(3)
  const [craAddCallback, setCraAddCallback] = useState(0.1)
  const [craTpRatio, setCraTpRatio] = useState(1.3)
  const [craProfitCallback, setCraProfitCallback] = useState(0.1)
  const [craTpMethod, setCraTpMethod] = useState<'full' | 'tail' | 'head_tail' | 'moving'>('full')
  const [craOpenInd, setCraOpenInd] = useState('macd_golden')
  const [craAddInd, setCraAddInd] = useState('macd')
  const [craWaterfall, setCraWaterfall] = useState(2)
  const [craOpenDouble, setCraOpenDouble] = useState(false)
  const [craTrendInd, setCraTrendInd] = useState(false)
  const [craTrendTf, setCraTrendTf] = useState('15m')
  const [craFollowTrend, setCraFollowTrend] = useState(false)
  const [craBurnCut, setCraBurnCut] = useState(false)
  const [craCloseAdd, setCraCloseAdd] = useState(false)
  const [craLeverage, setCraLeverage] = useState(5)
  const [craDirection, setCraDirection] = useState<'long' | 'short' | 'dual'>('long')

  // Multi-strategy comparison
  const [compareStrategies, setCompareStrategies] = useState<string[]>([])
  const [compareResults, setCompareResults] = useState<Record<string, { report: BacktestReport; params: BacktestParams; trades: TradeRecord[] }>>({})
  const [showCompare, setShowCompare] = useState(false)
  // Optimizer
  const [showOptimizer, setShowOptimizer] = useState(false)
  const [optimizerConfig, setOptimizerConfig] = useState({ method: 'de' as 'de' | 'tpe', generations: 10, population: 20 })
  const [optimizerResult, setOptimizerResult] = useState<any>(null)

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
        // ── CRA 参数 ──
        order_count: craOrderCount,
        first_order_amount: craFirstAmount,
        add_position_spread: craAddSpread,
        add_position_callback: craAddCallback,
        take_profit_ratio: craTpRatio,
        profit_callback: craProfitCallback,
        take_profit_method: craTpMethod,
        open_indicator: craOpenInd,
        add_position_indicator: craAddInd,
        waterfall_protection: craWaterfall,
        open_double: craOpenDouble,
        trend_indicator: craTrendInd,
        trend_timeframe: craTrendTf,
        follow_trend: craFollowTrend,
        burn_cut: craBurnCut ? { enabled: true, dual_burn_start: 3, global_burn_start: 5 } : undefined,
        close_add_position: craCloseAdd,
        leverage: craLeverage,
        direction: craDirection,
      }),
    onSuccess: (data: BacktestResult) => {
      setReport(isBacktestReport(data?.report) ? data.report : null)
      setParams(isBacktestParams(data?.params) ? data.params : null)
      const curve = (data?.equity_curve || []).map((p) => ({ time: p.time, equity: p.equity }))
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

  const handleRunCompare = async () => {
    if (compareStrategies.length < 2) return
    setCompareResults({})
    const results: Record<string, any> = {}
    for (const st of compareStrategies) {
      try {
        const data = await backtestApi.run({
          symbol, interval, strategy_type: st,
          initial_balance: { USDT: initialBalance },
          from: fromDate, to: toDate,
          order_count: craOrderCount, first_order_amount: craFirstAmount,
          add_position_spread: craAddSpread, add_position_callback: craAddCallback,
          take_profit_ratio: craTpRatio, profit_callback: craProfitCallback,
          take_profit_method: craTpMethod, open_indicator: craOpenInd,
          add_position_indicator: craAddInd, waterfall_protection: craWaterfall,
          open_double: craOpenDouble, trend_indicator: craTrendInd,
          trend_timeframe: craTrendTf, follow_trend: craFollowTrend,
          burn_cut: craBurnCut ? { enabled: true, dual_burn_start: 3, global_burn_start: 5 } : undefined,
          close_add_position: craCloseAdd, leverage: craLeverage, direction: craDirection,
        })
        results[st] = {
          report: data?.report || null,
          params: data?.params || null,
          trades: parseTrades(data?.trades || []),
        }
      } catch { /* skip failed */ }
    }
    setCompareResults(results)
  }

  const handleRunOptimizer = async () => {
    setOptimizerResult(null)
    try {
      const data = await indicatorApi.experiment.run({
        symbol, interval,
        strategy_type: strategyType,
        optimizer: optimizerConfig.method,
        param_space: {
          order_count: { min: 3, max: 15, step: 1 },
          first_order_amount: { min: 50, max: 500, step: 10 },
          add_position_spread: { min: 1, max: 10, step: 0.5 },
          take_profit_ratio: { min: 0.5, max: 5, step: 0.1 },
          waterfall_protection: { min: 1, max: 10, step: 0.5 },
        },
        generations: optimizerConfig.generations,
        population_size: optimizerConfig.population,
        backtest_config: {
          initial_balance: initialBalance,
          from: fromDate, to: toDate,
        },
      })
      setOptimizerResult(data?.data || data)
    } catch (e) {
      // 优化器错误已在 UI 中反馈
    }
  }

  const applyOptimizerResult = (result: any) => {
    if (!result?.best_params) return
    const p = result.best_params
    if (p.order_count !== undefined) setCraOrderCount(p.order_count)
    if (p.first_order_amount !== undefined) setCraFirstAmount(p.first_order_amount)
    if (p.add_position_spread !== undefined) setCraAddSpread(p.add_position_spread)
    if (p.take_profit_ratio !== undefined) setCraTpRatio(p.take_profit_ratio)
    if (p.waterfall_protection !== undefined) setCraWaterfall(p.waterfall_protection)
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
                  isRunning ? 'cursor-not-allowed bg-quant-gold/50' : 'bg-quant-gold text-[#0a0a0a] hover:opacity-90'
                )}
              >
                {isRunning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                {isRunning ? '回测中...' : '开始回测'}
              </button>
              {isCraStrategy && (
                <button
                  onClick={() => setShowOptimizer(true)}
                  className="flex items-center gap-1.5 rounded-lg border border-quant-gold/30 bg-quant-gold/10 px-4 py-2 text-sm font-medium text-quant-gold transition-colors hover:bg-quant-gold/20"
                >
                  <Beaker className="h-3.5 w-3.5" />
                  自动调参
                </button>
              )}
              <button
                onClick={() => setShowCompare(!showCompare)}
                className={cn(
                  'flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors',
                  showCompare
                    ? 'border-quant-gold/30 bg-quant-gold/10 text-quant-gold'
                    : 'border-quant-border text-muted-foreground hover:text-foreground'
                )}
              >
                <GitBranch className="h-3.5 w-3.5" />
                策略对比
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
                aria-label="交易对"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">K线周期</label>
              <select
                value={interval}
                onChange={(e) => setIntervalVal(e.target.value)}
                aria-label="K线周期"
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
                aria-label="策略类型"
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

          {/* ── CRA 回测参数面板 ── */}
          <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-4">
            <div className="flex items-center gap-2 text-xs font-semibold text-quant-gold">
              <SlidersHorizontal className="w-3.5 h-3.5" />
              CRA 量化回测参数
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">做单数量</label>
                <input type="number" min={1} max={20} value={craOrderCount} onChange={(e) => setCraOrderCount(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="做单数量" />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">首单仓位 (USDT)</label>
                <input type="number" min={10} max={10000} step={10} value={craFirstAmount} onChange={(e) => setCraFirstAmount(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="首单仓位 USDT" />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">杠杆倍数</label>
                <input type="number" min={1} max={125} value={craLeverage} onChange={(e) => setCraLeverage(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="杠杆倍数" />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">补仓价差 (%)</label>
                <input type="number" min={0.5} max={50} step={0.5} value={craAddSpread} onChange={(e) => setCraAddSpread(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="补仓价差百分比" />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">补仓回调 (%)</label>
                <input type="number" min={0.01} max={0.5} step={0.01} value={craAddCallback} onChange={(e) => setCraAddCallback(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="补仓回调百分比" />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">防瀑布 (%)</label>
                <input type="number" min={0.5} max={20} step={0.5} value={craWaterfall} onChange={(e) => setCraWaterfall(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="防瀑布百分比" />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">止盈比例 (%)</label>
                <input type="number" min={0.1} max={50} step={0.1} value={craTpRatio} onChange={(e) => setCraTpRatio(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="止盈比例百分比" />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">盈利回调 (%)</label>
                <input type="number" min={0.01} max={0.5} step={0.01} value={craProfitCallback} onChange={(e) => setCraProfitCallback(Number(e.target.value))}
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold" aria-label="盈利回调百分比" />
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">止盈方式</label>
                <select value={craTpMethod} onChange={(e) => setCraTpMethod(e.target.value as 'full' | 'tail' | 'head_tail' | 'moving')} aria-label="止盈方式"
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                  <option value="full">全仓止盈</option>
                  <option value="tail">尾单止盈</option>
                  <option value="head_tail">首尾止盈</option>
                  <option value="moving">移动止盈</option>
                </select>
              </div>
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">开仓指标</label>
                <select value={craOpenInd} onChange={(e) => setCraOpenInd(e.target.value)} aria-label="开仓指标"
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                  <option value="macd_golden">MACD金叉开多</option>
                  <option value="macd_death">MACD死叉开空</option>
                  <option value="ema">EMA拐点开仓</option>
                  <option value="close">关闭（无脑买入）</option>
                </select>
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">补仓指标</label>
                <select value={craAddInd} onChange={(e) => setCraAddInd(e.target.value)} aria-label="补仓指标"
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                  <option value="macd">MACD补仓</option>
                  <option value="ema">EMA4补仓</option>
                  <option value="close">仅按跌幅</option>
                </select>
              </div>
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">交易方向</label>
                <select value={craDirection} onChange={(e) => setCraDirection(e.target.value as 'long' | 'short' | 'dual')} aria-label="交易方向"
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold">
                  <option value="long">做多</option>
                  <option value="short">做空</option>
                  <option value="dual">双向</option>
                </select>
              </div>
            </div>
            <div className="flex flex-wrap gap-4">
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={craOpenDouble} onChange={(e) => setCraOpenDouble(e.target.checked)} className="rounded border-quant-border" aria-label="开仓加倍" />
                开仓加倍
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={craTrendInd} onChange={(e) => setCraTrendInd(e.target.checked)} className="rounded border-quant-border" aria-label="趋势指标EMA4" />
                趋势指标(EMA4)
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={craFollowTrend} onChange={(e) => setCraFollowTrend(e.target.checked)} className="rounded border-quant-border" aria-label="顺势而为" />
                顺势而为
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={craBurnCut} onChange={(e) => setCraBurnCut(e.target.checked)} className="rounded border-quant-border" aria-label="斩仓燃烧" />
                斩仓燃烧
              </label>
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={craCloseAdd} onChange={(e) => setCraCloseAdd(e.target.checked)} className="rounded border-quant-border" aria-label="关闭补仓" />
                关闭补仓
              </label>
            </div>
            {craTrendInd && (
              <div>
                <label className="mb-1.5 block text-xs text-muted-foreground">EMA4 时间周期</label>
                <div className="flex gap-2">
                  {(['5m', '15m', '30m', '60m'] as const).map((tf) => (
                    <button key={tf} onClick={() => setCraTrendTf(tf)}
                      className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', craTrendTf === tf ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                      {tf}
                    </button>
                  ))}
                </div>
              </div>
            )}
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
                aria-label="初始资金 USDT"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">开始日期</label>
              <input
                type="date"
                value={fromDate}
                onChange={(e) => { setFromDate(e.target.value); setActivePreset(null) }}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
                aria-label="开始日期"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">结束日期</label>
              <input
                type="date"
                value={toDate}
                onChange={(e) => { setToDate(e.target.value); setActivePreset(null) }}
                className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"
                aria-label="结束日期"
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
          <div role="alert" className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
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
                <EquityChart data={equityCurve} trades={trades} isLoading={isRunning} />
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
                <DataTable<TradeRecord>
                  data={trades}
                  columns={[
                    { key: 'time', title: '时间', render: (t) => <span className="text-muted-foreground">{new Date(t.time).toLocaleString()}</span> },
                    { key: 'side', title: '方向', render: (t) => (
                      <span className={cn('px-1.5 py-0.5 rounded text-[10px]', t.side === 'buy' ? 'bg-quant-green/10 text-quant-green' : 'bg-quant-red/10 text-quant-red')}>
                        {t.side === 'buy' ? '买入' : '卖出'}
                      </span>
                    )},
                    { key: 'price', title: '价格', render: (t) => <span className="font-mono">${formatCurrency(t.exit_price ?? t.entry_price ?? 0)}</span> },
                    { key: 'qty', title: '数量', render: (t) => <span className="font-mono">{t.qty}</span> },
                    { key: 'pnl', title: '盈亏', render: (t) => (
                      <span className={cn('font-mono', (t.pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                        {t.pnl != null ? `${t.pnl >= 0 ? '+' : ''}$${t.pnl.toFixed(2)}` : '-'}
                      </span>
                    )},
                  ]}
                  keyExtractor={(_, i) => String(i)}
                  emptyText="暂无交易记录"
                />
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
