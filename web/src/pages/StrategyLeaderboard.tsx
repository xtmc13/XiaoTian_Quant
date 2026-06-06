import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { strategyCommunityApi } from '@/lib/api'
import { OverfitRiskGauge } from '@/components/community/OverfitRiskGauge'
import type { StrategyCommunityItem } from '@/types'
import { Trophy, TrendingUp, BarChart3, Star, Loader2, Download, MessageSquare } from 'lucide-react'

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

function StrategyCard({ item, rank }: { item: StrategyCommunityItem; rank: number }) {
  const navigate = useNavigate()
  const kpi = item.kpi_score
  const overfit = item.overfit_risk

  return (
    <div
      className="flex flex-col gap-3 p-4 rounded-xl border border-quant-border bg-quant-card hover:border-quant-gold/30 transition-colors cursor-pointer"
      onClick={() => navigate(`/indicator-community/${item.id}`)}
    >
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          <div className={cn(
            'flex h-8 w-8 items-center justify-center rounded-full text-xs font-bold',
            rank === 1 ? 'bg-quant-gold text-black' :
            rank === 2 ? 'bg-gray-300 text-black' :
            rank === 3 ? 'bg-amber-700 text-white' :
            'bg-quant-bg-tertiary text-muted-foreground'
          )}>
            {rank}
          </div>
          <div>
            <h3 className="text-sm font-semibold">{item.name}</h3>
            <p className="text-[11px] text-muted-foreground">by {item.author_name || item.author}</p>
          </div>
        </div>
        {kpi && (
          <div className="text-right">
            <div className="text-lg font-bold text-quant-gold">{kpi.total_score.toFixed(1)}</div>
            <div className="text-[9px] text-muted-foreground">KPI评分</div>
          </div>
        )}
      </div>

      <div className="grid grid-cols-4 gap-2 text-center">
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className={cn('text-xs font-semibold font-mono', (item.total_return || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
            {formatPct(item.total_return)}
          </div>
          <div className="text-[9px] text-muted-foreground">收益</div>
        </div>
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className="text-xs font-semibold font-mono">{item.sharpe_ratio != null ? item.sharpe_ratio.toFixed(2) : '—'}</div>
          <div className="text-[9px] text-muted-foreground">夏普</div>
        </div>
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className="text-xs font-semibold font-mono text-quant-red">{formatPct(item.max_drawdown)}</div>
          <div className="text-[9px] text-muted-foreground">回撤</div>
        </div>
        <div className="rounded-lg bg-quant-bg-secondary p-2">
          <div className="text-xs font-semibold font-mono">{item.win_rate != null ? `${item.win_rate.toFixed(1)}%` : '—'}</div>
          <div className="text-[9px] text-muted-foreground">胜率</div>
        </div>
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
          <span className="flex items-center gap-1"><Download className="h-3 w-3" /> {item.download_count || 0}</span>
          <span className="flex items-center gap-1"><Star className="h-3 w-3" /> {item.rating_count || 0}</span>
          <span className="flex items-center gap-1"><MessageSquare className="h-3 w-3" /> {item.comment_count || 0}</span>
        </div>
        <OverfitRiskGauge result={overfit} size="sm" showLabel={true} />
      </div>
    </div>
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

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="space-y-5 max-w-5xl mx-auto">
        <PageHeader
          title="策略排行榜"
          subtitle="基于KPI综合评分、收益率、夏普比率和人气的策略排名"
        />

        <div className="flex items-center gap-2">
          {SORT_TABS.map((tab) => {
            const Icon = tab.icon
            return (
              <button
                key={tab.key}
                onClick={() => setSortBy(tab.key)}
                className={cn(
                  'flex items-center gap-1.5 px-3 py-2 rounded-lg text-xs font-medium transition-colors',
                  sortBy === tab.key ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground bg-quant-bg-secondary'
                )}
              >
                <Icon className="h-3.5 w-3.5" />
                {tab.label}
              </button>
            )
          })}
        </div>

        {loading ? (
          <div className="flex items-center justify-center py-20 text-muted-foreground">
            <Loader2 className="h-5 w-5 animate-spin mr-2" />
            加载中...
          </div>
        ) : items.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {items.map((item, idx) => (
              <StrategyCard key={item.id} item={item} rank={idx + 1} />
            ))}
          </div>
        ) : (
          <div className="text-center py-20 text-muted-foreground text-sm">
            暂无策略数据
          </div>
        )}
      </div>
    </div>
  )
}
