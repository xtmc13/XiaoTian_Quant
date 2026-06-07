import { useState, useEffect, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { KPICard } from '@/components/ui/KPICard'
import { OverfitRiskGauge } from '@/components/community/OverfitRiskGauge'
import { communityApi, indicatorApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import type { IndicatorItem } from '@/types'
import {
  Plus,
  TrendingUp,
  Download,
  Star,
  Eye,
  Layers,
  DollarSign,
  BarChart3,
  ArrowUpRight,
  ArrowDownRight,
  Loader2,
  Edit3,
  Trash2,
  AlertCircle,
  CheckCircle2,
  Clock,
  XCircle,
  Trophy,
  type LucideIcon,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

interface AuthorIndicator {
  id: number
  name: string
  description?: string
  pricing_type: 'free' | 'paid'
  price: number
  status: 'draft' | 'pending' | 'approved' | 'rejected'
  score?: number
  total_return?: number
  sharpe?: number
  max_drawdown?: number
  purchase_count: number
  avg_rating: number
  rating_count: number
  view_count: number
  revenue: number
  created_at: number
  updated_at: number
  kpi_score?: { total_score: number }
  overfit_risk?: { score: number; risk_level: 'low' | 'medium' | 'high' | 'insufficient_data' }
}

/* ── Helpers ─────────────────────────────────────────────────────── */

function formatPct(v?: number) {
  if (v == null || Number.isNaN(v)) return '—'
  const sign = v > 0 ? '+' : ''
  return `${sign}${v.toFixed(2)}%`
}

function formatDate(ts?: number) {
  if (!ts) return '—'
  return new Date(ts * 1000).toLocaleDateString('zh-CN')
}

const STATUS_META: Record<string, { label: string; class: string; icon: LucideIcon }> = {
  draft: { label: '草稿', class: 'bg-quant-bg-tertiary text-muted-foreground', icon: Clock },
  pending: { label: '审核中', class: 'bg-amber-500/10 text-amber-400', icon: Clock },
  approved: { label: '已上架', class: 'bg-quant-green/10 text-quant-green', icon: CheckCircle2 },
  rejected: { label: '未通过', class: 'bg-quant-red/10 text-quant-red', icon: XCircle },
}

/* ── Indicator Row ───────────────────────────────────────────────── */

function IndicatorRow({
  item,
  onDelete,
}: {
  item: AuthorIndicator
  onDelete: (id: number) => void
}) {
  const navigate = useNavigate()
  const status = STATUS_META[item.status] || STATUS_META.draft
  const StatusIcon = status.icon

  return (
    <div className="flex flex-col sm:flex-row sm:items-center gap-3 p-3 rounded-lg border border-quant-border bg-quant-card hover:border-quant-gold/20 transition-colors"
    >
      {/* Left: Info */}
      <div className="flex-1 min-w-0"
      >
        <div className="flex items-center gap-2"
        >
          <h4 className="text-sm font-semibold truncate"
          >{item.name}</h4
          >
          <span className={cn('flex items-center gap-1 px-1.5 py-0 rounded text-[10px] font-medium', status.class)}
          >
            <StatusIcon className="h-3 w-3" />
            {status.label}
          </span
          >
          {item.pricing_type === 'free' ? (
            <span className="px-1.5 py-0 rounded text-[10px] bg-quant-green/10 text-quant-green"
            >免费</span
            >
          ) : (
            <span className="px-1.5 py-0 rounded text-[10px] bg-quant-gold/10 text-quant-gold"
            >{item.price} 积分</span
            >
          )}
        </div
        >
        <p className="text-[11px] text-muted-foreground mt-0.5 truncate"
        >{item.description || '暂无描述'}</p
        >
        <div className="flex flex-wrap items-center gap-3 mt-1.5 text-[10px] text-muted-foreground"
        >
          <span className="flex items-center gap-1"
          ><Download className="h-3 w-3" /> {item.purchase_count}</span
          >
          <span className="flex items-center gap-1"
          ><Eye className="h-3 w-3" /> {item.view_count}</span
          >
          <span className="flex items-center gap-1"
          ><Star className="h-3 w-3 text-quant-gold fill-quant-gold" /> {item.avg_rating > 0 ? item.avg_rating.toFixed(1) : '-'}</span
          >
          <span className="flex items-center gap-1"
          ><DollarSign className="h-3 w-3" /> {item.revenue || 0}</span
          >
          <span>更新于 {formatDate(item.updated_at)}</span>
        </div>
      </div>

      {/* Right: KPI strip */}
      <div className="flex items-center gap-3 shrink-0"
      >
        <div className="text-right"
        >
          <div className="text-[9px] text-muted-foreground"
          >KPI</div>
          <div className="text-[11px] font-semibold font-mono text-quant-gold"
          >
            {item.kpi_score ? item.kpi_score.total_score.toFixed(1) : '—'}
          </div>
        </div>
        <div className="text-right"
        >
          <div className="text-[9px] text-muted-foreground"
          >收益</div>
          <div className={cn('text-[11px] font-semibold font-mono', (item.total_return || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}
          >
            {formatPct(item.total_return)}
          </div>
        </div>
        <div className="text-right"
        >
          <div className="text-[9px] text-muted-foreground"
          >夏普</div>
          <div className="text-[11px] font-semibold font-mono"
          >{item.sharpe != null ? item.sharpe.toFixed(2) : '—'}</div>
        </div>
        <div className="text-right"
        >
          <div className="text-[9px] text-muted-foreground"
          >回撤</div>
          <div className="text-[11px] font-semibold font-mono text-quant-red"
          >{formatPct(item.max_drawdown)}</div>
        </div>
        <div className="shrink-0"
        >
          {item.overfit_risk ? (
            <OverfitRiskGauge result={item.overfit_risk} size="sm" showLabel={false} />
          ) : (
            <span className="text-[10px] text-muted-foreground">未检测</span>
          )}
        </div>
        <div className="flex items-center gap-1"
        >
          <button
            onClick={() =>
              navigate(`/indicator-ide?id=${item.id}`)}
            className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-white/5 transition-colors"
            title="编辑"
          >
            <Edit3 className="h-3.5 w-3.5" />
          </button>
          <button
            onClick={() =>
              onDelete(item.id)}
            className="p-1.5 rounded text-muted-foreground hover:text-quant-red hover:bg-white/5 transition-colors"
            title="删除"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
    </div>
  )
}

/* ── Main Page ───────────────────────────────────────────────────── */

export function AuthorDashboard() {
  const navigate = useNavigate()
  const [indicators, setIndicators] = useState<AuthorIndicator[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<'all' | 'draft' | 'published'>('all')

  useEffect(() => {
    setLoading(true)
    indicatorApi.list().then((res: IndicatorItem[]) => {
      const list = Array.isArray(res) ? res : []
      const enriched = list.map((item) => ({
        ...item,
        status: (item.review_status || item.status || 'approved') as AuthorIndicator['status'],
        revenue: item.revenue || (item.purchase_count || 0) * (item.price || 0),
      }))
      setIndicators(enriched as unknown as AuthorIndicator[])
    }).catch(() => {
      setIndicators([])
    }).finally(() => setLoading(false))
  }, [])

  const filtered = useMemo(() => {
    if (filter === 'all') return indicators
    if (filter === 'draft') return indicators.filter((i) => i.status === 'draft')
    return indicators.filter((i) => i.status === 'approved' || i.status === 'pending')
  }, [indicators, filter])

  const stats = useMemo(() => {
    const totalRevenue = indicators.reduce((s, i) => s + (i.revenue || 0), 0)
    const totalDownloads = indicators.reduce((s, i) => s + (i.purchase_count || 0), 0)
    const totalViews = indicators.reduce((s, i) => s + (i.view_count || 0), 0)
    const avgRating = indicators.length > 0
      ? indicators.reduce((s, i) => s + (i.avg_rating || 0), 0) / indicators.length
      : 0
    const publishedCount = indicators.filter((i) => i.status === 'approved').length
    return { totalRevenue, totalDownloads, totalViews, avgRating, publishedCount }
  }, [indicators])

  const handleDelete = async (id: number) => {
    if (!confirm('确认删除该指标？此操作不可恢复。')) return
    try {
      await indicatorApi.delete(id)
      setIndicators((prev) => prev.filter((i) => i.id !== id))
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '删除失败')
    }
  }

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5"
    >
      <div className="space-y-5"
      >
        <PageHeader
          title="作者后台"
          subtitle="管理您发布的指标、查看销售数据"
          actions={
            <div className="flex items-center gap-2"
            >
              <button
                onClick={() =>
                  navigate('/strategy-leaderboard')}
                className="flex items-center gap-1.5 rounded-lg bg-quant-bg-secondary px-3 py-2 text-xs font-medium text-foreground hover:bg-white/5 transition-colors"
              >
                <Trophy className="h-3.5 w-3.5 text-quant-gold" />
                排行榜
              </button>
              <button
                onClick={() =>
                  navigate('/indicator-ide')}
                className="flex items-center gap-1.5 rounded-lg bg-quant-gold px-3 py-2 text-xs font-medium text-black hover:opacity-90 transition-opacity"
              >
                <Plus className="h-3.5 w-3.5" />
                新建指标
              </button>
            </div>
          }
        />

        {/* Stats Overview */}
        <div className="grid grid-cols-2 md:grid-cols-5 gap-3"
        >
          <KPICard
            icon={<DollarSign className="h-4 w-4 text-quant-gold" />}
            label="总收益"
            value={`${stats.totalRevenue} 积分`}
            trend="up"
          />
          <KPICard
            icon={<Download className="h-4 w-4 text-quant-green" />}
            label="总下载"
            value={String(stats.totalDownloads)}
            trend="up"
          />
          <KPICard
            icon={<Eye className="h-4 w-4 text-blue-400" />}
            label="总浏览"
            value={String(stats.totalViews)}
            trend="up"
          />
          <KPICard
            icon={<Star className="h-4 w-4 text-quant-gold" />}
            label="平均评分"
            value={stats.avgRating > 0 ? stats.avgRating.toFixed(1) : '-'}
            trend="neutral"
          />
          <KPICard
            icon={<Layers className="h-4 w-4 text-purple-400" />}
            label="已上架"
            value={`${stats.publishedCount} / ${indicators.length}`}
            trend="neutral"
          />
        </div>

        {/* Filters */}
        <div className="flex items-center gap-2"
        >
          {([
            { key: 'all', label: '全部' },
            { key: 'draft', label: '草稿' },
            { key: 'published', label: '已发布' },
          ] as const).map((f) => (
            <button
              key={f.key}
              onClick={() =>
                setFilter(f.key)}
              className={cn(
                'px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors',
                filter === f.key ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground bg-quant-bg-secondary'
              )}
            >
              {f.label}
            </button>
          ))}
        </div>

        {/* Indicator List */}
        {loading ? (
          <div className="flex items-center justify-center py-20 text-muted-foreground"
          >
            <Loader2 className="h-5 w-5 animate-spin mr-2" />
            加载中...
          </div>
        ) : filtered.length > 0 ? (
          <div className="space-y-2"
          >
            {filtered.map((item) => (
              <IndicatorRow key={item.id} item={item} onDelete={handleDelete} />
            ))}
          </div>
        ) : (
          <EmptyState
            title="暂无指标"
            description="点击右上角「新建指标」开始创建您的第一个策略指标"
            actionLabel="新建指标"
            onAction={() =>
              navigate('/indicator-ide')}
          />
        )}
      </div>
    </div>
  )
}
