import { useState, useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  BarChart3,
  CalendarDays,
  Clock,
  Flame,
  Gauge,
  Globe2,
  Loader2,
  Newspaper,
  Percent,
  Scale,
  Zap,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { dataProviderApi } from '@/lib/api'
import { useI18n } from '@/i18n'
import type {
  CalendarData,
  CoinglassData,
  FearGreedData,
  HeatmapData,
  MacroData,
  NewsData,
  ProviderResult,
  SourceHealth,
} from '@/types'

/* ── 通用小部件 ─────────────────────────────────────────────── */

function SectionCard({
  title,
  icon: Icon,
  children,
  className,
  actions,
}: {
  title: string
  icon: React.ComponentType<{ className?: string }>
  children: React.ReactNode
  className?: string
  actions?: React.ReactNode
}) {
  return (
    <div className={cn('bg-quant-card border border-quant-border rounded-xl shadow-sm flex flex-col min-h-0', className)}>
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-quant-border">
        <div className="flex items-center gap-1.5 text-xs font-bold text-foreground">
          <Icon className="w-3.5 h-3.5 text-quant-gold" />
          {title}
        </div>
        {actions}
      </div>
      <div className="flex-1 p-3 overflow-auto min-h-0">{children}</div>
    </div>
  )
}

/** 降级横幅：stale / not_configured / unavailable / refreshing */
function DegradedBanner({ result }: { result?: { status: string } }) {
  const { t } = useI18n()
  if (!result || result.status === 'ok') return null
  const key =
    result.status === 'not_configured'
      ? 'market.degraded.notConfigured'
      : result.status === 'stale'
        ? 'market.degraded.stale'
        : result.status === 'refreshing'
          ? 'market.degraded.refreshing'
          : 'market.degraded.unavailable'
  return (
    <div className="mb-2 text-[10px] px-2 py-1 rounded bg-quant-gold/10 text-quant-gold">
      {t(key)}
    </div>
  )
}

/* ── 恐惧贪婪仪表盘 ─────────────────────────────────────────── */

function fgBandKey(v: number): string {
  if (v <= 20) return 'market.fearGreed.extremeFear'
  if (v <= 40) return 'market.fearGreed.fear'
  if (v <= 60) return 'market.fearGreed.neutral'
  if (v <= 80) return 'market.fearGreed.greed'
  return 'market.fearGreed.extremeGreed'
}

function fgColor(v: number): string {
  if (v <= 20) return '#ef4444'
  if (v <= 40) return '#f97316'
  if (v <= 60) return '#eab308'
  if (v <= 80) return '#84cc16'
  return '#22c55e'
}

function FearGreedGauge({ result }: { result?: ProviderResult<FearGreedData> }) {
  const { t } = useI18n()
  const data = result?.data
  const value = data?.value ?? 50
  // 半圆仪表：0°(左) → 180°(右)
  const angle = (value / 100) * 180
  const rad = ((180 - angle) * Math.PI) / 180
  const cx = 100
  const cy = 90
  const r = 70
  const nx = cx + r * Math.cos(rad)
  const ny = cy - r * Math.sin(rad)

  return (
    <div className="flex flex-col items-center">
      <DegradedBanner result={result} />
      <svg viewBox="0 0 200 105" className="w-full max-w-[240px]">
        {/* 5 段色带 */}
        {[
          { from: 0, to: 20, color: '#ef4444' },
          { from: 20, to: 40, color: '#f97316' },
          { from: 40, to: 60, color: '#eab308' },
          { from: 60, to: 80, color: '#84cc16' },
          { from: 80, to: 100, color: '#22c55e' },
        ].map((seg) => {
          const a1 = ((180 - (seg.from / 100) * 180) * Math.PI) / 180
          const a2 = ((180 - (seg.to / 100) * 180) * Math.PI) / 180
          const x1 = cx + r * Math.cos(a1)
          const y1 = cy - r * Math.sin(a1)
          const x2 = cx + r * Math.cos(a2)
          const y2 = cy - r * Math.sin(a2)
          return (
            <path
              key={seg.from}
              d={`M ${x1} ${y1} A ${r} ${r} 0 0 1 ${x2} ${y2}`}
              stroke={seg.color}
              strokeWidth={12}
              fill="none"
              opacity={0.35}
            />
          )
        })}
        {/* 指针 */}
        <line x1={cx} y1={cy} x2={nx} y2={ny} stroke={fgColor(value)} strokeWidth={3} strokeLinecap="round" />
        <circle cx={cx} cy={cy} r={5} fill={fgColor(value)} />
        <text x={18} y={102} fontSize={9} fill="currentColor" opacity={0.5}>0</text>
        <text x={176} y={102} fontSize={9} fill="currentColor" opacity={0.5}>100</text>
      </svg>
      <div className="text-3xl font-bold" style={{ color: fgColor(value) }}>
        {data ? value : '--'}
      </div>
      <div className="text-xs text-muted-foreground mt-0.5">
        {data ? t(fgBandKey(value)) : t('market.degraded.unavailable')}
      </div>
      {/* 近 7 日 */}
      {data?.history && data.history.length > 1 && (
        <div className="w-full mt-3">
          <div className="text-[10px] text-muted-foreground mb-1">{t('market.fearGreed.history7d')}</div>
          <div className="flex items-end gap-1 h-8">
            {[...data.history].reverse().map((p, i) => (
              <div
                key={i}
                title={`${p.value}`}
                className="flex-1 rounded-sm"
                style={{ height: `${Math.max(8, p.value)}%`, backgroundColor: fgColor(p.value), opacity: 0.75 }}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

/* ── 衍生品情绪卡片 ─────────────────────────────────────────── */

function DerivativesPanel({ result }: { result?: ProviderResult<CoinglassData> }) {
  const { t } = useI18n()
  const data = result?.data
  const topFunding = useMemo(() => (data?.funding ?? []).slice(0, 6), [data])
  const ls = data?.long_short
  const liq = data?.liquidations

  return (
    <div className="space-y-3">
      <DegradedBanner result={result} />
      {!data && <div className="text-xs text-muted-foreground py-6 text-center">{t('market.degraded.notConfigured')}</div>}
      {data && (
        <>
          {/* 多空比 */}
          {ls && (
            <div>
              <div className="flex items-center justify-between text-[10px] text-muted-foreground mb-1">
                <span className="flex items-center gap-1">
                  <Scale className="w-3 h-3" />
                  {t('market.derivatives.longShort')} (BTC)
                </span>
                <span>{ls.ratio.toFixed(2)}</span>
              </div>
              <div className="flex h-2.5 rounded-full overflow-hidden">
                <div className="bg-quant-green" style={{ width: `${ls.long_pct}%` }} />
                <div className="bg-quant-red" style={{ width: `${ls.short_pct}%` }} />
              </div>
              <div className="flex justify-between text-[10px] mt-0.5">
                <span className="text-quant-green">
                  {t('market.derivatives.long')} {ls.long_pct.toFixed(1)}%
                </span>
                <span className="text-quant-red">
                  {t('market.derivatives.short')} {ls.short_pct.toFixed(1)}%
                </span>
              </div>
            </div>
          )}
          {/* 爆仓 */}
          {liq && (
            <div className="grid grid-cols-3 gap-2">
              <div className="bg-quant-bg-secondary rounded-lg p-2 text-center">
                <div className="text-[9px] text-muted-foreground">{t('market.derivatives.liquidations')}</div>
                <div className="text-xs font-bold text-foreground">{fmtUSD(liq.total_usd)}</div>
              </div>
              <div className="bg-quant-bg-secondary rounded-lg p-2 text-center">
                <div className="text-[9px] text-quant-green">{t('market.derivatives.long')}</div>
                <div className="text-xs font-bold text-quant-green">{fmtUSD(liq.long_usd)}</div>
              </div>
              <div className="bg-quant-bg-secondary rounded-lg p-2 text-center">
                <div className="text-[9px] text-quant-red">{t('market.derivatives.short')}</div>
                <div className="text-xs font-bold text-quant-red">{fmtUSD(liq.short_usd)}</div>
              </div>
            </div>
          )}
          {/* 资金费率 */}
          {topFunding.length > 0 && (
            <div>
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground mb-1">
                <Percent className="w-3 h-3" />
                {t('market.derivatives.funding')} (BTC)
              </div>
              <div className="space-y-1">
                {topFunding.map((f) => (
                  <div key={f.exchange} className="flex items-center justify-between text-xs">
                    <span className="text-muted-foreground">{f.exchange}</span>
                    <span className={cn('font-mono font-medium', f.rate_pct >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                      {f.rate_pct >= 0 ? '+' : ''}
                      {f.rate_pct.toFixed(4)}%
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

/* ── 热力图 ─────────────────────────────────────────────────── */

function heatColor(changePct: number): string {
  // -5% .. +5% 映射红→灰→绿
  const capped = Math.max(-5, Math.min(5, changePct))
  if (Math.abs(capped) < 0.05) return 'rgba(120,120,120,0.25)'
  const intensity = 0.25 + (Math.abs(capped) / 5) * 0.65
  return capped > 0 ? `rgba(34,197,94,${intensity})` : `rgba(239,68,68,${intensity})`
}

function HeatmapGrid({ result }: { result?: ProviderResult<HeatmapData> }) {
  const { t } = useI18n()
  const entries = result?.data?.entries ?? []
  return (
    <div className="h-full flex flex-col">
      <DegradedBanner result={result} />
      {entries.length === 0 && <div className="text-xs text-muted-foreground py-10 text-center">{t('market.degraded.unavailable')}</div>}
      {entries.length > 0 && (
        <div className="flex flex-wrap gap-1 flex-1 content-start">
          {entries.map((e) => (
            <div
              key={e.symbol}
              title={`${e.symbol} ${e.change_pct_24h >= 0 ? '+' : ''}${e.change_pct_24h.toFixed(2)}% · ${t('market.heatmap.volume')} ${fmtUSD(e.volume_24h)}`}
              className="rounded-md p-1.5 flex flex-col items-center justify-center text-center cursor-default transition-transform hover:scale-[1.03]"
              style={{
                backgroundColor: heatColor(e.change_pct_24h),
                flexGrow: Math.max(1, Math.round(e.weight * 12)),
                flexBasis: `${70 + e.weight * 90}px`,
                minHeight: `${44 + e.weight * 34}px`,
              }}
            >
              <div className="text-[11px] font-bold text-white drop-shadow-sm">{e.base}</div>
              <div className="text-[10px] font-medium text-white/90">
                {e.change_pct_24h >= 0 ? '+' : ''}
                {e.change_pct_24h.toFixed(2)}%
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

/* ── 新闻流 ─────────────────────────────────────────────────── */

function NewsFeed({ result, symbol, onSymbolChange }: { result?: ProviderResult<NewsData>; symbol: string; onSymbolChange: (s: string) => void }) {
  const { t } = useI18n()
  const items = result?.data?.items ?? []
  return (
    <div className="h-full flex flex-col">
      <div className="mb-2">
        <input
          value={symbol}
          onChange={(e) => onSymbolChange(e.target.value)}
          placeholder={t('market.news.filterPlaceholder')}
          className="w-full bg-quant-bg border border-quant-border rounded-lg px-2.5 py-1.5 text-xs focus:outline-none focus:border-quant-gold"
        />
      </div>
      <DegradedBanner result={result} />
      {items.length === 0 && <div className="text-xs text-muted-foreground py-10 text-center">{t('market.news.empty')}</div>}
      <div className="space-y-2">
        {items.map((n) => (
          <a
            key={n.id}
            href={n.url}
            target="_blank"
            rel="noreferrer"
            className="block bg-quant-bg-secondary hover:border-quant-gold/40 border border-transparent rounded-lg p-2.5 transition-colors"
          >
            <div className="text-xs font-medium text-foreground leading-snug line-clamp-2">{n.title}</div>
            {n.summary && <div className="text-[10px] text-muted-foreground mt-1 line-clamp-2">{n.summary}</div>}
            <div className="flex items-center gap-2 mt-1.5 text-[9px] text-muted-foreground">
              <span className="font-medium text-quant-gold">{n.source}</span>
              <span>{timeAgo(n.published_at)}</span>
              {n.categories.slice(0, 3).map((c) => (
                <span key={c} className="px-1 py-px rounded bg-quant-bg border border-quant-border">{c}</span>
              ))}
            </div>
          </a>
        ))}
      </div>
    </div>
  )
}

/* ── 宏观序列表 ─────────────────────────────────────────────── */

function MacroTable({ result }: { result?: ProviderResult<MacroData> }) {
  const { t } = useI18n()
  const series = result?.data?.series ?? []
  return (
    <div>
      <DegradedBanner result={result} />
      {series.length === 0 && (
        <div className="text-xs text-muted-foreground py-8 text-center">
          {result?.status === 'not_configured' ? t('market.macro.notConfigured') : t('market.degraded.unavailable')}
        </div>
      )}
      {series.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-[10px] text-muted-foreground border-b border-quant-border">
                <th className="py-1.5 pr-3 font-medium">{t('market.calendar.event')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('market.macro.latest')}</th>
                <th className="py-1.5 pr-3 font-medium">{t('market.macro.date')}</th>
                <th className="py-1.5 font-medium">Trend</th>
              </tr>
            </thead>
            <tbody>
              {series.map((s) => {
                const obs = s.observations
                const last = obs[obs.length - 1]
                const prev = obs.length > 1 ? obs[obs.length - 2] : undefined
                const delta = last && prev ? last.value - prev.value : 0
                return (
                  <tr key={s.id} className="border-b border-quant-border/50 last:border-0">
                    <td className="py-2 pr-3">
                      <div className="font-medium text-foreground">{s.name}</div>
                      <div className="text-[9px] text-muted-foreground">{s.id} · {s.name_en}</div>
                    </td>
                    <td className="py-2 pr-3 font-mono font-bold text-foreground">
                      {last ? `${last.value.toLocaleString(undefined, { maximumFractionDigits: 2 })}${s.unit === '%' ? '%' : ''}` : '--'}
                    </td>
                    <td className="py-2 pr-3 text-muted-foreground">{last?.date ?? '--'}</td>
                    <td className="py-2">
                      <span className={cn('font-mono text-[10px]', delta > 0 ? 'text-quant-green' : delta < 0 ? 'text-quant-red' : 'text-muted-foreground')}>
                        {delta > 0 ? '▲' : delta < 0 ? '▼' : '—'} {delta !== 0 ? Math.abs(delta).toFixed(2) : ''}
                      </span>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

/* ── 经济日历 ───────────────────────────────────────────────── */

function CalendarTable({ result }: { result?: ProviderResult<CalendarData> }) {
  const { t } = useI18n()
  const events = useMemo(() => {
    const all = result?.data?.events ?? []
    // 高/中重要性优先，按日期+时间排序
    const rank = (i: string) => (i === 'high' ? 0 : i === 'medium' ? 1 : i === 'low' ? 2 : 3)
    return [...all].sort((a, b) => rank(a.impact) - rank(b.impact) || a.date.localeCompare(b.date))
  }, [result])

  return (
    <div>
      <DegradedBanner result={result} />
      {events.length === 0 && <div className="text-xs text-muted-foreground py-10 text-center">{t('market.calendar.empty')}</div>}
      {events.length > 0 && (
        <table className="w-full text-xs">
          <thead>
            <tr className="text-left text-[10px] text-muted-foreground border-b border-quant-border">
              <th className="py-1.5 pr-2 font-medium">{t('market.calendar.time')}</th>
              <th className="py-1.5 pr-2 font-medium">{t('market.calendar.event')}</th>
              <th className="py-1.5 pr-2 font-medium">{t('market.calendar.impact')}</th>
              <th className="py-1.5 pr-2 font-medium text-right">{t('market.calendar.forecast')}</th>
              <th className="py-1.5 font-medium text-right">{t('market.calendar.previous')}</th>
            </tr>
          </thead>
          <tbody>
            {events.map((e) => (
              <tr key={e.id} className="border-b border-quant-border/50 last:border-0">
                <td className="py-2 pr-2 whitespace-nowrap text-muted-foreground">
                  <div>{e.date.slice(5)}</div>
                  <div className="text-[9px]">{e.time}</div>
                </td>
                <td className="py-2 pr-2">
                  <span className="font-medium text-foreground">{e.name}</span>
                  <span className="ml-1 text-[9px] px-1 rounded bg-quant-bg-secondary text-muted-foreground">{e.currency}</span>
                </td>
                <td className="py-2 pr-2">
                  <ImpactBadge impact={e.impact} />
                </td>
                <td className="py-2 pr-2 text-right font-mono text-muted-foreground">{e.forecast || '—'}</td>
                <td className="py-2 text-right font-mono text-muted-foreground">{e.previous || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

function ImpactBadge({ impact }: { impact: string }) {
  const { t } = useI18n()
  const map: Record<string, { key: string; cls: string }> = {
    high: { key: 'market.calendar.impactHigh', cls: 'bg-quant-red/10 text-quant-red' },
    medium: { key: 'market.calendar.impactMedium', cls: 'bg-quant-gold/10 text-quant-gold' },
    low: { key: 'market.calendar.impactLow', cls: 'bg-quant-green/10 text-quant-green' },
    holiday: { key: 'market.calendar.impactHoliday', cls: 'bg-quant-blue/10 text-quant-blue' },
  }
  const conf = map[impact] ?? map.low
  return <span className={cn('text-[9px] px-1.5 py-0.5 rounded font-medium', conf.cls)}>{t(conf.key)}</span>
}

/* ── 源健康 chips ───────────────────────────────────────────── */

function SourceChips({ sources }: { sources: SourceHealth[] }) {
  const { t } = useI18n()
  const stateConf: Record<string, { key: string; cls: string }> = {
    ok: { key: 'market.sources.ok', cls: 'text-quant-green border-quant-green/30 bg-quant-green/5' },
    stale: { key: 'market.sources.stale', cls: 'text-quant-gold border-quant-gold/30 bg-quant-gold/5' },
    no_data: { key: 'market.sources.noData', cls: 'text-muted-foreground border-quant-border bg-quant-bg-secondary' },
    not_configured: { key: 'market.sources.notConfigured', cls: 'text-muted-foreground border-dashed border-quant-border bg-transparent' },
  }
  return (
    <div className="flex flex-wrap gap-1.5">
      {sources.map((s) => {
        const conf = stateConf[s.state] ?? stateConf.no_data
        return (
          <div
            key={s.name}
            title={`${s.description}${s.last_error ? ` · ${s.last_error}` : ''} · ${t('market.sources.circuit')}: ${s.circuit}`}
            className={cn('inline-flex items-center gap-1 text-[10px] px-2 py-1 rounded-lg border', conf.cls)}
          >
            <span className={cn('w-1.5 h-1.5 rounded-full', s.state === 'ok' ? 'bg-quant-green' : s.state === 'stale' ? 'bg-quant-gold' : 'bg-muted-foreground/50')} />
            <span className="font-medium">{s.name}</span>
            <span className="opacity-80">{t(conf.key)}</span>
            {s.requires_key && !s.configured && <span className="opacity-60">· {t('market.sources.requiresKey')}</span>}
            {s.circuit === 'open' && <span className="text-quant-red">· {t('market.sources.circuitOpen')}</span>}
          </div>
        )
      })}
    </div>
  )
}

/* ── 工具 ───────────────────────────────────────────────────── */

function fmtUSD(v: number): string {
  if (v >= 1e9) return `$${(v / 1e9).toFixed(2)}B`
  if (v >= 1e6) return `$${(v / 1e6).toFixed(2)}M`
  if (v >= 1e3) return `$${(v / 1e3).toFixed(1)}K`
  return `$${v.toFixed(0)}`
}

function timeAgo(unixSec: number): string {
  const diff = Math.max(0, Date.now() / 1000 - unixSec)
  if (diff < 3600) return `${Math.floor(diff / 60)}m`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`
  return `${Math.floor(diff / 86400)}d`
}

/* ── 主页面 ─────────────────────────────────────────────────── */

export function MarketData() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [newsSymbol, setNewsSymbol] = useState('')

  const { data: sentiment, isLoading: sentimentLoading } = useQuery({
    queryKey: ['dp-sentiment'],
    queryFn: () => dataProviderApi.sentiment(),
    staleTime: 60_000,
    refetchInterval: 120_000,
    retry: 1,
  })
  const { data: heatmap } = useQuery({
    queryKey: ['dp-heatmap'],
    queryFn: () => dataProviderApi.heatmap(),
    staleTime: 30_000,
    refetchInterval: 60_000,
    retry: 1,
  })
  const { data: news } = useQuery({
    queryKey: ['dp-news', newsSymbol],
    queryFn: () => dataProviderApi.news(newsSymbol || undefined),
    staleTime: 60_000,
    refetchInterval: 180_000,
    retry: 1,
  })
  const { data: macro } = useQuery({
    queryKey: ['dp-macro'],
    queryFn: () => dataProviderApi.macro(),
    staleTime: 300_000,
    refetchInterval: 600_000,
    retry: 1,
  })
  const { data: calendar } = useQuery({
    queryKey: ['dp-calendar'],
    queryFn: () => dataProviderApi.calendar(),
    staleTime: 300_000,
    refetchInterval: 600_000,
    retry: 1,
  })
  const { data: sources } = useQuery({
    queryKey: ['dp-sources'],
    queryFn: () => dataProviderApi.sources(),
    staleTime: 30_000,
    refetchInterval: 60_000,
    retry: 1,
  })

  const loading = sentimentLoading
  const refreshAll = () => queryClient.invalidateQueries({ queryKey: ['dp-'] })

  return (
    <div className="h-full flex flex-col p-4 gap-3 overflow-auto">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <Globe2 className="w-5 h-5 text-quant-gold" />
          <h1 className="text-lg font-bold text-foreground">{t('market.title')}</h1>
          <span className="text-[10px] text-muted-foreground hidden sm:inline">{t('market.subtitle')}</span>
        </div>
        <button
          onClick={refreshAll}
          disabled={loading}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-quant-card border border-quant-border text-foreground text-xs font-medium hover:border-quant-gold/40 transition-colors disabled:opacity-50"
        >
          {loading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Clock className="w-3.5 h-3.5" />}
          {t('market.refresh')}
        </button>
      </div>

      {/* 源健康 */}
      {sources && sources.length > 0 && <SourceChips sources={sources} />}

      {/* 情绪行 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <SectionCard title={t('market.fearGreed.title')} icon={Gauge}>
          <FearGreedGauge result={sentiment?.fear_greed} />
        </SectionCard>
        <SectionCard title={t('market.derivatives.title')} icon={Flame}>
          <DerivativesPanel result={sentiment?.derivatives} />
        </SectionCard>
      </div>

      {/* 热力图 */}
      <SectionCard title={t('market.heatmap.title')} icon={BarChart3} className="min-h-[220px]">
        <HeatmapGrid result={heatmap} />
      </SectionCard>

      {/* 新闻 + 日历 */}
      <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
        <SectionCard title={t('market.news.title')} icon={Newspaper} className="max-h-[480px]">
          <NewsFeed result={news} symbol={newsSymbol} onSymbolChange={setNewsSymbol} />
        </SectionCard>
        <SectionCard title={t('market.calendar.title')} icon={CalendarDays} className="max-h-[480px]">
          <CalendarTable result={calendar} />
        </SectionCard>
      </div>

      {/* 宏观序列 */}
      <SectionCard title={t('market.macro.title')} icon={Activity} actions={<Zap className="w-3 h-3 text-muted-foreground" />}>
        <MacroTable result={macro} />
      </SectionCard>
    </div>
  )
}
