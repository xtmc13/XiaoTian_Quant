import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import { strategyCommunityApi } from '@/lib/api'
import { OverfitRiskGauge } from '@/components/community/OverfitRiskGauge'
import type { StrategyCommunityItem } from '@/types'
import {
  Trophy,
  TrendingUp,
  BarChart3,
  Star,
  Download,
  MessageSquare,
  Crown,
  Medal,
  ChevronRight,
} from 'lucide-react'

type SortKey = 'kpi' | 'return' | 'sharpe' | 'popular'

const SORT_TABS: { key: SortKey; label: string; icon: typeof Trophy }[] = [
  { key: 'kpi', label: 'KPI综合', icon: Trophy },
  { key: 'return', label: '收益率', icon: TrendingUp },
  { key: 'sharpe', label: '夏普比率', icon: BarChart3 },
  { key: 'popular', label: '人气', icon: Star },
]

function formatPct(v?: number) {
  if (v == null || Number.isNaN(v)) return '—'
  const sign = v > 0 ? '+' : ''
  return `${sign}${v.toFixed(2)}%`
}

function formatNum(v?: number, digits = 2) {
  return v != null && !Number.isNaN(v) ? v.toFixed(digits) : '—'
}

/* ── 前三名奖牌样式：金/银/铜 ── */
function getPodiumMeta(rank: number) {
  if (rank === 1) {
    return {
      order: 'md:order-2',
      border: 'border-quant-gold/50',
      medal: 'bg-quant-gold text-black',
      Icon: Crown,
    }
  }
  if (rank === 2) {
    return {
      order: 'md:order-1',
      border: 'border-gray-400/30',
      medal: 'bg-gray-300 text-black',
      Icon: Medal,
    }
  }
  return {
    order: 'md:order-3',
    border: 'border-amber-700/40',
    medal: 'bg-amber-700 text-foreground',
    Icon: Medal,
  }
}

/* ── 前三名领奖台卡片 ── */
function PodiumCard({ item, rank }: { item: StrategyCommunityItem; rank: number }) {
  const navigate = useNavigate()
  const meta = getPodiumMeta(rank)
  const kpi = item.kpi_score
  const ret = item.total_return

  return (
    <div
      onClick={() => navigate(`/indicator-community/${item.id}`)}
      className={cn(
        'flex flex-col items-center gap-2.5 rounded-xl border bg-quant-card p-4 text-center cursor-pointer transition-all hover:bg-quant-hover',
        meta.order,
        meta.border,
        rank === 1 ? 'md:-translate-y-2 shadow-[0_0_24px_rgba(54,153,255,0.12)] hover:border-quant-gold/70' : 'hover:border-quant-gold/30'
      )}
    >
      <div className={cn('flex h-9 w-9 items-center justify-center rounded-full', meta.medal)}>
        <meta.Icon className="h-4 w-4" />
      </div>

      <div className="w-full min-w-0">
        <h3 className="text-sm font-bold text-foreground truncate">{item.name}</h3>
        <p className="text-[11px] text-muted-foreground truncate">by {item.author_name || item.author}</p>
      </div>

      {kpi && (
        <div>
          <div className="text-xl font-bold font-mono text-quant-gold">{kpi.total_score.toFixed(1)}</div>
          <div className="text-[10px] text-muted-foreground">KPI评分</div>
        </div>
      )}

      <div className="grid w-full grid-cols-3 gap-2">
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className={cn(
            'text-xs font-semibold font-mono',
            ret == null ? 'text-muted-foreground' : ret >= 0 ? 'text-quant-green' : 'text-quant-red'
          )}>
            {formatPct(ret)}
          </div>
          <div className="text-[9px] text-muted-foreground">收益</div>
        </div>
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className="text-xs font-semibold font-mono text-foreground">{formatNum(item.sharpe_ratio)}</div>
          <div className="text-[9px] text-muted-foreground">夏普</div>
        </div>
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className="text-xs font-semibold font-mono text-foreground">
            {item.win_rate != null ? `${item.win_rate.toFixed(1)}%` : '—'}
          </div>
          <div className="text-[9px] text-muted-foreground">胜率</div>
        </div>
      </div>

      <OverfitRiskGauge result={item.overfit_risk} size="sm" showLabel={true} />
    </div>
  )
}

/* ── 榜单行（第 4 名起） ── */
function RankRow({ item, rank }: { item: StrategyCommunityItem; rank: number }) {
  const navigate = useNavigate()
  const kpi = item.kpi_score
  const ret = item.total_return
  const sharpe = item.sharpe_ratio
  const winRate = item.win_rate

  return (
    <div
      onClick={() => navigate(`/indicator-community/${item.id}`)}
      className="flex items-center gap-3 px-4 py-3 cursor-pointer transition-colors hover:bg-quant-hover"
    >
      <span className="w-6 shrink-0 text-center text-xs font-bold font-mono text-muted-foreground">
        {rank}
      </span>

      <div className="min-w-0 flex-1">
        <div className="text-xs font-semibold text-foreground truncate">{item.name}</div>
        <div className="mt-0.5 flex items-center gap-2 text-[10px] text-muted-foreground">
          <span className="truncate">by {item.author_name || item.author}</span>
          <span className="flex items-center gap-1 shrink-0">
            <Download className="h-2.5 w-2.5" /> {item.download_count || 0}
          </span>
          <span className="flex items-center gap-1 shrink-0">
            <Star className="h-2.5 w-2.5" /> {item.rating_count || 0}
          </span>
          <span className="flex items-center gap-1 shrink-0">
            <MessageSquare className="h-2.5 w-2.5" /> {item.comment_count || 0}
          </span>
        </div>
      </div>

      {/* 收益 */}
      <div className="hidden sm:block w-20 shrink-0 text-right">
        <div className={cn(
          'text-xs font-semibold font-mono',
          ret == null ? 'text-muted-foreground' : ret >= 0 ? 'text-quant-green' : 'text-quant-red'
        )}>
          {formatPct(ret)}
        </div>
        <div className="text-[9px] text-muted-foreground">收益</div>
      </div>

      {/* 夏普 */}
      <div className="hidden md:block w-14 shrink-0 text-right">
        <div className={cn(
          'text-xs font-semibold font-mono',
          sharpe == null ? 'text-muted-foreground' : sharpe >= 1 ? 'text-quant-green' : sharpe < 0 ? 'text-quant-red' : 'text-foreground'
        )}>
          {formatNum(sharpe)}
        </div>
        <div className="text-[9px] text-muted-foreground">夏普</div>
      </div>

      {/* 回撤（风险指标，恒为跌色） */}
      <div className="hidden md:block w-16 shrink-0 text-right">
        <div className="text-xs font-semibold font-mono text-quant-red">{formatPct(item.max_drawdown)}</div>
        <div className="text-[9px] text-muted-foreground">回撤</div>
      </div>

      {/* 胜率 */}
      <div className="hidden lg:block w-14 shrink-0 text-right">
        <div className={cn(
          'text-xs font-semibold font-mono',
          winRate == null ? 'text-muted-foreground' : winRate >= 50 ? 'text-quant-green' : 'text-foreground'
        )}>
          {winRate != null ? `${winRate.toFixed(1)}%` : '—'}
        </div>
        <div className="text-[9px] text-muted-foreground">胜率</div>
      </div>

      {/* KPI */}
      <div className="hidden sm:block w-14 shrink-0 text-right">
        <div className="text-xs font-bold font-mono text-quant-gold">
          {kpi ? kpi.total_score.toFixed(1) : '—'}
        </div>
        <div className="text-[9px] text-muted-foreground">KPI</div>
      </div>

      {/* 过拟合风险 */}
      <div className="hidden xl:flex shrink-0 w-24 justify-end">
        <OverfitRiskGauge result={item.overfit_risk} size="sm" showLabel={false} />
      </div>

      <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
    </div>
  )
}

/* ── 加载骨架：领奖台 + 榜单行 ── */
function LeaderboardSkeleton() {
  return (
    <>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} variant="card" className="h-52 rounded-xl" />
        ))}
      </div>
      <SectionCard title="完整榜单" noPadding>
        <div className="divide-y divide-quant-border/50">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="flex items-center gap-3 px-4 py-3">
              <Skeleton variant="circle" className="h-5 w-5" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3 w-1/3" />
                <Skeleton className="h-2.5 w-1/4" />
              </div>
              <Skeleton className="h-3 w-14 hidden sm:block" />
              <Skeleton className="h-3 w-14 hidden md:block" />
              <Skeleton className="h-3 w-14 hidden lg:block" />
            </div>
          ))}
        </div>
      </SectionCard>
    </>
  )
}

export function StrategyLeaderboard() {
  const [sortBy, setSortBy] = useState<SortKey>('kpi')
  const [items, setItems] = useState<StrategyCommunityItem[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    strategyCommunityApi.leaderboard(sortBy, 20)
      .then((res) => {
        const list = Array.isArray(res) ? res : []
        setItems(list)
      })
      .catch(() => setItems([]))
      .finally(() => setLoading(false))
  }, [sortBy])

  const podiumItems = items.slice(0, 3)
  const restItems = items.slice(3)
  const activeTab = SORT_TABS.find((t) => t.key === sortBy)

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="space-y-5 max-w-5xl mx-auto">
        <PageHeader
          icon={<Trophy className="w-5 h-5" />}
          title="策略排行榜"
          subtitle="基于KPI综合评分、收益率、夏普比率和人气的策略排名"
        />

        {/* 榜单维度切换 */}
        <div className="flex items-center gap-2 flex-wrap">
          {SORT_TABS.map((tab) => {
            const Icon = tab.icon
            return (
              <button
                key={tab.key}
                onClick={() => setSortBy(tab.key)}
                className={cn(
                  'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors',
                  sortBy === tab.key
                    ? 'bg-quant-gold/10 text-quant-gold border-quant-gold/30'
                    : 'border-quant-border text-muted-foreground hover:text-foreground hover:border-quant-gold/20'
                )}
              >
                <Icon className="h-3.5 w-3.5" />
                {tab.label}
              </button>
            )
          })}
          {!loading && items.length > 0 && (
            <span className="ml-auto text-[11px] text-muted-foreground">
              共 {items.length} 个策略
            </span>
          )}
        </div>

        {loading ? (
          <LeaderboardSkeleton />
        ) : items.length === 0 ? (
          <SectionCard>
            <EmptyState
              icon={<Trophy className="w-8 h-8" />}
              title="暂无策略数据"
              description="策略上榜后将在此展示排名与指标表现"
            />
          </SectionCard>
        ) : (
          <>
            {/* 前三名领奖台 */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              {podiumItems.map((item, idx) => (
                <PodiumCard key={item.id} item={item} rank={idx + 1} />
              ))}
            </div>

            {/* 完整榜单 */}
            {restItems.length > 0 && (
              <SectionCard
                title="完整榜单"
                headerAction={
                  <span className="text-xs text-muted-foreground">
                    {activeTab?.label} · 第 4 - {items.length} 名
                  </span>
                }
                noPadding
              >
                <div className="divide-y divide-quant-border/50">
                  {restItems.map((item, idx) => (
                    <RankRow key={item.id} item={item} rank={idx + 4} />
                  ))}
                </div>
              </SectionCard>
            )}
          </>
        )}
      </div>
    </div>
  )
}
