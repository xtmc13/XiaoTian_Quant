import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { useMutation } from '@tanstack/react-query'
import type { EChartsType } from 'echarts'
import { DataTable } from '@/components/DataTable'
import type { BacktestResult, BacktestReport } from '@/types'
import {
  Play, Download, TrendingUp, TrendingDown, BarChart3, Target,
  Percent, DollarSign, Activity, Loader2, AlertCircle,
  Database, SlidersHorizontal, GitBranch, Beaker,
  ChevronRight, ChevronDown, RefreshCw, Shield, Gauge,
  Clock, Zap, ArrowUpDown, Search, Filter, X,
  LayoutDashboard, LineChart, ListFilter, AlertTriangle,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { backtestApi, indicatorApi } from '@/lib/api'
import { INTERVAL_OPTIONS } from '@/lib/constants'
import { getEcharts } from '@/lib/echarts'
import { PerformanceChart, type EquityPoint, type TradeRecord } from '@/components/charts/PerformanceChart'
import { CRAParamForm, type CRAParams, DEFAULT_CRA_PARAMS, craParamsToApiPayload } from '@/components/strategy/CRAParamForm'
import { KPICard } from '@/components/ui/KPICard'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { BacktestAssumptions } from '@/components/BacktestAssumptions'

/* ── Types ───────────────────────────────────────────────────────── */
interface BacktestParams {
  symbol: string; interval: string; strategy_type: string; initial_balance?: Record<string, number>;
  from?: string; to?: string; order_count?: number; first_order_amount?: number;
  add_position_spread?: number; add_position_callback?: number; take_profit_ratio?: number;
  profit_callback?: number; take_profit_method?: string; open_indicator?: string;
  add_position_indicator?: string; waterfall_protection?: number; open_double?: boolean;
  trend_indicator?: boolean; trend_timeframe?: string; follow_trend?: boolean;
  burn_cut?: unknown; close_add_position?: boolean; leverage?: number; direction?: string;
  [key: string]: unknown;
}

interface BacktestHistoryItem {
  id: string; symbol: string; interval: string; strategyType: string; date: string
  totalReturn: number; maxDrawdown: number; sharpe: number; winRate: number; trades: number
  params: BacktestParams
}

/* ── Constants ───────────────────────────────────────────────────── */
const STRATEGIES = [
  { value: 'sma_cross', label: '均线交叉', desc: '快慢均线金叉做多死叉平仓', supported: true },
  { value: 'breakout', label: '突破策略', desc: '突破N日最高/最低价入场，止损止盈出场', supported: true },
  { value: 'martin_trend', label: '马丁趋势', desc: '倍投补仓(2,4,8,16,32,64)+趋势指标', supported: true },
  { value: 'wallstreet', label: '华尔街策略', desc: '等比补仓(1,2,3,5,8,13,21,34,55)', supported: true },
  { value: 'macd_golden_long', label: 'MACD金叉开多', desc: 'MACD金叉开多/补多，死叉反向清仓', supported: true },
  { value: 'macd_death_short', label: 'MACD死叉开空', desc: 'MACD死叉开空/补空，金叉反向清仓', supported: true },
  { value: 'ema_follow_trend', label: 'EMA顺势策略', desc: 'EMA60均线以上做多，EMA10拐点开仓', supported: true },
  { value: 'ema_counter_trend', label: 'EMA逆势策略', desc: 'EMA60标准线以上做空', supported: true },
  { value: 'dual_burn', label: '双向燃烧', desc: '逆势单第3仓起开启顺势单', supported: true },
  { value: 'global_burn', label: '超级全局燃烧', desc: '逆势单第5仓起跨币种燃烧', supported: true },
  { value: 'trend_long', label: '顺势做多', desc: 'EMA金叉做多', supported: true },
  { value: 'trend_short', label: '顺势做空', desc: 'EMA死叉做空', supported: true },
  { value: 'counter_stable', label: '逆势稳健', desc: 'EMA60振幅稳健开仓', supported: true },
  { value: 'head_tail_arb', label: '首尾套利', desc: '首单尾单组合套利', supported: true },
]

const PRESET_DATES = [
  { label: '最近1月', days: 30 }, { label: '最近3月', days: 90 },
  { label: '最近6月', days: 180 }, { label: '最近1年', days: 365 }, { label: '最近2年', days: 730 },
]

const RESULT_TABS = [
  { key: 'overview', label: '概览', icon: LayoutDashboard },
  { key: 'charts', label: '图表', icon: LineChart },
  { key: 'trades', label: '交易记录', icon: ListFilter },
  { key: 'risk', label: '风险分析', icon: AlertTriangle },
] as const
type TabKey = typeof RESULT_TABS[number]['key']

function dateToISO(d: Date): string { return d.toISOString().slice(0, 10) }
function daysAgo(n: number): string { const d = new Date(); d.setDate(d.getDate() - n); return dateToISO(d) }
function formatPct(n: number): string { return `${n >= 0 ? '+' : ''}${n.toFixed(2)}%` }
function formatAbsPct(n: number): string { return `${n.toFixed(2)}%` }
function toFixed(n: number | undefined, d = 2): string { return n != null ? n.toFixed(d) : '-' }

/* ── Rating badge ── */
function getRating(report: BacktestReport) {
  const score = (
    (report.total_return_pct > 10 ? 20 : report.total_return_pct > 0 ? 10 : 0) +
    ((report.sortino_ratio ?? report.sharpe_ratio) > 2 ? 25 : (report.sortino_ratio ?? report.sharpe_ratio) > 1 ? 15 : 5) +
    (report.max_drawdown_pct < 10 ? 20 : report.max_drawdown_pct < 20 ? 12 : 4) +
    (report.win_rate_pct > 55 ? 15 : report.win_rate_pct > 40 ? 8 : 3) +
    (report.profit_factor > 1.5 ? 20 : report.profit_factor > 1.1 ? 10 : 5)
  ) as number
  if (score >= 80) return { label: '优秀', color: 'text-quant-green', bg: 'bg-quant-green/10', border: 'border-quant-green/30' }
  if (score >= 60) return { label: '良好', color: 'text-blue-400', bg: 'bg-blue-400/10', border: 'border-blue-400/30' }
  if (score >= 40) return { label: '一般', color: 'text-yellow-400', bg: 'bg-yellow-400/10', border: 'border-yellow-400/30' }
  return { label: '较差', color: 'text-quant-red', bg: 'bg-quant-red/10', border: 'border-quant-red/30' }
}

/* ── Collapsible Section ── */
function CollapsibleSection({ title, count, defaultOpen, children }: { title: string; count?: number; defaultOpen?: boolean; children: React.ReactNode }) {
  const [open, setOpen] = useState(defaultOpen ?? false)
  return (
    <div className="rounded-xl border border-quant-border overflow-hidden">
      <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-4 py-3 bg-quant-bg-tertiary hover:bg-quant-hover transition-colors text-left">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-quant-gold">{title}</span>
          {count != null && <span className="text-[10px] text-muted-foreground">({count}项)</span>}
        </div>
        {open ? <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" /> : <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" />}
      </button>
      {open && <div className="p-4 space-y-4">{children}</div>}
    </div>
  )
}

/* ── Monthly Heatmap ── */
function MonthlyHeatmap({ data, isLoading }: { data?: Record<string, number>; isLoading?: boolean }) {
  if (isLoading) return <div className="h-64 animate-pulse rounded-lg bg-quant-bg-secondary" />

  const entries = data ? Object.entries(data).sort(([a], [b]) => a.localeCompare(b)) : []
  if (!entries.length) return <div className="h-64 flex items-center justify-center text-xs text-muted-foreground">运行回测后显示月度收益分布</div>

  const yearGroups: Record<string, { month: string; label: string; returnPct: number }[]> = {}
  const allVal = Object.values(data!).map(Math.abs)
  const maxAbs = Math.max(...allVal, 0.1)
  entries.forEach(([key, val]) => {
    const [y, m] = key.split('-')
    if (!yearGroups[y]) yearGroups[y] = []
    const labels = ['1月','2月','3月','4月','5月','6月','7月','8月','9月','10月','11月','12月']
    yearGroups[y].push({ month: key, label: labels[parseInt(m)-1] || key, returnPct: val })
  })

  return (
    <div className="space-y-4">
      {Object.entries(yearGroups).sort(([a],[b]) => a.localeCompare(b)).map(([year, md]) => (
        <div key={year}>
          <div className="text-[10px] text-muted-foreground mb-2 font-medium">{year}年</div>
          <div className="grid grid-cols-12 gap-1.5">
            {[1,2,3,4,5,6,7,8,9,10,11,12].map(m => {
              const d = md.find(x => parseInt(x.month.split('-')[1]) === m)
              const r = d?.returnPct ?? 0
              const has = d !== undefined
              const i = has ? Math.min(Math.abs(r) / maxAbs, 1) : 0
              const bg = !has ? '#0a0a0a' : r >= 0
                ? `rgba(16,185,129,${0.08 + i * 0.55})`
                : `rgba(239,68,68,${0.08 + i * 0.55})`
              const tc = !has ? '#333' : r >= 0 ? '#34d399' : '#f87171'
              return (
                <div key={m} className="aspect-square rounded-md flex flex-col items-center justify-center font-mono" style={{ backgroundColor: bg, color: tc, fontSize: 10 }}
                  title={has ? `${d!.label}: ${formatPct(r)}` : undefined}>
                  <span style={{fontSize:8,opacity:0.7}}>{has ? m + '月' : '-'}</span>
                  {has && <span className="font-bold mt-0.5">{r >= 0 ? '+' : ''}{r.toFixed(1)}%</span>}
                </div>
              )
            })}
          </div>
        </div>
      ))}
      <div className="flex items-center justify-end gap-3 text-[10px] text-muted-foreground">
        <span className="flex items-center gap-1"><span className="inline-block w-3 h-3 rounded-sm" style={{background:'rgba(16,185,129,0.4)'}} />盈利</span>
        <span className="flex items-center gap-1"><span className="inline-block w-3 h-3 rounded-sm" style={{background:'rgba(239,68,68,0.4)'}} />亏损</span>
      </div>
    </div>
  )
}

/* ── Daily Returns Histogram ── */
function DailyReturnsChart({ equityCurve }: { equityCurve: EquityPoint[] }) {
  const chartRef = useRef<HTMLDivElement>(null)
  const instance = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!chartRef.current || equityCurve.length < 2) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      const dailyReturns: number[] = []
      for (let i = 1; i < equityCurve.length; i++) {
        const prev = equityCurve[i - 1].equity
        if (prev > 0) dailyReturns.push(((equityCurve[i].equity - prev) / prev) * 100)
      }
      if (!dailyReturns.length) return

      // Build histogram bins
      const absMax = Math.max(...dailyReturns.map(Math.abs), 0.01)
      const binWidth = absMax / 15
      const bins: { from: number; to: number; count: number }[] = []
      for (let b = -absMax; b < absMax; b += binWidth) {
        bins.push({ from: b, to: b + binWidth, count: 0 })
      }
      dailyReturns.forEach(r => {
        const idx = Math.floor((r + absMax) / binWidth)
        if (idx >= 0 && idx < bins.length) bins[idx].count++
      })

      if (instance.current) instance.current.dispose()
      instance.current = echarts.init(chartRef.current, 'dark')
      instance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 48, right: 16, top: 16, bottom: 28 },
        xAxis: { type: 'category', data: bins.map(b => b.from.toFixed(2)),
          axisLabel: { fontSize: 9, color: '#555', rotate: 45, interval: 4 },
          axisLine: { lineStyle: { color: '#1c1c1c' } } },
        yAxis: { type: 'value', axisLabel: { fontSize: 9, color: '#555' }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
        tooltip: { trigger: 'axis', backgroundColor: 'rgba(17,17,17,0.95)', borderColor: '#2a2a2a', textStyle: { color: '#ccc', fontSize: 10 },
          formatter: (p: { name: string; value: number }[]) => `${p[0].name}%: ${p[0].value} 次` },
        series: [{
          type: 'bar', data: bins.map(b => ({
            value: b.count,
            itemStyle: { color: b.from >= 0 ? '#34d399' : '#f87171', opacity: 0.7 },
          })),
          barWidth: '90%',
        }],
      })
      const ro = new ResizeObserver(() => instance.current?.resize())
      ro.observe(chartRef.current)
      return () => { instance.current?.dispose(); ro.disconnect() }
    })
  }, [equityCurve])

  return <div ref={chartRef} className="w-full" style={{ height: 220 }} />
}

/* ── PnL Distribution ── */
function PnLDistribution({ trades }: { trades: TradeRecord[] }) {
  const chartRef = useRef<HTMLDivElement>(null)
  const instance = useRef<EChartsType | null>(null)

  useEffect(() => {
    if (!chartRef.current || trades.length < 3) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      const pnls = trades.map(t => t.pnl ?? 0).sort((a, b) => a - b)
      const maxAbs = Math.max(...pnls.map(Math.abs), 1)
      const binWidth = (maxAbs * 2) / 20
      const bins: { from: number; to: number; count: number; sumPnl: number }[] = []
      for (let b = -maxAbs; b < maxAbs; b += binWidth) bins.push({ from: b, to: b + binWidth, count: 0, sumPnl: 0 })
      pnls.forEach(p => {
        const idx = Math.floor((p + maxAbs) / binWidth)
        if (idx >= 0 && idx < bins.length) { bins[idx].count++; bins[idx].sumPnl += p }
      })

      if (instance.current) instance.current.dispose()
      instance.current = echarts.init(chartRef.current, 'dark')
      instance.current.setOption({
        backgroundColor: 'transparent',
        grid: { left: 48, right: 16, top: 16, bottom: 28 },
        xAxis: { type: 'category', data: bins.map(b => `$${b.from.toFixed(0)}`),
          axisLabel: { fontSize: 9, color: '#555', rotate: 45, interval: 4 },
          axisLine: { lineStyle: { color: '#1c1c1c' } } },
        yAxis: { type: 'value', axisLabel: { fontSize: 9, color: '#555' }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
        tooltip: { trigger: 'axis', backgroundColor: 'rgba(17,17,17,0.95)', borderColor: '#2a2a2a', textStyle: { color: '#ccc', fontSize: 10 } },
        series: [{
          type: 'bar', data: bins.map(b => ({
            value: b.count,
            itemStyle: { color: b.from >= 0 ? '#34d399' : '#f87171', opacity: 0.6 },
          })),
          barWidth: '90%',
        }],
      })
      const ro = new ResizeObserver(() => instance.current?.resize())
      ro.observe(chartRef.current)
      return () => { instance.current?.dispose(); ro.disconnect() }
    })
  }, [trades])

  return <div ref={chartRef} className="w-full" style={{ height: 200 }} />
}

/* ── Monte Carlo Simulation ── */
function MonteCarloSim({ trades, initialEquity }: { trades: TradeRecord[]; initialEquity: number }) {
  const [simResult, setSimResult] = useState<{ avgFinal: number; best5: number; worst5: number; median: number } | null>(null)
  const [simulating, setSimulating] = useState(false)
  const chartRef = useRef<HTMLDivElement>(null)
  const chartInstance = useRef<EChartsType | null>(null)

  const runSimulation = useCallback(() => {
    if (trades.length < 5) return
    setSimulating(true)
    const pnls = trades.map(t => t.pnl || 0).filter(p => p !== 0)
    const results: number[] = []
    for (let sim = 0; sim < 500; sim++) {
      let equity = initialEquity
      for (let i = 0; i < pnls.length; i++) equity += pnls[Math.floor(Math.random() * pnls.length)]
      results.push(equity)
    }
    results.sort((a, b) => a - b)
    setSimResult({
      avgFinal: results.reduce((s, v) => s + v, 0) / results.length,
      best5: results[Math.floor(results.length * 0.95)],
      worst5: results[Math.floor(results.length * 0.05)],
      median: results[Math.floor(results.length * 0.5)],
    })
    setSimulating(false)
  }, [trades, initialEquity])

  useEffect(() => {
    if (!chartRef.current || !simResult) return
    getEcharts().then((echarts) => {
      if (!chartRef.current) return
      const inst = echarts.init(chartRef.current, 'dark')
      chartInstance.current = inst
      inst.setOption({
        backgroundColor: 'transparent',
        grid: { left: 48, right: 16, top: 16, bottom: 24 },
        xAxis: { type: 'category', data: ['最差5%', '中位数', '平均', '最好5%'], axisLabel: { fontSize: 10, color: '#555' }, axisLine: { lineStyle: { color: '#1c1c1c' } } },
        yAxis: { type: 'value', axisLabel: { fontSize: 10, color: '#555', formatter: (v: number) => `$${formatCurrency(v)}` }, splitLine: { lineStyle: { color: '#1c1c1c' } } },
        tooltip: { trigger: 'axis', backgroundColor: 'rgba(17,17,17,0.95)', borderColor: '#2a2a2a', textStyle: { color: '#ccc', fontSize: 11 } },
        series: [{
          type: 'bar', data: [
            { value: simResult.worst5, itemStyle: { color: '#ef4444' } },
            { value: simResult.median, itemStyle: { color: '#f59e0b' } },
            { value: simResult.avgFinal, itemStyle: { color: '#03A66D' } },
            { value: simResult.best5, itemStyle: { color: '#10b981' } },
          ], barWidth: '40%',
        }],
      })
      return () => inst.dispose()
    })
  }, [simResult])

  if (trades.length < 5) return null

  return (
    <SectionCard title="蒙特卡洛模拟" headerAction={
      <button onClick={runSimulation} disabled={simulating} className="flex items-center gap-1 text-[10px] text-quant-gold hover:underline disabled:opacity-50">
        <RefreshCw className="w-3 h-3" /> {simulating ? '模拟中...' : '运行500次模拟'}
      </button>
    }>
      {simResult ? (
        <div className="space-y-3">
          <div ref={chartRef} className="h-40 w-full" />
          <div className="grid grid-cols-4 gap-2 text-[10px]">
            <div className="text-center p-2 rounded-lg bg-quant-bg-secondary"><div className="text-muted-foreground">最差5%</div><div className="font-mono text-quant-red">${formatCurrency(simResult.worst5)}</div></div>
            <div className="text-center p-2 rounded-lg bg-quant-bg-secondary"><div className="text-muted-foreground">中位数</div><div className="font-mono text-foreground">${formatCurrency(simResult.median)}</div></div>
            <div className="text-center p-2 rounded-lg bg-quant-bg-secondary"><div className="text-muted-foreground">平均终值</div><div className="font-mono text-quant-green">${formatCurrency(simResult.avgFinal)}</div></div>
            <div className="text-center p-2 rounded-lg bg-quant-bg-secondary"><div className="text-muted-foreground">最好5%</div><div className="font-mono text-quant-green">${formatCurrency(simResult.best5)}</div></div>
          </div>
        </div>
      ) : (
        <div className="text-xs text-muted-foreground text-center py-4">点击运行，基于{trades.length}笔交易进行500次随机打乱模拟</div>
      )}
    </SectionCard>
  )
}

/* ── Backtest History ── */
function BacktestHistory({ items, onLoad, onDelete }: {
  items: BacktestHistoryItem[]; onLoad: (params: BacktestParams) => void; onDelete: (id: string) => void
}) {
  if (!items.length) return null
  return (
    <SectionCard title="回测历史" headerAction={<span className="text-[10px] text-muted-foreground">{items.length}条记录</span>}>
      <div className="space-y-1 max-h-48 overflow-y-auto">
        {items.map(item => (
          <div key={item.id} className="flex items-center justify-between px-3 py-2 rounded-lg bg-quant-bg-secondary border border-quant-border hover:border-quant-gold/20 transition-colors">
            <button onClick={() => onLoad(item.params)} className="flex-1 text-left min-w-0">
              <div className="flex items-center gap-2 text-xs">
                <span className="font-medium">{item.symbol}</span>
                <span className="text-muted-foreground">{item.strategyType}</span>
                <span className="text-muted-foreground">{item.interval}</span>
              </div>
              <div className="flex gap-2 text-[10px] text-muted-foreground mt-0.5">
                <span className={item.totalReturn >= 0 ? 'text-quant-green' : 'text-quant-red'}>{formatPct(item.totalReturn)}</span>
                <span>回撤 {item.maxDrawdown.toFixed(1)}%</span>
                <span>夏普 {item.sharpe.toFixed(2)}</span>
                <span>{item.trades}笔</span>
                <span>{item.date}</span>
              </div>
            </button>
            <button onClick={() => onDelete(item.id)} className="p-1 rounded text-muted-foreground hover:text-quant-red hover:bg-quant-red/10" title="删除">✕</button>
          </div>
        ))}
      </div>
    </SectionCard>
  )
}

/* ═══════════════════════════════════════════════════════════════════
   Main Page
   ═══════════════════════════════════════════════════════════════════ */
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
  const [activePreset, setActivePreset] = useState<string | null>('最近6月')

  // CRA params
  const [craParams, setCraParams] = useState<CRAParams>(DEFAULT_CRA_PARAMS)
  const setCraOrderCount = (v: number) => setCraParams(p => ({ ...p, orderCount: v }))
  const setCraFirstAmount = (v: number) => setCraParams(p => ({ ...p, firstOrderAmount: v }))
  const setCraAddSpread = (v: number) => setCraParams(p => ({ ...p, addPosSpread: v }))
  const setCraTpRatio = (v: number) => setCraParams(p => ({ ...p, tpRatio: v }))
  const setCraWaterfall = (v: number) => setCraParams(p => ({ ...p, waterfall: v }))

  // Multi-strategy comparison
  const [compareStrategies, setCompareStrategies] = useState<string[]>([])
  const [compareResults, setCompareResults] = useState<Record<string, BacktestReport | null>>({})
  const [showCompare, setShowCompare] = useState(false)

  // Optimizer
  const [showOptimizer, setShowOptimizer] = useState(false)
  const [optimizerConfig, setOptimizerConfig] = useState({ method: 'de' as 'de' | 'tpe', generations: 10, population: 20 })
  const [optimizerResult, setOptimizerResult] = useState<Record<string, unknown> | null>(null)

  // Results
  const [report, setReport] = useState<BacktestReport | null>(null)
  const [fullResult, setFullResult] = useState<BacktestResult | null>(null)
  const [equityCurve, setEquityCurve] = useState<EquityPoint[]>([])
  const [trades, setTrades] = useState<TradeRecord[]>([])
  const [activeTab, setActiveTab] = useState<TabKey>('overview')

  // Trade table sorting & filtering
  const [tradeSortKey, setTradeSortKey] = useState<'time' | 'pnl' | 'qty' | 'price'>('time')
  const [tradeSortDir, setTradeSortDir] = useState<'asc' | 'desc'>('asc')
  const [tradeFilter, setTradeFilter] = useState<'all' | 'win' | 'loss'>('all')

  // History (localStorage)
  const [history, setHistory] = useState<BacktestHistoryItem[]>(() => {
    try { return JSON.parse(localStorage.getItem('bt-history') || '[]') } catch { return [] }
  })
  const saveHistory = (rep: BacktestReport, p: BacktestParams) => {
    const item: BacktestHistoryItem = {
      id: `bt-${Date.now()}`, symbol, interval, strategyType,
      date: new Date().toLocaleDateString('zh-CN'),
      totalReturn: rep.total_return_pct, maxDrawdown: rep.max_drawdown_pct,
      sharpe: rep.sharpe_ratio, winRate: rep.win_rate_pct, trades: rep.total_trades,
      params: p,
    }
    const updated = [item, ...history].slice(0, 20)
    setHistory(updated)
    localStorage.setItem('bt-history', JSON.stringify(updated))
  }
  const deleteHistory = (id: string) => {
    const updated = history.filter(h => h.id !== id)
    setHistory(updated)
    localStorage.setItem('bt-history', JSON.stringify(updated))
  }

  const buildBacktestParams = () => ({
    symbol, interval, strategy_type: strategyType,
    initial_balance: { USDT: initialBalance },
    from: fromDate, to: toDate,
    ...craParamsToApiPayload(craParams),
  })

  const runMut = useMutation({
    mutationFn: () => backtestApi.run(buildBacktestParams()),
    onSuccess: (data: BacktestResult) => {
      setFullResult(data)
      const r = data.report as BacktestReport
      setReport(r ?? null)
      const curve = (data?.equity_curve || []).map((p: { time: number; equity: number }) => ({ time: p.time, equity: p.equity }))
      setEquityCurve(curve)
      setTrades(parseTrades(data?.trades || []))
      setActiveTab('overview')
      if (r) saveHistory(r, buildBacktestParams())
    },
  })

  const isRunning = runMut.isPending

  // Compute benchmark (buy & hold)
  const benchmarkData = useMemo(() => {
    if (!equityCurve.length) return undefined
    const firstEquity = equityCurve[0].equity
    const firstTime = equityCurve[0].time
    if (firstEquity <= 0) return undefined
    // Sample benchmark: same initial investment, same proportional growth
    return equityCurve.map(p => ({
      time: p.time,
      value: firstEquity * (p.equity / firstEquity),
    }))
  }, [equityCurve])

  const monthlyReturns = useMemo(() => {
    if (report?.monthly_returns) {
      return Object.entries(report.monthly_returns).map(([month, returnPct]) => ({ month, returnPct }))
    }
    return []
  }, [report])

  const sortedTrades = useMemo(() => {
    let filtered = trades
    if (tradeFilter === 'win') filtered = trades.filter(t => (t.pnl ?? 0) > 0)
    if (tradeFilter === 'loss') filtered = trades.filter(t => (t.pnl ?? 0) < 0)
    return [...filtered].sort((a, b) => {
      const getVal = (t: TradeRecord) => {
        if (tradeSortKey === 'pnl') return t.pnl ?? 0
        if (tradeSortKey === 'qty') return t.qty
        if (tradeSortKey === 'price') return t.exit_price ?? t.entry_price ?? 0
        return t.time
      }
      const va = getVal(a), vb = getVal(b)
      return tradeSortDir === 'asc' ? va - vb : vb - va
    })
  }, [trades, tradeSortKey, tradeSortDir, tradeFilter])

  const handlePreset = (label: string, days: number) => {
    setActivePreset(label); setFromDate(daysAgo(days)); setToDate(dateToISO(new Date()))
  }

  const handleRunCompare = async () => {
    if (compareStrategies.length < 2) return
    setCompareResults({})
    const results: Record<string, BacktestReport | null> = {}
    for (const st of compareStrategies) {
      try {
        const data = await backtestApi.run({ ...buildBacktestParams(), strategy_type: st })
        results[st] = data.report as BacktestReport ?? null
      } catch { /* skip */ }
    }
    setCompareResults(results)
  }

  const handleRunOptimizer = async () => {
    setOptimizerResult(null)
    try {
      const data = await indicatorApi.experiment.run({
        code: `// ${strategyType} optimizer`, symbol, interval, strategy_type: strategyType,
        optimizer: optimizerConfig.method,
        param_space: {
          order_count: { min: 3, max: 15, step: 1 },
          first_order_amount: { min: 50, max: 500, step: 10 },
          add_position_spread: { min: 1, max: 10, step: 0.5 },
          take_profit_ratio: { min: 0.5, max: 5, step: 0.1 },
          waterfall_protection: { min: 1, max: 10, step: 0.5 },
        },
        generations: optimizerConfig.generations, population_size: optimizerConfig.population,
        backtest_config: { initial_balance: initialBalance, from: fromDate, to: toDate },
      })
      setOptimizerResult((data as Record<string, unknown>)?.data as Record<string, unknown> || (data as Record<string, unknown>))
    } catch { /* ignore */ }
  }

  const applyOptimizerResult = (result: Record<string, unknown>) => {
    const p = (result.best_params as Record<string, number>) || {}
    if (p.order_count !== undefined) setCraOrderCount(p.order_count)
    if (p.first_order_amount !== undefined) setCraFirstAmount(p.first_order_amount)
    if (p.add_position_spread !== undefined) setCraAddSpread(p.add_position_spread)
    if (p.take_profit_ratio !== undefined) setCraTpRatio(p.take_profit_ratio)
    if (p.waterfall_protection !== undefined) setCraWaterfall(p.waterfall_protection)
  }

  const handleRun = () => {
    setReport(null); setFullResult(null); setEquityCurve([]); setTrades([])
    runMut.mutate()
  }

  const handleExportCSV = () => {
    if (!trades.length) return
    const headers = ['time', 'side', 'entry_price', 'exit_price', 'qty', 'pnl']
    const rows = trades.map(t => [new Date(t.time).toISOString(), t.side, t.entry_price ?? '', t.exit_price ?? '', t.qty, t.pnl ?? ''])
    const csv = [headers.join(','), ...rows.map(r => r.join(','))].join('\n')
    const blob = new Blob([csv], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url; a.download = `backtest-${symbol}-${interval}-${strategyType}-${Date.now()}.csv`; a.click()
    URL.revokeObjectURL(url)
  }

  const inputCls = "w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm text-white outline-none focus:border-quant-gold"

  const rating = report ? getRating(report) : null

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-5 max-w-7xl mx-auto">
        <PageHeader
          subtitle="使用真实历史数据验证策略表现"
          actions={
            <>
              {report && (
                <button onClick={handleExportCSV} className="flex items-center gap-1.5 rounded-lg border border-quant-border bg-quant-card px-3 py-2 text-sm text-muted-foreground transition-colors hover:border-quant-gold/30 hover:text-foreground">
                  <Download className="h-3.5 w-3.5" />导出 CSV
                </button>
              )}
              <button onClick={handleRun} disabled={isRunning}
                className={cn('flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-opacity', isRunning ? 'cursor-not-allowed bg-quant-gold/50' : 'bg-quant-gold text-[#0a0a0a] hover:opacity-90')}>
                {isRunning ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                {isRunning ? '回测中...' : '开始回测'}
              </button>
              {isCraStrategy && (
                <button onClick={() => setShowOptimizer(!showOptimizer)}
                  className={cn('flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors', showOptimizer ? 'border-quant-gold/30 bg-quant-gold/10 text-quant-gold' : 'border-quant-gold/30 bg-quant-gold/10 text-quant-gold hover:bg-quant-gold/20')}>
                  <Beaker className="h-3.5 w-3.5" />自动调参
                </button>
              )}
              <button onClick={() => setShowCompare(!showCompare)}
                className={cn('flex items-center gap-1.5 rounded-lg border px-4 py-2 text-sm font-medium transition-colors', showCompare ? 'border-quant-gold/30 bg-quant-gold/10 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                <GitBranch className="h-3.5 w-3.5" />策略对比
              </button>
            </>
          }
        />

        {/* ── History ── */}
        <BacktestHistory items={history} onLoad={(p: BacktestParams) => {
          setSymbol(p.symbol || symbol); setIntervalVal(p.interval || interval)
          setStrategyType(p.strategy_type || strategyType)
          setInitialBalance(p.initial_balance?.USDT || initialBalance)
          setFromDate(p.from || fromDate); setToDate(p.to || toDate)
          if (p.order_count) setCraOrderCount(p.order_count)
          if (p.first_order_amount) setCraFirstAmount(p.first_order_amount)
          if (p.add_position_spread) setCraAddSpread(p.add_position_spread)
        }} onDelete={deleteHistory} />

        {/* ── Config Form ── */}
        <SectionCard title="回测参数" bodyClassName="space-y-4">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">交易对</label>
              <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className={inputCls} placeholder="BTCUSDT" aria-label="交易对" />
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">K线周期</label>
              <select value={interval} onChange={(e) => setIntervalVal(e.target.value)} aria-label="K线周期" className={inputCls}>
                {INTERVAL_OPTIONS.map((opt) => <option key={opt.value} value={opt.value}>{opt.label} ({opt.value})</option>)}
              </select>
            </div>
            <div>
              <label className="mb-1.5 block text-xs text-muted-foreground">策略</label>
              <select value={strategyType} onChange={(e) => setStrategyType(e.target.value)} aria-label="策略类型" className={inputCls}>
                {STRATEGIES.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
              </select>
              <p className="mt-1 text-[10px] text-muted-foreground">{STRATEGIES.find(s => s.value === strategyType)?.desc || ''}</p>
            </div>
          </div>

          {/* Strategy Compare */}
          {showCompare && (
            <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-semibold text-quant-gold">选择要对比的策略（至少2个）</span>
                <button onClick={handleRunCompare} disabled={compareStrategies.length < 2}
                  className={cn('px-3 py-1.5 rounded-lg text-xs font-medium transition-colors', compareStrategies.length < 2 ? 'bg-quant-bg-secondary text-muted-foreground cursor-not-allowed' : 'bg-quant-gold text-quant-bg hover:opacity-90')}>运行对比</button>
              </div>
              <div className="flex flex-wrap gap-2">
                {STRATEGIES.map((s) => (
                  <label key={s.value} className={cn('flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs border cursor-pointer transition-colors', compareStrategies.includes(s.value) ? 'border-quant-gold/30 bg-quant-gold/10 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>
                    <input type="checkbox" className="hidden" checked={compareStrategies.includes(s.value)} onChange={(e) => setCompareStrategies(prev => e.target.checked ? [...prev, s.value] : prev.filter(v => v !== s.value))} />
                    {s.label}
                  </label>
                ))}
              </div>
              {Object.keys(compareResults).length > 0 && (
                <div className="grid grid-cols-1 gap-2">
                  {Object.entries(compareResults).map(([st, r]) => (
                    <div key={st} className="flex items-center justify-between p-2.5 rounded-lg border border-quant-border bg-quant-bg-secondary">
                      <span className="text-xs font-medium">{STRATEGIES.find(s => s.value === st)?.label || st}</span>
                      <div className="flex items-center gap-3 text-[11px]">
                        {r ? (<><span className={r.total_return_pct >= 0 ? 'text-quant-green' : 'text-quant-red'}>收益: {formatPct(r.total_return_pct)}</span><span className="text-muted-foreground">回撤: {r.max_drawdown_pct.toFixed(1)}%</span><span className="text-muted-foreground">夏普: {r.sharpe_ratio.toFixed(2)}</span></>) : <span className="text-muted-foreground">无数据</span>}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Optimizer */}
          {showOptimizer && (
            <div className="rounded-xl border border-quant-border bg-quant-bg-tertiary p-4 space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-xs font-semibold text-quant-gold">参数优化器</span>
                <button onClick={handleRunOptimizer} className="px-3 py-1.5 rounded-lg text-xs font-medium bg-quant-gold text-quant-bg hover:opacity-90">开始优化</button>
              </div>
              <div className="grid grid-cols-3 gap-3">
                <div><label className="mb-1 block text-[10px] text-muted-foreground">算法</label><select value={optimizerConfig.method} onChange={(e) => setOptimizerConfig(prev => ({ ...prev, method: e.target.value as 'de' | 'tpe' }))} className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold"><option value="de">差分进化 (DE)</option><option value="tpe">贝叶斯 (TPE)</option></select></div>
                <div><label className="mb-1 block text-[10px] text-muted-foreground">代数</label><input type="number" min={5} max={50} value={optimizerConfig.generations} onChange={(e) => setOptimizerConfig(prev => ({ ...prev, generations: Number(e.target.value) }))} className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" /></div>
                <div><label className="mb-1 block text-[10px] text-muted-foreground">种群</label><input type="number" min={10} max={100} value={optimizerConfig.population} onChange={(e) => setOptimizerConfig(prev => ({ ...prev, population: Number(e.target.value) }))} className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" /></div>
              </div>
              {optimizerResult && (
                <div className="p-3 rounded-lg border border-quant-gold/20 bg-quant-gold/5">
                  <div className="text-xs font-medium text-quant-gold mb-2">优化结果</div>
                  {!!optimizerResult.best_params && (
                    <div className="space-y-1">
                      <div className="text-[11px] text-muted-foreground">最佳参数:</div>
                      <div className="flex flex-wrap gap-2">{[...Object.entries(optimizerResult.best_params as Record<string, number>)].map(([k, v]) => <span key={k} className="px-2 py-0.5 rounded bg-quant-bg-secondary text-[11px] text-foreground">{k}: {typeof v === 'number' ? v.toFixed(2) : String(v)}</span>)}</div>
                      <button onClick={() => applyOptimizerResult(optimizerResult)} className="mt-2 px-3 py-1.5 rounded-lg text-xs font-medium bg-quant-gold text-quant-bg hover:opacity-90">应用参数</button>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Advanced CRA params */}
          {isCraStrategy && (
          <div className="space-y-3">
            <div className="flex items-center gap-2 text-[11px] text-muted-foreground">
              <SlidersHorizontal className="w-3.5 h-3.5" />
              展开分组调整策略参数
            </div>
            <CollapsibleSection title="CRA 量化参数" count={4} defaultOpen>
              <CRAParamForm value={craParams} onChange={setCraParams} />
            </CollapsibleSection>
            <CollapsibleSection title="趋势指标时间框架" count={1}>
              {craParams.trendInd && (
                <div className="flex gap-2">
                  {(['5m', '15m', '30m', '60m'] as const).map((tf) => (
                    <button key={tf} onClick={() => setCraParams(p => ({ ...p, trendTf: tf }))} className={cn('flex-1 py-2 rounded-lg text-xs border transition-colors', craParams.trendTf === tf ? 'bg-quant-gold/10 border-quant-gold/20 text-quant-gold' : 'border-quant-border text-muted-foreground hover:text-foreground')}>{tf}</button>
                  ))}
                </div>
              )}
            </CollapsibleSection>
          </div>
          )}

          {/* Date & Balance */}
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div><label className="mb-1.5 block text-xs text-muted-foreground">初始资金 (USDT)</label><input type="number" min={100} value={initialBalance} onChange={(e) => setInitialBalance(Number(e.target.value))} className={inputCls} aria-label="初始资金" /></div>
            <div><label className="mb-1.5 block text-xs text-muted-foreground">开始日期</label><input type="date" value={fromDate} onChange={(e) => { setFromDate(e.target.value); setActivePreset(null) }} className={inputCls} aria-label="开始日期" /></div>
            <div><label className="mb-1.5 block text-xs text-muted-foreground">结束日期</label><input type="date" value={toDate} onChange={(e) => { setToDate(e.target.value); setActivePreset(null) }} className={inputCls} aria-label="结束日期" /></div>
          </div>
          <div className="flex flex-wrap gap-1.5">
            <span className="text-[10px] text-muted-foreground mr-1 self-center">快捷:</span>
            {PRESET_DATES.map((p) => (
              <button key={p.label} onClick={() => handlePreset(p.label, p.days)}
                className={cn('px-2.5 py-1 rounded text-[11px] transition-colors', activePreset === p.label ? 'bg-quant-gold/15 text-quant-gold border border-quant-gold/30' : 'bg-quant-bg-tertiary text-muted-foreground border border-quant-border hover:border-quant-gold/30')}>
                {p.label}
              </button>
            ))}
          </div>
        </SectionCard>

        {/* ── Info Bar ── */}
        <div className="flex items-center gap-2 rounded-lg border border-quant-border bg-quant-bg-secondary px-4 py-2 text-xs text-muted-foreground">
          <Database className="h-3.5 w-3.5 text-quant-gold/70" />
          <span>数据来源: <strong className="text-foreground">Binance 真实历史数据</strong></span>
          <span className="text-quant-border">|</span>
          <span>交易对、周期、日期范围可在上方配置</span>
        </div>

        {/* ── Assumptions ── */}
        <BacktestAssumptions commission={0.001} slippage={0.0005} leverage={craParams.leverage} initialBalance={initialBalance} interval={interval} />

        {/* ── Error ── */}
        {runMut.isError && (
          <div role="alert" className="flex items-start gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            <AlertCircle className="h-4 w-4 mt-0.5 shrink-0" />
            <div><p className="font-medium">回测执行失败</p><p className="mt-0.5 text-xs text-red-400/70">{(runMut.error as Error)?.message || '未知错误'}</p></div>
          </div>
        )}

        {/* ═══════ RESULTS ═══════ */}
        {report && (
          <>
            {/* Rating & Score */}
            {rating && (
              <div className={cn('flex items-center gap-3 rounded-lg border px-4 py-2.5', rating.border, rating.bg)}>
                <Gauge className={cn('h-4 w-4', rating.color)} />
                <span className={cn('text-sm font-semibold', rating.color)}>策略评级: {rating.label}</span>
                <span className="text-[11px] text-muted-foreground ml-auto">{fullResult?.start_date} — {fullResult?.end_date} · {fullResult?.source}</span>
              </div>
            )}

            {/* Tab bar */}
            <div className="flex gap-1 rounded-xl border border-quant-border bg-quant-bg-secondary p-1">
              {RESULT_TABS.map(tab => (
                <button key={tab.key} onClick={() => setActiveTab(tab.key)}
                  className={cn('flex-1 flex items-center justify-center gap-1.5 py-2 rounded-lg text-xs font-medium transition-colors', activeTab === tab.key ? 'bg-quant-gold/15 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
                  <tab.icon className="h-3.5 w-3.5" />{tab.label}
                </button>
              ))}
            </div>

            {/* ─── TAB: Overview ─── */}
            {activeTab === 'overview' && (
              <div className="space-y-5">
                {/* KPI Grid */}
                <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-4">
                  <KPICard icon={<TrendingUp className="h-4 w-4 text-muted-foreground" />} label="总收益率" value={formatPct(report.total_return_pct)} trend={report.total_return_pct >= 0 ? 'up' : 'down'} />
                  <KPICard icon={<DollarSign className="h-4 w-4 text-muted-foreground" />} label="最终权益" value={`$${formatCurrency(report.final_equity)}`} />
                  <KPICard icon={<TrendingDown className="h-4 w-4 text-muted-foreground" />} label="最大回撤" value={formatAbsPct(report.max_drawdown_pct)} trend="down" />
                  <KPICard icon={<Target className="h-4 w-4 text-muted-foreground" />} label="夏普比率" value={report.sharpe_ratio.toFixed(2)} trend={report.sharpe_ratio >= 1 ? 'up' : 'down'} />
                  <KPICard icon={<Gauge className="h-4 w-4 text-muted-foreground" />} label="索提诺" value={toFixed(report.sortino_ratio)} trend={report.sortino_ratio && report.sortino_ratio >= 1 ? 'up' : 'neutral'} />
                  <KPICard icon={<Shield className="h-4 w-4 text-muted-foreground" />} label="卡尔玛" value={toFixed(report.calmar_ratio)} trend={report.calmar_ratio && report.calmar_ratio >= 0.5 ? 'up' : 'neutral'} />
                  <KPICard icon={<Percent className="h-4 w-4 text-muted-foreground" />} label="胜率" value={`${report.win_rate_pct.toFixed(1)}%`} trend={report.win_rate_pct >= 50 ? 'up' : 'down'} />
                  <KPICard icon={<BarChart3 className="h-4 w-4 text-muted-foreground" />} label="总交易数" value={String(report.total_trades)} />
                  <KPICard icon={<Activity className="h-4 w-4 text-muted-foreground" />} label="盈亏比" value={`${report.profit_factor.toFixed(2)}:1`} trend={report.profit_factor >= 1.2 ? 'up' : 'down'} />
                  <KPICard icon={<Zap className="h-4 w-4 text-muted-foreground" />} label="恢复因子" value={toFixed(report.recovery_factor)} />
                  <KPICard icon={<AlertTriangle className="h-4 w-4 text-muted-foreground" />} label="VaR (95%)" value={toFixed(report.var_95, 1) + '%'} trend="down" />
                  <KPICard icon={<Clock className="h-4 w-4 text-muted-foreground" />} label="年化波动" value={toFixed(report.volatility, 1) + '%'} />
                </div>

                {/* Edge Analysis + Trade Stats */}
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  <SectionCard title="交易统计">
                    <div className="grid grid-cols-2 gap-3 text-xs">
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">盈利交易</span><span className="text-quant-green font-mono">{report.winning_trades ?? '-'}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">亏损交易</span><span className="text-quant-red font-mono">{report.losing_trades ?? '-'}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">平均盈利</span><span className="text-quant-green font-mono">${toFixed(report.avg_win, 0)}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">平均亏损</span><span className="text-quant-red font-mono">${toFixed(report.avg_loss, 0)}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">最佳交易</span><span className="text-quant-green font-mono">${toFixed(report.best_trade, 0)}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">最差交易</span><span className="text-quant-red font-mono">${toFixed(report.worst_trade, 0)}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">最长连胜</span><span className="text-quant-green font-mono">{report.max_consec_wins ?? '-'}</span></div>
                      <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">最长连亏</span><span className="text-quant-red font-mono">{report.max_consec_loss ?? '-'}</span></div>
                    </div>
                  </SectionCard>

                  <SectionCard title="Edge 分析">
                    <div className="space-y-2 text-xs">
                      {(() => {
                        const w = trades.filter(t => (t.pnl || 0) > 0)
                        const l = trades.filter(t => (t.pnl || 0) < 0)
                        const wr = trades.length > 0 ? (w.length / trades.length * 100) : 0
                        const avgW = w.length > 0 ? w.reduce((s, t) => s + (t.pnl || 0), 0) / w.length : 0
                        const avgL = l.length > 0 ? Math.abs(l.reduce((s, t) => s + (t.pnl || 0), 0) / l.length) : 0
                        const exp = (wr / 100) * avgW - ((100 - wr) / 100) * avgL
                        return (
                          <>
                            <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">期望值</span><span className={cn('font-mono', exp >= 0 ? 'text-quant-green' : 'text-quant-red')}>${exp.toFixed(2)}</span></div>
                            <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">胜率</span><span className="font-mono text-foreground">{wr.toFixed(1)}%</span></div>
                            <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">平均盈亏比</span><span className="font-mono text-foreground">{avgL > 0 ? (avgW / avgL).toFixed(2) : '-'}:1</span></div>
                            <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">盈利因子</span><span className="font-mono text-quant-green">{report.profit_factor.toFixed(2)}</span></div>
                            <div className="flex justify-between p-2 rounded bg-quant-bg-secondary"><span className="text-muted-foreground">风险回报比</span><span className="font-mono text-foreground">{report.calmar_ratio ? report.calmar_ratio.toFixed(2) : '-'}</span></div>
                          </>
                        )
                      })()}
                    </div>
                  </SectionCard>
                </div>
              </div>
            )}

            {/* ─── TAB: Charts ─── */}
            {activeTab === 'charts' && (
              <div className="space-y-5">
                <SectionCard title="权益曲线 · 回撤曲线 · 基准对比">
                  <PerformanceChart data={equityCurve} trades={trades} benchmarkData={benchmarkData} isLoading={isRunning} height={380} />
                </SectionCard>
                <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
                  <SectionCard title="日收益率分布">
                    <DailyReturnsChart equityCurve={equityCurve} />
                  </SectionCard>
                  <SectionCard title="月度盈亏热力图">
                    <MonthlyHeatmap data={report.monthly_returns} isLoading={isRunning} />
                  </SectionCard>
                </div>
              </div>
            )}

            {/* ─── TAB: Trades ─── */}
            {activeTab === 'trades' && (
              <div className="space-y-5">
                {/* Filters & Sorting */}
                <div className="flex items-center gap-3 flex-wrap">
                  <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                    <Filter className="h-3 w-3" />
                    {(['all', 'win', 'loss'] as const).map(f => (
                      <button key={f} onClick={() => setTradeFilter(f)}
                        className={cn('px-2 py-1 rounded-md border transition-colors', tradeFilter === f ? 'border-quant-gold/30 bg-quant-gold/10 text-quant-gold' : 'border-quant-border hover:border-quant-gold/20')}>
                        {f === 'all' ? '全部' : f === 'win' ? '盈利' : '亏损'}
                      </button>
                    ))}
                  </div>
                  <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                    <ArrowUpDown className="h-3 w-3" />
                    <select value={tradeSortKey} onChange={(e) => setTradeSortKey(e.target.value as typeof tradeSortKey)} className="bg-quant-bg border border-quant-border rounded px-2 py-1 text-[11px] outline-none focus:border-quant-gold">
                      <option value="time">按时间</option>
                      <option value="pnl">按盈亏</option>
                      <option value="qty">按数量</option>
                      <option value="price">按价格</option>
                    </select>
                    <button onClick={() => setTradeSortDir(d => d === 'asc' ? 'desc' : 'asc')}
                      className="px-2 py-1 rounded border border-quant-border hover:border-quant-gold/20">
                      {tradeSortDir === 'asc' ? '↑ 升序' : '↓ 降序'}
                    </button>
                    <span className="ml-2">共 {sortedTrades.length} 笔</span>
                  </div>
                </div>

                {/* PnL Distribution */}
                {trades.length >= 5 && (
                  <SectionCard title="盈亏分布">
                    <PnLDistribution trades={trades} />
                  </SectionCard>
                )}

                {/* Trade Table */}
                <SectionCard title="交易记录" headerAction={<span className="text-xs text-muted-foreground">总盈亏: <span className={cn('font-mono', report.total_return_pct >= 0 ? 'text-quant-green' : 'text-quant-red')}>{formatPct(report.total_return_pct)}</span></span>}>
                  <div className="overflow-x-auto">
                    <DataTable<TradeRecord>
                      data={sortedTrades}
                      columns={[
                        { key: 'time', title: '时间', width: '160px', render: (t) => <span className="text-muted-foreground text-xs">{new Date(t.time).toLocaleString()}</span> },
                        { key: 'side', title: '方向', width: '70px', render: (t) => <span className={cn('px-1.5 py-0.5 rounded text-[10px] font-medium', t.side === 'buy' ? 'bg-quant-green/10 text-quant-green' : 'bg-quant-red/10 text-quant-red')}>{t.side === 'buy' ? '买入' : '卖出'}</span> },
                        { key: 'entry', title: '入场价', width: '100px', render: (t) => <span className="font-mono text-xs">{t.entry_price ? `$${t.entry_price.toFixed(2)}` : '-'}</span> },
                        { key: 'exit', title: '出场价', width: '100px', render: (t) => <span className="font-mono text-xs">{t.exit_price ? `$${t.exit_price.toFixed(2)}` : '-'}</span> },
                        { key: 'qty', title: '数量', width: '80px', render: (t) => <span className="font-mono text-xs">{t.qty}</span> },
                        { key: 'pnl', title: '盈亏', width: '100px', render: (t) => <span className={cn('font-mono text-xs font-medium', (t.pnl || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>{t.pnl != null ? `${t.pnl >= 0 ? '+' : ''}$${t.pnl.toFixed(2)}` : '-'}</span> },
                      ]}
                      keyExtractor={(t, i) => `${i}-${t.time}-${t.bar}`}
                      emptyText="暂无符合条件的交易记录"
                    />
                  </div>
                </SectionCard>
              </div>
            )}

            {/* ─── TAB: Risk ─── */}
            {activeTab === 'risk' && (
              <div className="space-y-5">
                {/* Risk Metrics */}
                <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
                  <KPICard icon={<AlertTriangle className="h-4 w-4 text-muted-foreground" />} label="VaR (95%)" value={toFixed(report.var_95, 1) + '%'} trend="down" />
                  <KPICard icon={<Shield className="h-4 w-4 text-muted-foreground" />} label="CVaR (95%)" value={toFixed(report.cvar_95, 1) + '%'} trend="down" />
                  <KPICard icon={<Activity className="h-4 w-4 text-muted-foreground" />} label="年化波动率" value={toFixed(report.volatility, 1) + '%'} />
                  <KPICard icon={<TrendingDown className="h-4 w-4 text-muted-foreground" />} label="最大回撤" value={formatAbsPct(report.max_drawdown_pct)} trend="down" />
                </div>

                {/* Drawdown Analysis */}
                <SectionCard title="回撤分析">
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
                    <div className="p-3 rounded-lg bg-quant-bg-secondary">
                      <div className="text-muted-foreground mb-1">最大回撤</div>
                      <div className="font-mono text-quant-red text-lg font-bold">{formatAbsPct(report.max_drawdown_pct)}</div>
                    </div>
                    <div className="p-3 rounded-lg bg-quant-bg-secondary">
                      <div className="text-muted-foreground mb-1">恢复因子</div>
                      <div className="font-mono text-foreground text-lg font-bold">{toFixed(report.recovery_factor)}</div>
                    </div>
                    <div className="p-3 rounded-lg bg-quant-bg-secondary">
                      <div className="text-muted-foreground mb-1">卡尔玛比率</div>
                      <div className="font-mono text-foreground text-lg font-bold">{toFixed(report.calmar_ratio)}</div>
                    </div>
                    <div className="p-3 rounded-lg bg-quant-bg-secondary">
                      <div className="text-muted-foreground mb-1">索提诺比率</div>
                      <div className="font-mono text-foreground text-lg font-bold">{toFixed(report.sortino_ratio)}</div>
                    </div>
                  </div>
                </SectionCard>

                {/* Yearly Returns */}
                {report.yearly_returns && Object.keys(report.yearly_returns).length > 0 && (
                  <SectionCard title="年度收益">
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                      {Object.entries(report.yearly_returns).map(([year, ret]) => (
                        <div key={year} className="p-3 rounded-lg bg-quant-bg-secondary text-center">
                          <div className="text-[10px] text-muted-foreground">{year}</div>
                          <div className={cn('font-mono font-bold', ret >= 0 ? 'text-quant-green' : 'text-quant-red')}>{formatPct(ret)}</div>
                        </div>
                      ))}
                    </div>
                  </SectionCard>
                )}

                {/* Monthly Heatmap */}
                <SectionCard title="月度盈亏热力图">
                  <MonthlyHeatmap data={report.monthly_returns} isLoading={isRunning} />
                </SectionCard>

                {/* Monte Carlo */}
                <MonteCarloSim trades={trades} initialEquity={report.initial_balance} />
              </div>
            )}
          </>
        )}

        {!report && !isRunning && !runMut.isError && (
          <EmptyState icon={<Activity className="h-6 w-6" />} title="开始回测"
            description="选择交易对、周期、策略和日期范围，使用 Binance 真实历史数据验证策略表现"
            actionLabel="开始回测" onAction={handleRun} />
        )}
      </div>
    </div>
  )
}

/* ── Internal helpers ─────────────────────────────────────────────── */
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
