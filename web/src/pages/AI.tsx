import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import {
  Cpu,
  Send,
  Sparkles,
  BarChart3,
  Code,
  Play,
  RefreshCw,
  TrendingUp,
  TrendingDown,
  Minus,
  Calendar,
  Search,
  Zap,
  History,
  LineChart,
  Gauge,
  Building2,
  Star,
  Plus,
  Trash2,
  CheckCircle2,
  Wallet,
  Clock,
  ChevronUp,
  ChevronDown,
  Loader2,
  BrainCircuit,
  ArrowUp,
  ArrowDown,
  X,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { aiApi, marketApi, mlApi } from '@/lib/api'

/* ------------------------------------------------------------------ */
/*  Types                                                              */
/* ------------------------------------------------------------------ */

type HeatmapType = 'us_stocks' | 'hk_stocks' | 'crypto' | 'commodities' | 'sectors' | 'forex'

interface MarketIndex {
  flag: string
  symbol: string
  price: number
  change: number
}

interface HeatmapItem {
  name: string
  name_cn?: string
  name_en?: string
  fullName?: string
  price?: number
  value: number
}

interface CalendarEvent {
  id: string
  date: string
  time?: string
  country: string
  name: string
  name_en?: string
  importance: 'high' | 'medium' | 'low'
  actual?: string | number
  forecast?: string | number
  actual_impact?: 'bullish' | 'bearish' | 'neutral'
  expected_impact?: 'bullish' | 'bearish' | 'neutral'
}

interface WatchlistItem {
  market: string
  symbol: string
  name?: string
  price?: number
  change?: number
  changePercent?: number
}

interface WatchlistPrice {
  price: number
  change: number
}

interface PositionSummary {
  quantity: number
  avgEntry: number
  pnl: number
  pnlPercent: number
  monitorCount?: number
  activeMonitorCount?: number
  nextRunAtText?: string
}

interface AIModelAnalysis {
  model: string
  name: string
  sentiment: 'bullish' | 'bearish' | 'neutral'
  analysis: string
  content: string
}

interface AnalysisResult {
  symbol: string
  consensus: 'bullish' | 'bearish' | 'neutral'
  analyses: AIModelAnalysis[]
}

/* ------------------------------------------------------------------ */
/*  Constants                                                          */
/* ------------------------------------------------------------------ */

const HEATMAP_TABS: { key: HeatmapType; label: string }[] = [
  { key: 'us_stocks', label: '美股' },
  { key: 'hk_stocks', label: '港股' },
  { key: 'crypto', label: '加密' },
  { key: 'commodities', label: '商品' },
  { key: 'sectors', label: '板块' },
  { key: 'forex', label: '外汇' },
]

const COUNTRY_FLAGS: Record<string, string> = {
  US: '🇺🇸', CN: '🇨🇳', EU: '🇪🇺', JP: '🇯🇵', UK: '🇬🇧', DE: '🇩🇪', AU: '🇦🇺', CA: '🇨🇦',
}

const MARKET_COLORS: Record<string, string> = {
  USStock: 'bg-green-500',
  CNStock: 'bg-blue-500',
  HKStock: 'bg-indigo-500',
  Crypto: 'bg-purple-500',
  Forex: 'bg-yellow-500',
  Futures: 'bg-cyan-500',
}

const MARKET_NAMES: Record<string, string> = {
  USStock: '美股', CNStock: 'A股', HKStock: '港股', Crypto: '加密', Forex: '外汇', Futures: '期货',
}

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

function formatNum(n: number | undefined | null, digits = 2): string {
  if (n === undefined || n === null || Number.isNaN(n)) return '--'
  return Number(n).toFixed(digits)
}

function formatPrice(price?: number): string {
  if (!price) return '--'
  if (price >= 10000) return (price / 1000).toFixed(1) + 'K'
  if (price >= 1000) return price.toFixed(0)
  return price.toFixed(2)
}

function formatHeatmapPrice(price: number, type: HeatmapType): string {
  const prefix = type === 'hk_stocks' ? 'HK$' : '$'
  if (price >= 10000) return prefix + (price / 1000).toFixed(1) + 'K'
  if (price >= 1000) return prefix + price.toFixed(0)
  if (price >= 1) return prefix + price.toFixed(2)
  return prefix + price.toFixed(4)
}

function getFearGreedClass(val?: number): string {
  if (!val) return ''
  if (val <= 25) return 'extreme-fear'
  if (val <= 45) return 'fear'
  if (val <= 55) return 'neutral'
  if (val <= 75) return 'greed'
  return 'extreme-greed'
}

function getVixClass(val?: number): string {
  if (!val) return ''
  if (val < 15) return 'low'
  if (val < 25) return 'medium'
  return 'high'
}

function getHeatmapStyle(value: number, isDark: boolean) {
  const v = parseFloat(String(value)) || 0
  const intensity = Math.min(Math.abs(v) / 5, 1)
  if (v >= 0) {
    const color = isDark ? (v > 2 ? '#fff' : '#4ade80') : v > 2 ? '#fff' : '#166534'
    return { background: `rgba(34, 197, 94, ${0.15 + intensity * 0.6})`, color }
  }
  const color = isDark ? (v < -2 ? '#fff' : '#f87171') : v < -2 ? '#fff' : '#991b1b'
  return { background: `rgba(239, 68, 68, ${0.15 + intensity * 0.6})`, color }
}

function getImpactClass(evt: CalendarEvent): 'bullish' | 'bearish' | 'neutral' {
  return evt.actual_impact || evt.expected_impact || 'neutral'
}

function formatCalendarDate(dateStr?: string): string {
  if (!dateStr) return ''
  try {
    const date = new Date(dateStr)
    const today = new Date()
    const tomorrow = new Date(today)
    tomorrow.setDate(tomorrow.getDate() + 1)
    if (date.toDateString() === today.toDateString()) return '今天'
    if (date.toDateString() === tomorrow.toDateString()) return '明天'
    return `${date.getMonth() + 1}/${date.getDate()}`
  } catch {
    return dateStr
  }
}

function getHeatmapName(item: HeatmapItem, type: HeatmapType, isZh: boolean): string {
  if (type === 'hk_stocks') return isZh ? item.name_cn || item.fullName || item.name : item.name || item.fullName || ''
  if (type === 'us_stocks') return item.name || item.fullName || ''
  if (type === 'sectors' || type === 'commodities' || type === 'forex') {
    return isZh ? item.name_cn || item.name : item.name_en || item.name
  }
  return item.name
}

/* ------------------------------------------------------------------ */
/*  Skeleton components                                                */
/* ------------------------------------------------------------------ */

function SkeletonBox() {
  return (
    <div className="flex flex-col items-center gap-1 px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]">
      <div className="w-8 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-10 h-3 rounded bg-quant-border animate-pulse" />
    </div>
  )
}

function SkeletonCell() {
  return (
    <div className="flex flex-col items-center justify-center gap-1 rounded-md p-2 bg-quant-bg-secondary">
      <div className="w-3/5 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-4/5 h-2.5 rounded bg-quant-border animate-pulse" />
    </div>
  )
}

function SkeletonCalItem() {
  return (
    <div className="flex items-center gap-2 py-1.5 border-b border-quant-border/50">
      <div className="w-8 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-8 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-4 h-2 rounded bg-quant-border animate-pulse" />
      <div className="flex-1 h-2 rounded bg-quant-border animate-pulse" />
      <div className="w-10 h-2 rounded bg-quant-border animate-pulse" />
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Sub-components                                                     */
/* ------------------------------------------------------------------ */

function TopIndexBar({
  marketData,
  loadingSentiment,
  loadingIndices,
  onRefresh,
  loadingMarket,
}: {
  marketData: { fearGreed?: number; vix?: number; dxy?: number; indices: MarketIndex[] }
  loadingSentiment: boolean
  loadingIndices: boolean
  onRefresh: () => void
  loadingMarket: boolean
}) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-quant-border bg-quant-bg-tertiary">
      {loadingSentiment ? (
        <>
          <SkeletonBox />
          <SkeletonBox />
          <SkeletonBox />
        </>
      ) : (
        <>
          <div
            className={cn(
              'indicator-box flex flex-col items-center px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]',
              getFearGreedClass(marketData.fearGreed)
            )}
          >
            <span className="text-[9px] text-muted-foreground uppercase tracking-wide">恐惧贪婪</span>
            <span className="text-[13px] font-bold text-foreground">{marketData.fearGreed ?? '--'}</span>
          </div>
          <div
            className={cn(
              'indicator-box flex flex-col items-center px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]',
              getVixClass(marketData.vix)
            )}
          >
            <span className="text-[9px] text-muted-foreground uppercase tracking-wide">VIX</span>
            <span className="text-[13px] font-bold text-foreground">{marketData.vix ?? '--'}</span>
          </div>
          <div className="indicator-box flex flex-col items-center px-2.5 py-1 bg-quant-card border border-quant-border rounded-md min-w-[50px]">
            <span className="text-[9px] text-muted-foreground uppercase tracking-wide">DXY</span>
            <span className="text-[13px] font-bold text-quant-blue">{marketData.dxy ?? '--'}</span>
          </div>
        </>
      )}

      {/* Indices Marquee */}
      <div className="flex-1 overflow-hidden min-w-0">
        {loadingIndices ? (
          <div className="flex items-center justify-center h-full text-xs text-muted-foreground gap-1">
            <Loader2 className="w-3 h-3 animate-spin" /> 加载中...
          </div>
        ) : marketData.indices.length > 0 ? (
          <div className="flex gap-2 animate-marquee whitespace-nowrap">
            {[...marketData.indices, ...marketData.indices].map((idx, i) => (
              <div
                key={`${idx.symbol}-${i}`}
                className="inline-flex items-center gap-1 px-2 py-1 bg-quant-card border border-quant-border rounded text-[11px]"
              >
                <span>{idx.flag}</span>
                <span className="text-muted-foreground font-medium">{idx.symbol}</span>
                <span className="text-foreground font-semibold">{formatPrice(idx.price)}</span>
                <span className={cn('font-semibold flex items-center gap-0.5', idx.change >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                  {idx.change >= 0 ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                  {Math.abs(idx.change).toFixed(2)}%
                </span>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-xs text-muted-foreground text-center">--</div>
        )}
      </div>

      <button
        onClick={onRefresh}
        disabled={loadingMarket}
        className="text-muted-foreground hover:text-foreground transition-colors shrink-0 disabled:opacity-50"
      >
        <RefreshCw className={cn('w-4 h-4', loadingMarket && 'animate-spin')} />
      </button>
    </div>
  )
}

function HeatmapSection({
  heatmapType,
  setHeatmapType,
  currentHeatmap,
  loadingHeatmap,
  isDark,
}: {
  heatmapType: HeatmapType
  setHeatmapType: (t: HeatmapType) => void
  currentHeatmap: HeatmapItem[]
  loadingHeatmap: boolean
  isDark: boolean
}) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3.5 shadow-sm">
      <div className="mb-2.5">
        <div className="flex flex-wrap gap-1 bg-quant-bg-secondary rounded-lg p-1">
          {HEATMAP_TABS.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setHeatmapType(tab.key)}
              className={cn(
                'flex-1 min-w-[calc(33.33%-4px)] max-w-[calc(33.33%-2px)] text-[10px] font-semibold h-[26px] rounded-md transition-all whitespace-nowrap overflow-hidden text-ellipsis',
                heatmapType === tab.key
                  ? 'bg-quant-card text-foreground shadow-sm border border-quant-border'
                  : 'text-muted-foreground hover:text-foreground bg-transparent'
              )}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>
      <div className="grid grid-cols-3 gap-1">
        {loadingHeatmap ? (
          Array.from({ length: 12 }).map((_, i) => <SkeletonCell key={i} />)
        ) : currentHeatmap.length > 0 ? (
          currentHeatmap.slice(0, 12).map((item, i) => (
            <div
              key={i}
              className="rounded-md p-1.5 text-center text-[9px] transition-transform hover:scale-[1.03] cursor-default"
              style={getHeatmapStyle(item.value, isDark)}
            >
              <span className="block font-semibold truncate">{getHeatmapName(item, heatmapType, true)}</span>
              {item.price != null && <span className="block opacity-80">{formatHeatmapPrice(item.price, heatmapType)}</span>}
              <span className="block font-bold text-[10px]">
                {item.value >= 0 ? '+' : ''}
                {formatNum(item.value)}%
              </span>
            </div>
          ))
        ) : (
          <div className="col-span-3 text-center py-5 text-xs text-muted-foreground">暂无数据</div>
        )}
      </div>
    </div>
  )
}

function EconomicCalendar({
  events,
  loadingCalendar,
}: {
  events: CalendarEvent[]
  loadingCalendar: boolean
}) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3.5 shadow-sm flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="mb-2 flex items-center gap-1.5 text-xs font-bold text-foreground">
        <Calendar className="w-3.5 h-3.5 text-quant-gold" />
        财经日历
      </div>
      <div className="flex-1 overflow-y-auto min-h-0 scrollbar-thin">
        {loadingCalendar ? (
          Array.from({ length: 5 }).map((_, i) => <SkeletonCalItem key={i} />)
        ) : events.length > 0 ? (
          events.slice(0, 10).map((evt) => (
            <div
              key={evt.id}
              className={cn(
                'flex items-center gap-1.5 py-1.5 border-b border-quant-border/50 text-[10px] last:border-b-0',
                evt.importance === 'high' && 'border-l-[3px] border-l-quant-red pl-2 -ml-1',
                evt.importance === 'medium' && 'border-l-[3px] border-l-yellow-500 pl-2 -ml-1',
                evt.importance === 'low' && 'border-l-[3px] border-l-quant-green pl-2 -ml-1'
              )}
            >
              <span className="text-[9px] text-muted-foreground min-w-[32px] font-medium">{formatCalendarDate(evt.date)}</span>
              <span className="text-muted-foreground min-w-[36px] font-medium">{evt.time || '--:--'}</span>
              <span className="text-xs">{COUNTRY_FLAGS[evt.country] || '🌍'}</span>
              <span className="flex-1 text-foreground truncate">{evt.name}</span>
              <span
                className={cn(
                  'font-semibold text-[10px] flex items-center gap-0.5',
                  getImpactClass(evt) === 'bullish' && 'text-quant-green',
                  getImpactClass(evt) === 'bearish' && 'text-quant-red',
                  getImpactClass(evt) === 'neutral' && 'text-muted-foreground'
                )}
              >
                {getImpactClass(evt) === 'bullish' ? (
                  <ArrowUp className="w-3 h-3" />
                ) : getImpactClass(evt) === 'bearish' ? (
                  <ArrowDown className="w-3 h-3" />
                ) : (
                  <Minus className="w-3 h-3" />
                )}
                {evt.actual ?? evt.forecast ?? '--'}
              </span>
            </div>
          ))
        ) : (
          <div className="text-center py-5 text-xs text-muted-foreground">暂无事件</div>
        )}
      </div>
    </div>
  )
}

function AnalysisPlaceholder({ onAddStock, onAnalyze, canAnalyze }: {
  onAddStock: () => void
  onAnalyze: () => void
  canAnalyze: boolean
}) {
  return (
    <div className="flex items-center justify-center h-full min-h-[300px] relative overflow-hidden">
      {/* Background circles + grid */}
      <div className="absolute inset-0 pointer-events-none">
        <div
          className="absolute rounded-full opacity-50 animate-hero-float"
          style={{
            width: 320,
            height: 320,
            top: -80,
            right: -60,
            background: 'radial-gradient(circle, rgba(234,179,8,0.10) 0%, transparent 70%)',
          }}
        />
        <div
          className="absolute rounded-full opacity-50 animate-hero-float-reverse"
          style={{
            width: 240,
            height: 240,
            bottom: -40,
            left: -40,
            background: 'radial-gradient(circle, rgba(168,85,247,0.08) 0%, transparent 70%)',
          }}
        />
        <div
          className="absolute inset-0"
          style={{
            backgroundImage:
              'linear-gradient(rgba(234,179,8,0.03) 1px, transparent 1px), linear-gradient(90deg, rgba(234,179,8,0.03) 1px, transparent 1px)',
            backgroundSize: '32px 32px',
          }}
        />
      </div>

      <div className="relative text-center px-8 py-10 max-w-[560px]">
        <div className="inline-block px-3 py-0.5 rounded-full text-[10px] font-bold tracking-widest text-quant-gold bg-quant-gold/10 border border-quant-gold/20 mb-4">
          AI-POWERED
        </div>
        <h2 className="text-2xl font-extrabold text-foreground mb-2 tracking-tight">AI 资产分析</h2>
        <p className="text-sm text-muted-foreground mb-8 leading-relaxed">选择标的并启动 AI 分析，获取实时交易建议与策略生成</p>

        <div className="flex gap-3 justify-center mb-8 flex-wrap">
          {[
            { icon: LineChart, title: '趋势分析', desc: '多时间框架技术研判' },
            { icon: Gauge, title: '风险评估', desc: '波动率与回撤测算' },
            { icon: Building2, title: '策略生成', desc: '自动输出交易计划' },
          ].map((f) => (
            <div
              key={f.title}
              className="flex items-center gap-2.5 px-3.5 py-3 bg-quant-card border border-quant-border rounded-xl shadow-sm flex-1 min-w-0 text-left transition-all hover:border-quant-gold/40 hover:shadow-md hover:-translate-y-0.5"
            >
              <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-quant-gold/10 text-quant-gold shrink-0">
                <f.icon className="w-4 h-4" />
              </div>
              <div className="min-w-0">
                <div className="text-xs font-bold text-foreground truncate">{f.title}</div>
                <div className="text-[10px] text-muted-foreground truncate">{f.desc}</div>
              </div>
            </div>
          ))}
        </div>

        <div className="flex gap-3 justify-center mb-4">
          <button
            onClick={onAddStock}
            className="inline-flex items-center gap-1.5 px-6 h-[42px] rounded-xl text-sm font-semibold bg-quant-gold text-white shadow-lg shadow-quant-gold/20 hover:opacity-90 transition-opacity"
          >
            <Plus className="w-4 h-4" /> 添加标的
          </button>
          <button
            onClick={onAnalyze}
            disabled={!canAnalyze}
            className="inline-flex items-center gap-1.5 px-6 h-[42px] rounded-xl text-sm font-semibold bg-quant-card border border-quant-border text-foreground hover:border-quant-gold/40 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <Zap className="w-4 h-4" /> 开始分析
          </button>
        </div>
        <p className="text-xs text-muted-foreground">从上方搜索框选择标的，或点击“添加标的”快速开始</p>
      </div>
    </div>
  )
}

function AnalysisResultView({
  result,
  loading,
  error,
  onRetry,
}: {
  result: AnalysisResult | null
  loading: boolean
  error: string | null
  onRetry: () => void
}) {
  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center h-full min-h-[300px] gap-4">
        <Loader2 className="w-8 h-8 text-quant-gold animate-spin" />
        <div className="text-sm text-muted-foreground">AI 正在分析中，请稍候...</div>
        <div className="w-48 h-1.5 bg-quant-border rounded-full overflow-hidden">
          <div className="h-full bg-quant-gold animate-progress rounded-full" />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center h-full min-h-[300px] gap-3">
        <div className="text-sm text-quant-red">{error}</div>
        <button
          onClick={onRetry}
          className="px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
        >
          重试
        </button>
      </div>
    )
  }

  if (!result) return null

  const consensusColor =
    result.consensus === 'bullish'
      ? 'text-quant-green bg-quant-green/10 border-quant-green/20'
      : result.consensus === 'bearish'
      ? 'text-quant-red bg-quant-red/10 border-quant-red/20'
      : 'text-quant-blue bg-quant-blue/10 border-quant-blue/20'

  const sentimentIcon = (s: string) =>
    s === 'bullish' ? (
      <TrendingUp className="h-3.5 w-3.5 text-quant-green" />
    ) : s === 'bearish' ? (
      <TrendingDown className="h-3.5 w-3.5 text-quant-red" />
    ) : (
      <Minus className="h-3.5 w-3.5 text-quant-blue" />
    )

  return (
    <div className="space-y-4">
      {/* Consensus header */}
      <div className="flex items-center gap-3">
        <div className={cn('px-3 py-1 rounded-lg text-xs font-bold border', consensusColor)}>
          {result.consensus === 'bullish' ? '看涨共识' : result.consensus === 'bearish' ? '看跌共识' : '中性共识'}
        </div>
        <div className="text-sm text-muted-foreground">
          标的 <span className="font-mono font-bold text-foreground">{result.symbol}</span>
        </div>
      </div>

      {/* Model cards */}
      <div className="space-y-3">
        {result.analyses.map((a) => (
          <div key={a.model} className="bg-quant-card border border-quant-border rounded-xl p-4">
            <div className="flex items-center justify-between mb-2">
              <div className="flex items-center gap-2">
                <BrainCircuit className="h-4 w-4 text-quant-gold" />
                <span className="text-sm font-semibold text-foreground">{a.name}</span>
              </div>
              <div className="flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[10px] font-medium
                {a.sentiment === 'bullish' ? 'border-quant-green/20 text-quant-green bg-quant-green/10' : a.sentiment === 'bearish' ? 'border-quant-red/20 text-quant-red bg-quant-red/10' : 'border-quant-blue/20 text-quant-blue bg-quant-blue/10'}">
                {sentimentIcon(a.sentiment)}
                {a.sentiment === 'bullish' ? '看涨' : a.sentiment === 'bearish' ? '看跌' : '中性'}
              </div>
            </div>
            <p className="text-xs text-muted-foreground leading-relaxed">{a.analysis}</p>
          </div>
        ))}
      </div>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/*  Main Page                                                          */
/* ------------------------------------------------------------------ */

export function AI() {
  /* -- AI Strategy states (legacy) -- */
  const [mode, setMode] = useState('natural')
  const [model, setModel] = useState('gpt-4o')
  const [voting, setVoting] = useState(false)
  const [confidence, setConfidence] = useState(78)
  const [messages, setMessages] = useState([
    { role: 'bot', content: '你好！描述你的交易思路，我来帮你生成量化策略。\n\n例如："做一个BTC的网格策略，震荡区间30000-40000"' },
  ])
  const [input, setInput] = useState('')

  /* -- Market data states -- */
  const [loadingMarket, setLoadingMarket] = useState(false)
  const [loadingSentiment, setLoadingSentiment] = useState(false)
  const [loadingIndices, setLoadingIndices] = useState(false)
  const [loadingHeatmap, setLoadingHeatmap] = useState(false)
  const [loadingCalendar, setLoadingCalendar] = useState(false)

  const [marketData, setMarketData] = useState<{
    fearGreed?: number
    vix?: number
    dxy?: number
    indices: MarketIndex[]
    heatmap: Record<HeatmapType, HeatmapItem[]>
    calendar: CalendarEvent[]
  }>({
    fearGreed: 52,
    vix: 18.5,
    dxy: 103.2,
    indices: [
      { flag: '🇺🇸', symbol: 'SPX', price: 5234.12, change: 0.45 },
      { flag: '🇺🇸', symbol: 'NDX', price: 18342.5, change: 0.82 },
      { flag: '🇺🇸', symbol: 'DJI', price: 39123.8, change: 0.12 },
      { flag: '🇨🇳', symbol: 'SH', price: 3052.3, change: -0.34 },
      { flag: '🇭🇰', symbol: 'HSI', price: 16782.4, change: 0.56 },
      { flag: '🇯🇵', symbol: 'N225', price: 39852.1, change: 1.12 },
      { flag: '🇬🇧', symbol: 'FTSE', price: 7934.2, change: -0.21 },
      { flag: '🇩🇪', symbol: 'DAX', price: 17892.3, change: 0.38 },
    ],
    heatmap: {
      us_stocks: [
        { name: 'AAPL', value: 1.24, price: 178.35 },
        { name: 'MSFT', value: 0.86, price: 412.2 },
        { name: 'NVDA', value: 3.45, price: 892.1 },
        { name: 'GOOGL', value: -0.34, price: 156.8 },
        { name: 'AMZN', value: 0.92, price: 178.9 },
        { name: 'META', value: 1.56, price: 498.2 },
        { name: 'TSLA', value: -2.12, price: 172.4 },
        { name: 'AMD', value: 2.34, price: 198.5 },
        { name: 'NFLX', value: -0.78, price: 612.3 },
        { name: 'CRM', value: 1.12, price: 298.4 },
        { name: 'INTC', value: -1.45, price: 42.3 },
        { name: 'BABA', value: -0.92, price: 78.5 },
      ],
      hk_stocks: [
        { name: '00700', name_cn: '腾讯', value: 1.12, price: 298.4 },
        { name: '09988', name_cn: '阿里', value: -0.82, price: 78.5 },
        { name: '03690', name_cn: '美团', value: 2.34, price: 112.3 },
        { name: '01810', name_cn: '小米', value: 0.56, price: 16.8 },
        { name: '09618', name_cn: '京东', value: -1.23, price: 112.5 },
        { name: '01299', name_cn: '友邦', value: 0.34, price: 56.2 },
        { name: '02318', name_cn: '平安', value: -0.45, price: 38.9 },
        { name: '00883', name_cn: '中海油', value: 1.78, price: 12.4 },
        { name: '00939', name_cn: '建行', value: 0.12, price: 4.56 },
        { name: '01398', name_cn: '工行', value: -0.23, price: 3.89 },
        { name: '02899', name_cn: '紫金', value: 2.12, price: 14.5 },
        { name: '09888', name_cn: '百度', value: -0.67, price: 98.4 },
      ],
      crypto: [
        { name: 'BTC', value: 2.34, price: 67234.5 },
        { name: 'ETH', value: 1.56, price: 3521.2 },
        { name: 'SOL', value: 4.12, price: 178.9 },
        { name: 'BNB', value: -0.34, price: 612.3 },
        { name: 'XRP', value: 0.78, price: 0.62 },
        { name: 'DOGE', value: 3.45, price: 0.18 },
        { name: 'ADA', value: -1.23, price: 0.48 },
        { name: 'AVAX', value: 2.89, price: 38.5 },
        { name: 'LINK', value: 1.12, price: 18.9 },
        { name: 'MATIC', value: -0.56, price: 0.78 },
        { name: 'DOT', value: 0.92, price: 7.85 },
        { name: 'LTC', value: -0.78, price: 82.4 },
      ],
      commodities: [
        { name: 'Gold', name_cn: '黄金', value: 0.45, price: 2345.6 },
        { name: 'Silver', name_cn: '白银', value: 1.23, price: 28.4 },
        { name: 'Crude Oil', name_cn: '原油', value: -0.78, price: 78.5 },
        { name: 'Brent', name_cn: '布油', value: -0.56, price: 82.3 },
        { name: 'Copper', name_cn: '铜', value: 1.89, price: 4.56 },
        { name: 'Natural Gas', name_cn: '天然气', value: -2.34, price: 2.12 },
        { name: 'Wheat', name_cn: '小麦', value: 0.34, price: 612.5 },
        { name: 'Corn', name_cn: '玉米', value: -0.12, price: 445.2 },
        { name: 'Coffee', name_cn: '咖啡', value: 2.12, price: 178.5 },
        { name: 'Sugar', name_cn: '糖', value: 1.45, price: 19.8 },
        { name: 'Cotton', name_cn: '棉花', value: -0.89, price: 78.4 },
        { name: 'Aluminum', name_cn: '铝', value: 0.67, price: 2456.8 },
      ],
      sectors: [
        { name: 'Tech', name_cn: '科技', value: 1.56 },
        { name: 'Finance', name_cn: '金融', value: 0.34 },
        { name: 'Health', name_cn: '医疗', value: -0.78 },
        { name: 'Energy', name_cn: '能源', value: -1.23 },
        { name: 'Consumer', name_cn: '消费', value: 0.89 },
        { name: 'Industry', name_cn: '工业', value: 0.12 },
        { name: 'Materials', name_cn: '材料', value: 1.12 },
        { name: 'Utilities', name_cn: '公用', value: -0.34 },
        { name: 'Real Estate', name_cn: '地产', value: -1.56 },
        { name: 'Telecom', name_cn: '电信', value: 0.45 },
        { name: 'Auto', name_cn: '汽车', value: 2.34 },
        { name: 'Media', name_cn: '传媒', value: -0.67 },
      ],
      forex: [
        { name: 'EUR/USD', value: 0.12, price: 1.0845 },
        { name: 'GBP/USD', value: -0.34, price: 1.2634 },
        { name: 'USD/JPY', value: 0.56, price: 151.23 },
        { name: 'USD/CHF', value: -0.12, price: 0.9034 },
        { name: 'AUD/USD', value: 0.78, price: 0.6543 },
        { name: 'USD/CAD', value: -0.45, price: 1.3567 },
        { name: 'NZD/USD', value: 0.23, price: 0.5987 },
        { name: 'EUR/GBP', value: 0.45, price: 0.8589 },
        { name: 'EUR/JPY', value: 0.67, price: 163.98 },
        { name: 'GBP/JPY', value: 0.34, price: 190.87 },
        { name: 'USD/CNH', value: -0.23, price: 7.2345 },
        { name: 'EUR/CHF', value: 0.12, price: 0.9789 },
      ],
    },
    calendar: [
      { id: '1', date: new Date().toISOString(), time: '08:30', country: 'US', name: '非农就业人口', importance: 'high', actual: 22.5, forecast: 20.0, actual_impact: 'bullish' },
      { id: '2', date: new Date().toISOString(), time: '10:00', country: 'CN', name: '制造业PMI', importance: 'medium', actual: 50.2, forecast: 49.8, actual_impact: 'neutral' },
      { id: '3', date: new Date().toISOString(), time: '14:00', country: 'UK', name: 'GDP月率', importance: 'medium', actual: 0.3, forecast: 0.2, actual_impact: 'bullish' },
      { id: '4', date: new Date().toISOString(), time: '20:30', country: 'US', name: 'CPI月率', importance: 'high', actual: 0.4, forecast: 0.3, actual_impact: 'bearish' },
      { id: '5', date: new Date().toISOString(), time: '22:00', country: 'US', name: '零售销售月率', importance: 'medium', actual: 0.6, forecast: 0.4, actual_impact: 'bullish' },
    ],
  })

  const [heatmapType, setHeatmapType] = useState<HeatmapType>('us_stocks')

  /* -- Watchlist states -- */
  const [watchlist, setWatchlist] = useState<WatchlistItem[]>([
    { market: 'USStock', symbol: 'AAPL', name: 'Apple Inc.', price: 178.35, change: 1.24 },
    { market: 'USStock', symbol: 'NVDA', name: 'NVIDIA Corp.', price: 892.1, change: 3.45 },
    { market: 'USStock', symbol: 'TSLA', name: 'Tesla Inc.', price: 172.4, change: -2.12 },
    { market: 'Crypto', symbol: 'BTC/USDT', name: 'Bitcoin', price: 67234.5, change: 2.34 },
    { market: 'Crypto', symbol: 'ETH/USDT', name: 'Ethereum', price: 3521.2, change: 1.56 },
  ])
  const [watchlistPrices, setWatchlistPrices] = useState<Record<string, WatchlistPrice>>({})
  const [positionSummaryMap, setPositionSummaryMap] = useState<Record<string, PositionSummary>>({})
  const [selectedSymbol, setSelectedSymbol] = useState<string | undefined>(undefined)
  const [showAddStockModal, setShowAddStockModal] = useState(false)

  /* -- Analysis states -- */
  const [analyzing, setAnalyzing] = useState(false)
  const [analysisResult, setAnalysisResult] = useState<AnalysisResult | null>(null)
  const [analysisError, setAnalysisError] = useState<string | null>(null)
  const [showHistoryModal, setShowHistoryModal] = useState(false)

  /* -- Derived -- */
  const currentHeatmap = useMemo(() => marketData.heatmap[heatmapType] || [], [marketData.heatmap, heatmapType])

  /* -- Actions -- */
  const loadMarketData = useCallback(async (force = false) => {
    setLoadingMarket(true)
    setLoadingSentiment(true)
    setLoadingIndices(true)
    setLoadingHeatmap(true)
    setLoadingCalendar(true)

    // Simulate progressive loading
    await new Promise((r) => setTimeout(r, 300))
    setLoadingSentiment(false)
    await new Promise((r) => setTimeout(r, 200))
    setLoadingIndices(false)
    await new Promise((r) => setTimeout(r, 300))
    setLoadingHeatmap(false)
    await new Promise((r) => setTimeout(r, 200))
    setLoadingCalendar(false)
    setLoadingMarket(false)
  }, [])

  useEffect(() => {
    loadMarketData()
  }, [loadMarketData])

  const handleSymbolChange = useCallback((value: string) => {
    setSelectedSymbol(value)
    setAnalysisResult(null)
    setAnalysisError(null)
  }, [])

  const startFastAnalysis = useCallback(async () => {
    if (!selectedSymbol) return
    setAnalyzing(true)
    setAnalysisError(null)
    setAnalysisResult(null)

    try {
      const symbol = selectedSymbol.split(':').pop() || selectedSymbol
      const data = await aiApi.analyze({ symbol })
      setAnalysisResult({
        symbol: data.symbol || symbol,
        consensus: data.consensus || 'neutral',
        analyses: (data.analyses || []).map((a: any) => ({
          model: a.model || '',
          name: a.name || a.model || '',
          sentiment: a.sentiment || 'neutral',
          analysis: a.analysis || a.content || '',
          content: a.content || '',
        })),
      })
    } catch (e: any) {
      setAnalysisError(e.message || '分析失败')
    } finally {
      setAnalyzing(false)
    }
  }, [selectedSymbol])

  const handleRetry = useCallback(() => {
    startFastAnalysis()
  }, [startFastAnalysis])

  const removeFromWatchlist = useCallback((stock: WatchlistItem) => {
    setWatchlist((prev) => prev.filter((s) => !(s.market === stock.market && s.symbol === stock.symbol)))
  }, [])

  const selectWatchlistItem = useCallback((stock: WatchlistItem) => {
    setSelectedSymbol(`${stock.market}:${stock.symbol}`)
    setAnalysisResult(null)
    setAnalysisError(null)
  }, [])

  /* -- ML states -- */
  const [mlMode, setMlMode] = useState(false)
  const [mlTab, setMlTab] = useState<'predict' | 'train' | 'models'>('predict')
  const [mlPredicting, setMlPredicting] = useState(false)
  const [mlTraining, setMlTraining] = useState(false)
  const [mlResult, setMlResult] = useState<{ direction: string; prediction: number; strength: number } | null>(null)
  const [mlError, setMlError] = useState('')
  const [mlModels, setMlModels] = useState<any[]>([])
  const [selectedMlModel, setSelectedMlModel] = useState('')
  // Train config
  const [mlModelType, setMlModelType] = useState('lightgbm')
  const [mlTaskType, setMlTaskType] = useState('regression')
  const [mlTrainBars, setMlTrainBars] = useState(500)
  const [mlHorizon, setMlHorizon] = useState(5)
  const [mlTrainResult, setMlTrainResult] = useState<any>(null)
  const [mlDeploying, setMlDeploying] = useState('')

  const deployMlStrategy = useCallback(async (modelId: string) => {
    setMlDeploying(modelId)
    try {
      const symbol = (selectedSymbol?.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol) || 'BTCUSDT'
      await mlApi.deploy({ model_id: modelId, symbol, min_confidence: 0.3 })
      alert(`ML 策略已部署: ${modelId} → ${symbol}`)
    } catch (e: any) { alert('部署失败: ' + (e?.message || e)) }
    finally { setMlDeploying('') }
  }, [selectedSymbol])

  const loadMlModels = useCallback(async () => {
    try { const data = await mlApi.list(); setMlModels(data?.models || []) } catch {}
  }, [])

  useEffect(() => { if (mlMode) loadMlModels() }, [mlMode, loadMlModels])

  const runMlPredict = useCallback(async () => {
    if (!selectedSymbol || !selectedMlModel) return
    setMlPredicting(true); setMlError(''); setMlResult(null)
    try {
      const symbol = selectedSymbol.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol
      const bars = await marketApi.klines(symbol, '1h', 200)
      const result = await mlApi.predict({ model_id: selectedMlModel, bars })
      setMlResult({ direction: result.direction, prediction: result.prediction, strength: result.strength })
    } catch (e: any) { setMlError(e?.message || 'ML 预测失败') }
    finally { setMlPredicting(false) }
  }, [selectedSymbol, selectedMlModel])

  const runMlTrain = useCallback(async () => {
    if (!selectedSymbol) return
    setMlTraining(true); setMlError(''); setMlTrainResult(null)
    try {
      const symbol = selectedSymbol.includes(':') ? selectedSymbol.split(':')[1] : selectedSymbol
      const bars = await marketApi.klines(symbol, '1h', mlTrainBars)
      const result = await mlApi.train({
        model_id: `${mlModelType}_${symbol}_${Date.now()}`,
        model_type: mlModelType, task_type: mlTaskType,
        symbol, interval: '1h', bars,
        label_config: { horizon: mlHorizon, label_type: mlTaskType },
      })
      setMlTrainResult(result)
      loadMlModels()
    } catch (e: any) { setMlError(e?.message || '训练失败') }
    finally { setMlTraining(false) }
  }, [selectedSymbol, mlModelType, mlTaskType, mlTrainBars, mlHorizon, loadMlModels])

  /* -- Render helpers -- */
  const canAnalyze = !!selectedSymbol && !analyzing

  return (
    <div className="h-full flex flex-col">
      {/* ===== Top Index Bar ===== */}
      <TopIndexBar
        marketData={marketData}
        loadingSentiment={loadingSentiment}
        loadingIndices={loadingIndices}
        onRefresh={() => loadMarketData(true)}
        loadingMarket={loadingMarket}
      />

      {/* ===== Main Body ===== */}
      <div className="flex-1 flex gap-3 p-3 min-h-0 overflow-hidden">
        {/* Left Panel: Heatmap + Calendar — hidden on mobile */}
        <div className="hidden md:flex w-[280px] shrink-0 flex-col gap-2.5 overflow-y-auto min-h-0">
          <HeatmapSection
            heatmapType={heatmapType}
            setHeatmapType={setHeatmapType}
            currentHeatmap={currentHeatmap}
            loadingHeatmap={loadingHeatmap}
            isDark={false}
          />
          <EconomicCalendar events={marketData.calendar} loadingCalendar={loadingCalendar} />
        </div>

        {/* Center Panel: Analysis */}
        <div className="flex-1 flex flex-col min-w-0 overflow-hidden bg-quant-card border border-quant-border rounded-xl shadow-sm">
          {/* Analysis Toolbar */}
          <div className="flex items-center gap-3 px-4 py-3 border-b border-quant-border bg-quant-bg-tertiary rounded-t-xl">
            <div className="relative flex-1 max-w-[320px]">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <select
                value={selectedSymbol || ''}
                onChange={(e) => handleSymbolChange(e.target.value)}
                className="w-full bg-quant-bg border border-quant-border rounded-lg pl-8 pr-3 py-2 text-xs focus:outline-none focus:border-quant-gold appearance-none"
              >
                <option value="">选择标的...</option>
                {watchlist.map((stock) => (
                  <option key={`${stock.market}:${stock.symbol}`} value={`${stock.market}:${stock.symbol}`}>
                    [{MARKET_NAMES[stock.market] || stock.market}] {stock.symbol} {stock.name ? `· ${stock.name}` : ''}
                  </option>
                ))}
              </select>
            </div>
            <button
              onClick={startFastAnalysis}
              disabled={!canAnalyze}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity disabled:opacity-40 disabled:cursor-not-allowed"
            >
              <Zap className="w-3.5 h-3.5" /> AI 分析
            </button>
            <button
              onClick={() => { setMlMode(!mlMode); setMlResult(null); setMlError('') }}
              className={cn(
                'inline-flex items-center gap-1.5 px-4 py-2 rounded-lg text-xs font-semibold transition-all',
                mlMode ? 'bg-quant-gold text-white' : 'bg-quant-card border border-quant-border text-foreground hover:border-quant-gold/40'
              )}
            >
              {mlMode ? <><Zap className="w-3.5 h-3.5" /> 返回 AI 分析</>
                      : <><BrainCircuit className="w-3.5 h-3.5" /> ML 预测</>}
            </button>
            <button
              onClick={() => setShowHistoryModal(true)}
              className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors"
            >
              <History className="w-3.5 h-3.5" /> 历史
            </button>
          </div>

          {/* Analysis Result Area */}
          <div className="flex-1 overflow-auto p-4 min-h-0">
            {mlMode ? (
              /* ── ML Panel ── */
              <div className="space-y-4">
                {/* Sub-tabs */}
                <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
                  {[
                    { k: 'predict' as const, label: '预测', icon: TrendingUp },
                    { k: 'train' as const, label: '训练', icon: Play },
                    { k: 'models' as const, label: `模型 (${mlModels.length})`, icon: Cpu },
                  ].map(t => (
                    <button key={t.k} onClick={() => setMlTab(t.k)}
                      className={cn('flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium transition-colors',
                        mlTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
                      <t.icon className="h-3 w-3" />{t.label}
                    </button>
                  ))}
                </div>

                {/* Error banner */}
                {mlError && (
                  <div className="flex items-start gap-2 text-xs text-red-400 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
                    <X className="h-3.5 w-3.5 mt-0.5 shrink-0" /> {mlError}
                  </div>
                )}

                {/* ── Predict Tab ── */}
                {mlTab === 'predict' && (
                  <div className="space-y-3">
                    {mlModels.length > 0 ? (
                      <select value={selectedMlModel} onChange={e => setSelectedMlModel(e.target.value)}
                        className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-xs outline-none focus:border-quant-gold">
                        {mlModels.map((m: any) => (
                          <option key={m.model_id} value={m.model_id}>{m.model_id} ({m.model_type} · {m.task_type})</option>
                        ))}
                      </select>
                    ) : (
                      <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
                        暂无已训练模型，请先到「训练」标签训练模型
                      </div>
                    )}
                    <button onClick={runMlPredict} disabled={mlPredicting || !selectedMlModel || !selectedSymbol}
                      className={cn('w-full flex items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium',
                        !selectedMlModel ? 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed' :
                        mlPredicting ? 'bg-quant-gold/50 cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
                      {mlPredicting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Zap className="h-4 w-4" />}
                      {mlPredicting ? '预测中...' : '生成预测'}
                    </button>
                    {mlResult && (
                      <div className={cn('flex items-center gap-3 p-4 rounded-xl border',
                        mlResult.direction === 'LONG' ? 'bg-quant-green/5 border-quant-green/20' : 'bg-quant-red/5 border-quant-red/20')}>
                        <div className={cn('w-10 h-10 rounded-full flex items-center justify-center',
                          mlResult.direction === 'LONG' ? 'bg-quant-green/10' : 'bg-quant-red/10')}>
                          {mlResult.direction === 'LONG'
                            ? <ArrowUp className="h-5 w-5 text-quant-green" />
                            : <ArrowDown className="h-5 w-5 text-quant-red" />}
                        </div>
                        <div>
                          <div className={cn('text-base font-bold', mlResult.direction === 'LONG' ? 'text-quant-green' : 'text-quant-red')}>
                            {mlResult.direction === 'LONG' ? '做多 LONG' : '做空 SHORT'}
                          </div>
                          <div className="text-[11px] text-muted-foreground">
                            预测值: {mlResult.prediction?.toFixed(6)} · 强度: {(mlResult.strength * 100).toFixed(0)}%
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                )}

                {/* ── Train Tab ── */}
                {mlTab === 'train' && (
                  <div className="space-y-3">
                    <div className="grid grid-cols-2 gap-2">
                      <div>
                        <label className="text-[10px] text-muted-foreground mb-0.5 block">模型</label>
                        <select value={mlModelType} onChange={e => setMlModelType(e.target.value)}
                          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                          <option value="lightgbm">LightGBM</option>
                          <option value="xgboost">XGBoost</option>
                        </select>
                      </div>
                      <div>
                        <label className="text-[10px] text-muted-foreground mb-0.5 block">任务</label>
                        <select value={mlTaskType} onChange={e => setMlTaskType(e.target.value)}
                          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                          <option value="regression">回归 (收益率)</option>
                          <option value="classification">分类 (涨跌)</option>
                        </select>
                      </div>
                      <div>
                        <label className="text-[10px] text-muted-foreground mb-0.5 block">K线数</label>
                        <input type="number" min={100} max={2000} value={mlTrainBars}
                          onChange={e => setMlTrainBars(Number(e.target.value))}
                          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                      </div>
                      <div>
                        <label className="text-[10px] text-muted-foreground mb-0.5 block">预测周期</label>
                        <input type="number" min={1} max={50} value={mlHorizon}
                          onChange={e => setMlHorizon(Number(e.target.value))}
                          className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                      </div>
                    </div>
                    <button onClick={runMlTrain} disabled={mlTraining || !selectedSymbol}
                      className={cn('w-full flex items-center justify-center gap-2 rounded-lg py-2.5 text-sm font-medium',
                        mlTraining ? 'bg-quant-gold/50 cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
                      {mlTraining ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                      {mlTraining ? '训练中...' : '开始训练'}
                    </button>
                    {mlTrainResult && (
                      <div className="text-xs space-y-1 p-3 bg-quant-green/5 border border-quant-green/20 rounded-lg">
                        <div className="flex items-center gap-1 text-quant-green font-medium">
                          <CheckCircle2 className="h-3 w-3" /> 训练完成
                        </div>
                        <div className="text-muted-foreground">
                          特征: {mlTrainResult.feature_count} · 训练样本: {mlTrainResult.train_samples} · 测试样本: {mlTrainResult.test_samples}
                        </div>
                        {mlTrainResult.metrics && Object.entries(mlTrainResult.metrics as Record<string,number>)
                          .filter(([k]) => k.startsWith('test_')).slice(0, 3)
                          .map(([k, v]) => (
                            <div key={k} className="text-muted-foreground">
                              {k.replace('test_', '')}: {typeof v === 'number' ? v.toFixed(4) : String(v)}
                            </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}

                {/* ── Models Tab ── */}
                {mlTab === 'models' && (
                  <div className="space-y-2">
                    {mlModels.length === 0 ? (
                      <div className="text-xs text-muted-foreground p-4 text-center">暂无模型，请先训练</div>
                    ) : mlModels.map((m: any) => (
                      <div key={m.model_id} className="flex items-center justify-between p-3 rounded-lg border border-quant-border bg-quant-bg-secondary">
                        <div className="min-w-0 flex-1">
                          <div className="text-xs font-medium truncate">{m.model_id}</div>
                          <div className="text-[10px] text-muted-foreground">
                            {m.model_type} · {m.task_type}
                            {m.metrics?.test_rmse != null && ` · RMSE: ${Number(m.metrics.test_rmse).toFixed(4)}`}
                            {m.metrics?.test_accuracy != null && ` · Acc: ${(Number(m.metrics.test_accuracy)*100).toFixed(1)}%`}
                          </div>
                        </div>
                        <button onClick={() => deployMlStrategy(m.model_id)}
                          disabled={mlDeploying === m.model_id}
                          className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-quant-gold/10 shrink-0 text-[10px]"
                          title="部署为交易策略">{mlDeploying === m.model_id ? <Loader2 className="h-3 w-3 animate-spin" /> : <Zap className="h-3 w-3" />}</button>
                        <button onClick={async () => { await mlApi.deleteModel(m.model_id); loadMlModels() }}
                          className="p-1 rounded text-muted-foreground hover:text-red-400 hover:bg-red-500/10 shrink-0">
                          <Trash2 className="h-3 w-3" />
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ) : !analysisResult && !analyzing && !analysisError ? (
              <AnalysisPlaceholder
                onAddStock={() => setShowAddStockModal(true)}
                onAnalyze={startFastAnalysis}
                canAnalyze={canAnalyze}
              />
            ) : (
              <AnalysisResultView
                result={analysisResult}
                loading={analyzing}
                error={analysisError}
                onRetry={handleRetry}
              />
            )}
          </div>
        </div>

        {/* Right Panel: Watchlist — hidden on mobile */}
        <div className="hidden md:flex w-[280px] shrink-0 bg-quant-card border border-quant-border rounded-xl shadow-sm flex-col overflow-hidden">
          <div className="flex items-center justify-between px-3.5 py-3 border-b border-quant-border bg-quant-bg-tertiary rounded-t-xl">
            <div className="flex items-center gap-1.5 text-xs font-bold text-foreground">
              <Star className="w-3.5 h-3.5 text-quant-gold fill-quant-gold" />
              自选股
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setShowAddStockModal(true)}
                className="p-1 rounded text-muted-foreground hover:text-quant-gold hover:bg-quant-gold/10 transition-colors"
              >
                <Plus className="w-3.5 h-3.5" />
              </button>
            </div>
          </div>

          <div className="flex-1 overflow-y-auto min-h-0 p-2 scrollbar-thin">
            {watchlist.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                <Star className="w-8 h-8 mx-auto mb-2 opacity-30" />
                <p className="text-xs mb-3">暂无自选股</p>
                <button
                  onClick={() => setShowAddStockModal(true)}
                  className="px-3 py-1.5 rounded-md bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity"
                >
                  添加标的
                </button>
              </div>
            ) : (
              watchlist.map((stock) => {
                const key = `${stock.market}:${stock.symbol}`
                const priceData = watchlistPrices[key]
                const pos = positionSummaryMap[key]
                const isActive = selectedSymbol === key
                const change = priceData?.change ?? stock.change ?? 0
                const price = priceData?.price ?? stock.price ?? 0

                return (
                  <div
                    key={key}
                    onClick={() => selectWatchlistItem(stock)}
                    className={cn(
                      'relative group rounded-lg p-2.5 mb-1 cursor-pointer transition-all border border-transparent',
                      isActive
                        ? 'bg-quant-gold/5 border-quant-gold/30 shadow-sm'
                        : 'hover:bg-quant-bg-secondary hover:border-quant-border'
                    )}
                  >
                    <div className="flex items-center justify-between">
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1.5">
                          <span className="text-xs font-bold text-foreground truncate">{stock.symbol}</span>
                          <span className="text-[9px] text-muted-foreground px-1 py-0.5 bg-quant-bg-secondary rounded">
                            {MARKET_NAMES[stock.market] || stock.market}
                          </span>
                        </div>
                        {stock.name && stock.name !== stock.symbol && (
                          <div className="text-[10px] text-muted-foreground truncate">{stock.name}</div>
                        )}
                      </div>
                      <div className="text-right shrink-0 ml-2">
                        <div className="text-xs font-mono font-semibold text-foreground">{formatPrice(price)}</div>
                        <div
                          className={cn(
                            'text-[10px] font-mono font-semibold inline-flex items-center gap-0.5 px-1 py-0.5 rounded',
                            change >= 0 ? 'text-quant-green bg-quant-green/10' : 'text-quant-red bg-quant-red/10'
                          )}
                        >
                          {change >= 0 ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                          {formatNum(change)}%
                        </div>
                      </div>
                    </div>

                    {/* Position row */}
                    {pos && pos.quantity > 0 && (
                      <div className="flex items-center justify-between mt-1.5 text-[10px] font-mono">
                        <span className="text-muted-foreground">
                          {formatNum(pos.quantity, 4)} @ {formatPrice(pos.avgEntry)}
                        </span>
                        <span className={cn(pos.pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                          {pos.pnl >= 0 ? '+' : ''}
                          {formatNum(pos.pnl)} ({pos.pnlPercent >= 0 ? '+' : ''}
                          {formatNum(pos.pnlPercent)}%)
                        </span>
                      </div>
                    )}

                    {/* Hover actions */}
                    <div className="absolute top-0 right-0 bottom-0 flex items-center gap-1 pr-2 opacity-0 group-hover:opacity-100 transition-opacity bg-gradient-to-l from-quant-bg via-quant-bg/80 to-transparent rounded-r-lg">
                      <button
                        onClick={(e) => { e.stopPropagation() }}
                        className="p-1 rounded bg-quant-card border border-quant-border text-muted-foreground hover:text-quant-gold transition-colors"
                        title="持仓"
                      >
                        <Wallet className="w-3 h-3" />
                      </button>
                      <button
                        onClick={(e) => { e.stopPropagation() }}
                        className="p-1 rounded bg-quant-card border border-quant-border text-muted-foreground hover:text-quant-gold transition-colors"
                        title="任务"
                      >
                        <Clock className="w-3 h-3" />
                      </button>
                      <button
                        onClick={(e) => { e.stopPropagation(); removeFromWatchlist(stock) }}
                        className="p-1 rounded bg-quant-card border border-quant-border text-muted-foreground hover:text-quant-red transition-colors"
                        title="删除"
                      >
                        <Trash2 className="w-3 h-3" />
                      </button>
                    </div>
                  </div>
                )
              })
            )}
          </div>
        </div>
      </div>

      {/* ===== Add Stock Modal ===== */}
      {showAddStockModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-quant-card border border-quant-border rounded-xl shadow-xl w-[500px] max-w-[90vw] max-h-[80vh] flex flex-col">
            <div className="flex items-center justify-between px-4 py-3 border-b border-quant-border">
              <h3 className="text-sm font-bold text-foreground">添加标的</h3>
              <button
                onClick={() => setShowAddStockModal(false)}
                className="p-1 rounded text-muted-foreground hover:text-foreground transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="p-4 flex-1 overflow-auto">
              <div className="text-xs text-muted-foreground mb-3">搜索并选择要添加的标的</div>
              <div className="flex gap-2 mb-4">
                <input
                  type="text"
                  placeholder="输入代码或名称..."
                  className="flex-1 bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
                />
                <button className="px-3 py-2 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity">
                  搜索
                </button>
              </div>
              <div className="text-xs font-semibold text-foreground mb-2">热门标的</div>
              <div className="space-y-1">
                {['AAPL', 'MSFT', 'NVDA', 'TSLA', 'BTC/USDT', 'ETH/USDT'].map((sym) => (
                  <div
                    key={sym}
                    className="flex items-center justify-between p-2 rounded-lg hover:bg-quant-bg-secondary cursor-pointer transition-colors"
                    onClick={() => {
                      setWatchlist((prev) => {
                        if (prev.some((s) => s.symbol === sym)) return prev
                        return [...prev, { market: sym.includes('/') ? 'Crypto' : 'USStock', symbol: sym }]
                      })
                      setShowAddStockModal(false)
                    }}
                  >
                    <span className="text-xs font-medium text-foreground">{sym}</span>
                    <Plus className="w-3.5 h-3.5 text-muted-foreground" />
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ===== History Modal ===== */}
      {showHistoryModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-quant-card border border-quant-border rounded-xl shadow-xl w-[700px] max-w-[95vw] max-h-[70vh] flex flex-col">
            <div className="flex items-center justify-between px-4 py-3 border-b border-quant-border">
              <h3 className="text-sm font-bold text-foreground">分析历史</h3>
              <button
                onClick={() => setShowHistoryModal(false)}
                className="p-1 rounded text-muted-foreground hover:text-foreground transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="p-4 flex-1 overflow-auto">
              <div className="text-center py-8 text-muted-foreground text-xs">暂无历史记录</div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
