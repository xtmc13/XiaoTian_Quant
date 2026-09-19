import React, { useState, useMemo, useCallback, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import { communityApi } from '@/lib/api'
import {
  Store,
  Search,
  SearchX,
  X,
  Star,
  Download,
  Eye,
  Trophy,
  ShoppingBag,
  Grid3X3,
  List,
  CheckCircle2,
  TrendingUp,
  Plus,
  Zap,
  Layers,
  ChevronDown,
  Loader2,
  ArrowUpRight,
  PackageOpen,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

/** 与 /api/community/indicators 返回的列表项字段保持一致 */
interface IndicatorItem {
  id: number
  name: string
  description?: string
  pricing_type: 'free' | 'paid'
  price: number
  vip_free?: boolean
  score?: number
  sample_size?: number
  total_return?: number
  sharpe?: number
  max_drawdown?: number
  win_rate?: number
  profit_factor?: number
  applicable_symbols?: string[]
  applicable_timeframes?: string[]
  author_id?: number
  author_name?: string
  purchase_count?: number
  avg_rating?: number
  rating_count?: number
  view_count?: number
  created_at?: number | string
  is_purchased?: boolean
  is_own?: boolean
}

interface MarketPageData {
  items: IndicatorItem[]
  total: number
  total_pages: number
}

/* ── Constants ───────────────────────────────────────────────────── */

const PAGE_SIZE = 12
const MARKET_SORTS = [
  { key: 'score', label: '综合评分' },
  { key: 'newest', label: '最新发布' },
  { key: 'hot', label: '最热下载' },
  { key: 'rating', label: '用户评分' },
  { key: 'price_asc', label: '价格从低到高' },
  { key: 'price_desc', label: '价格从高到低' },
] as const

type SortKey = (typeof MARKET_SORTS)[number]['key']
type PricingFilter = 'all' | 'free' | 'paid'
type ViewMode = 'grid' | 'list'

/* ── Data fetching ──────────────────────────────────────────────── */

/**
 * communityApi.market 内部已解构出 items 数组返回；此处对两种可能的形态
 * （纯数组 / 完整分页对象）做归一化，并在分页元数据缺失时用满页启发式推算。
 */
function normalizeMarketResult(res: unknown, page: number, pageSize: number): MarketPageData {
  if (Array.isArray(res)) {
    return {
      items: res as IndicatorItem[],
      total: res.length,
      total_pages: res.length >= pageSize ? page + 1 : Math.max(1, page),
    }
  }
  const paged = (res ?? {}) as Partial<MarketPageData>
  const items = Array.isArray(paged.items) ? paged.items : []
  const estimated = items.length >= pageSize ? page + 1 : Math.max(1, page)
  return {
    items,
    total: typeof paged.total === 'number' ? paged.total : items.length,
    total_pages: typeof paged.total_pages === 'number' && paged.total_pages > 0 ? paged.total_pages : estimated,
  }
}

function useMarketIndicators(
  keyword: string,
  pricingFilter: PricingFilter,
  sortBy: SortKey,
  page: number,
  refreshTick: number
) {
  const [data, setData] = useState<MarketPageData | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    communityApi
      .market({
        page,
        page_size: PAGE_SIZE,
        keyword: keyword || undefined,
        pricing_type: pricingFilter === 'all' ? undefined : pricingFilter,
        sort_by: sortBy,
      })
      .then((res: unknown) => {
        setData(normalizeMarketResult(res, page, PAGE_SIZE))
      })
      .catch(() => {
        setData({ items: [], total: 0, total_pages: 0 })
      })
      .finally(() => setLoading(false))
  }, [keyword, pricingFilter, sortBy, page, refreshTick])

  return { data, loading }
}

/* ── Helpers ─────────────────────────────────────────────────────── */

const GRADIENTS = [
  'from-[#667eea] to-[#764ba2]',
  'from-[#f093fb] to-[#f5576c]',
  'from-[#4facfe] to-[#00f2fe]',
  'from-[#43e97b] to-[#38f9d7]',
  'from-[#fa709a] to-[#fee140]',
  'from-[#a8edea] to-[#fed6e3]',
  'from-[#d299c2] to-[#fef9d7]',
  'from-[#89f7fe] to-[#66a6ff]',
  'from-[#fddb92] to-[#d1fdff]',
  'from-[#9890e3] to-[#b1f4cf]',
  'from-[#ebc0fd] to-[#d9ded8]',
  'from-[#f6d365] to-[#fda085]',
]

function getGradient(id: number) {
  return GRADIENTS[id % GRADIENTS.length]
}

function getInitials(name?: string) {
  if (!name) return 'QT'
  if (/[一-龥]/.test(name)) return name.slice(0, 2)
  const words = name.split(/\s+/)
  if (words.length >= 2) return (words[0][0] + words[1][0]).toUpperCase()
  return name.slice(0, 2).toUpperCase()
}

function formatPct(v?: number) {
  if (v == null || Number.isNaN(v)) return '—'
  const sign = v > 0 ? '+' : ''
  return `${sign}${v.toFixed(2)}%`
}

function formatDate(ts?: number | string) {
  if (!ts) return ''
  const d = typeof ts === 'number' ? new Date(ts * 1000) : new Date(ts)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('zh-CN')
}

function scoreBadgeClass(score?: number) {
  const s = score || 0
  if (s >= 80) return 'from-[#f5af19] to-[#f12711]'
  if (s >= 60) return 'from-[#36d1dc] to-[#5b86e5]'
  if (s >= 40) return 'from-[#8e8e8e] to-[#b4b4b4]'
  return 'from-[#333] to-[#555]'
}

/* ── Loading skeletons ──────────────────────────────────────────── */

function IndicatorCardSkeleton({ variant }: { variant: ViewMode }) {
  if (variant === 'list') {
    return (
      <div className="flex gap-3 rounded-xl border border-quant-border bg-quant-card p-3">
        <Skeleton variant="rect" width={80} height={80} className="rounded-lg shrink-0" />
        <div className="flex-1 min-w-0 py-0.5">
          <Skeleton variant="text" width="35%" className="h-3.5" />
          <div className="mt-2">
            <Skeleton variant="text" lines={2} className="h-3" />
          </div>
          <Skeleton variant="text" width="50%" className="mt-2 h-3" />
        </div>
      </div>
    )
  }
  return (
    <div className="rounded-xl border border-quant-border bg-quant-card overflow-hidden">
      <Skeleton variant="rect" height={144} className="rounded-none" />
      <div className="p-3 space-y-2">
        <Skeleton variant="text" width="45%" className="h-3.5" />
        <Skeleton variant="text" lines={2} className="h-3" />
        <Skeleton variant="text" width="60%" className="h-3" />
      </div>
    </div>
  )
}

function MarketSkeleton({ variant }: { variant: ViewMode }) {
  if (variant === 'list') {
    return (
      <div className="flex flex-col gap-3">
        {Array.from({ length: 5 }).map((_, i) => (
          <IndicatorCardSkeleton key={i} variant="list" />
        ))}
      </div>
    )
  }
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
      {Array.from({ length: 6 }).map((_, i) => (
        <IndicatorCardSkeleton key={i} variant="grid" />
      ))}
    </div>
  )
}

/* ── Indicator Card ──────────────────────────────────────────────── */

interface IndicatorCardProps {
  indicator: IndicatorItem
  isPurchased: boolean
  isOwn: boolean
  variant?: ViewMode
  onPurchase: (id: number) => void
}

const IndicatorCard = React.memo(function IndicatorCard({
  indicator,
  isPurchased,
  isOwn,
  variant = 'grid',
  onPurchase,
}: IndicatorCardProps) {
  const navigate = useNavigate()
  const hasKpi =
    (indicator.sample_size || 0) > 0 ||
    (indicator.total_return || 0) !== 0 ||
    (indicator.sharpe || 0) !== 0 ||
    (indicator.max_drawdown || 0) !== 0

  const authorName = indicator.author_name || `作者 #${indicator.author_id ?? '?'}`
  const rating = (indicator.avg_rating || 0) > 0 ? (indicator.avg_rating ?? 0).toFixed(1) : null
  const publishedAt = formatDate(indicator.created_at)

  const visibleSymbols = (indicator.applicable_symbols || []).slice(0, 2)
  const extraSymbols = Math.max(0, (indicator.applicable_symbols || []).length - 2)
  const visibleTimeframes = (indicator.applicable_timeframes || []).slice(0, 2)
  const extraTimeframes = Math.max(0, (indicator.applicable_timeframes || []).length - 2)

  const openDetail = () => {
    navigate(`/indicator-community/${indicator.id}`)
  }

  const cover = (
    <div
      className={cn(
        'relative shrink-0 bg-gradient-to-br flex flex-col items-center justify-center text-foreground overflow-hidden',
        getGradient(indicator.id),
        variant === 'grid' ? 'h-36 w-full' : 'h-20 w-20 rounded-lg'
      )}
    >
      <div className="absolute inset-0 pointer-events-none">
        <div className="absolute -top-6 -right-6 w-24 h-24 rounded-full bg-white/10" />
        <div className="absolute -bottom-8 -left-8 w-20 h-20 rounded-full bg-white/5" />
      </div>

      <span
        className={cn(
          'font-bold tracking-wider z-10 drop-shadow-md',
          variant === 'grid' ? 'text-3xl' : 'text-sm'
        )}
      >
        {getInitials(indicator.name)}
      </span>
      {variant === 'grid' && (
        <span className="text-[11px] opacity-90 mt-1 z-10 max-w-[80%] truncate">{indicator.name}</span>
      )}

      {/* Price tag */}
      <div
        className={cn(
          'absolute z-20 px-1.5 py-0.5 rounded text-[10px] font-bold',
          variant === 'grid' ? 'top-2 right-2' : 'bottom-1 right-1',
          indicator.pricing_type === 'free'
            ? 'bg-quant-green text-white'
            : 'bg-gradient-to-r from-[#f5af19] to-[#f12711] text-foreground'
        )}
      >
        {indicator.pricing_type === 'free' ? '免费' : `${indicator.price} 积分`}
      </div>

      {/* Status tags */}
      {variant === 'grid' && isOwn && (
        <div className="absolute bottom-2 left-2 px-2 py-0.5 rounded text-[10px] bg-black/60 text-foreground z-20">我的指标</div>
      )}
      {variant === 'grid' && !isOwn && isPurchased && (
        <div className="absolute bottom-2 left-2 px-2 py-0.5 rounded text-[10px] bg-quant-green/90 text-white z-20 flex items-center gap-1">
          <CheckCircle2 className="h-3 w-3" /> 已购买
        </div>
      )}

      {/* Score badge */}
      {variant === 'grid' && (indicator.score || 0) > 0 && (
        <div
          className={cn(
            'absolute top-2 left-2 flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[10px] font-bold text-foreground z-20 shadow-md bg-gradient-to-r',
            scoreBadgeClass(indicator.score)
          )}
        >
          <Trophy className="h-3 w-3" />
          {(indicator.score ?? 0).toFixed(0)}
        </div>
      )}
    </div>
  )

  const actionButton =
    indicator.pricing_type === 'free' || isPurchased || isOwn ? (
      <button
        onClick={(e) => {
          e.stopPropagation()
          navigate(`/indicator-ide?id=${indicator.id}`)
        }}
        className="px-2.5 py-1 rounded-md bg-quant-gold/10 text-quant-gold text-[10px] font-medium hover:bg-quant-gold/20 transition-colors flex items-center gap-1"
      >
        <Zap className="h-3 w-3" /> 使用
      </button>
    ) : (
      <button
        onClick={(e) => {
          e.stopPropagation()
          onPurchase(indicator.id)
        }}
        className="px-2.5 py-1 rounded-md bg-white text-quant-bg text-[10px] font-medium hover:opacity-90 transition-opacity flex items-center gap-1"
      >
        <ShoppingBag className="h-3 w-3" /> 购买
      </button>
    )

  const statsRow = (
    <div className="flex items-center gap-3 min-w-0">
      <span className="flex items-center gap-1 text-[10px] text-muted-foreground" title="下载次数">
        <Download className="h-3 w-3" /> {indicator.purchase_count || 0}
      </span>
      <span className="flex items-center gap-1 text-[10px] text-muted-foreground" title="用户评分">
        <Star className="h-3 w-3 text-quant-gold fill-quant-gold" />
        {rating ?? '—'}
        {(indicator.rating_count || 0) > 0 && <span className="text-muted-foreground/70">({indicator.rating_count})</span>}
      </span>
      <span className="flex items-center gap-1 text-[10px] text-muted-foreground" title="浏览次数">
        <Eye className="h-3 w-3" /> {indicator.view_count || 0}
      </span>
    </div>
  )

  if (variant === 'list') {
    return (
      <div
        onClick={openDetail}
        className="group flex gap-3 rounded-xl border border-quant-border bg-quant-card p-3 cursor-pointer transition-all hover:border-quant-gold/30 hover:bg-quant-hover"
      >
        {cover}
        <div className="flex-1 min-w-0 flex flex-col">
          <div className="flex items-center gap-2">
            <h3 className="text-sm font-semibold text-foreground truncate" title={indicator.name}>
              {indicator.name}
            </h3>
            {isOwn && (
              <span className="shrink-0 px-1.5 py-0.5 rounded text-[10px] bg-quant-gold/10 text-quant-gold">我的指标</span>
            )}
            {!isOwn && isPurchased && (
              <span className="shrink-0 flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[10px] bg-quant-green/10 text-quant-green">
                <CheckCircle2 className="h-3 w-3" /> 已购买
              </span>
            )}
          </div>
          <p className="text-[11px] text-muted-foreground mt-0.5 truncate">{indicator.description || '暂无描述'}</p>
          <div className="flex items-center gap-2 mt-1.5 text-[10px] text-muted-foreground">
            <span className="flex items-center gap-1 min-w-0">
              <span className="h-4 w-4 shrink-0 rounded-full bg-quant-gold/20 text-quant-gold flex items-center justify-center text-[8px] font-bold">
                {getInitials(authorName)}
              </span>
              <span className="truncate">{authorName}</span>
            </span>
            {(indicator.applicable_symbols || []).length > 0 && (
              <span className="truncate">· {(indicator.applicable_symbols || []).slice(0, 3).join(' / ')}</span>
            )}
            {publishedAt && <span className="shrink-0">· {publishedAt}</span>}
          </div>
          <div className="flex items-center justify-between gap-3 mt-auto pt-2">
            {statsRow}
            {actionButton}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div
      onClick={openDetail}
      className="group flex flex-col rounded-xl border border-quant-border bg-quant-card overflow-hidden cursor-pointer transition-all hover:border-quant-gold/30 hover:shadow-lg hover:-translate-y-1"
    >
      <div className="relative">
        {cover}
        {/* Hover hint */}
        <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/40 opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none">
          <span className="flex items-center gap-1 px-2.5 py-1 rounded-full bg-white/15 text-foreground text-[10px] font-medium backdrop-blur-sm">
            查看详情 <ArrowUpRight className="h-3 w-3" />
          </span>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 flex flex-col p-3">
        <h3 className="text-sm font-semibold text-foreground truncate" title={indicator.name}>{indicator.name}</h3>
        <p className="text-[11px] text-muted-foreground mt-0.5 line-clamp-2 min-h-[2rem]">{indicator.description || '暂无描述'}</p>

        {/* KPI strip */}
        {hasKpi && (
          <div className="grid grid-cols-3 gap-1 p-1.5 mt-2 rounded-lg bg-quant-bg-tertiary/50">
            <div className="text-center">
              <div className="text-[9px] text-muted-foreground">总收益</div>
              <div className={cn('text-[11px] font-semibold truncate', (indicator.total_return || 0) >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                {formatPct(indicator.total_return)}
              </div>
            </div>
            <div className="text-center">
              <div className="text-[9px] text-muted-foreground">夏普</div>
              <div className={cn('text-[11px] font-semibold truncate', (indicator.sharpe || 0) >= 1 ? 'text-quant-green' : 'text-foreground')}>
                {indicator.sharpe != null ? indicator.sharpe.toFixed(2) : '—'}
              </div>
            </div>
            <div className="text-center">
              <div className="text-[9px] text-muted-foreground">回撤</div>
              <div className="text-[11px] font-semibold text-quant-red truncate">{formatPct(indicator.max_drawdown)}</div>
            </div>
          </div>
        )}

        {/* Tags */}
        {(visibleSymbols.length > 0 || visibleTimeframes.length > 0) && (
          <div className="flex flex-wrap gap-1 mt-2">
            {visibleSymbols.map((s) => (
              <span key={s} className="px-1.5 py-0.5 rounded text-[10px] bg-blue-500/10 text-blue-400">{s}</span>
            ))}
            {extraSymbols > 0 && (
              <span className="px-1.5 py-0.5 rounded text-[10px] bg-quant-bg-tertiary text-muted-foreground">+{extraSymbols}</span>
            )}
            {visibleTimeframes.map((tf) => (
              <span key={tf} className="px-1.5 py-0.5 rounded text-[10px] bg-quant-green/10 text-quant-green">{tf}</span>
            ))}
            {extraTimeframes > 0 && (
              <span className="px-1.5 py-0.5 rounded text-[10px] bg-quant-bg-tertiary text-muted-foreground">+{extraTimeframes}</span>
            )}
          </div>
        )}

        {/* Author */}
        <div className="flex items-center gap-1.5 mt-2">
          <div className="h-5 w-5 rounded-full bg-quant-gold/20 text-quant-gold flex items-center justify-center text-[8px] font-bold">
            {getInitials(authorName)}
          </div>
          <span className="text-[11px] text-muted-foreground truncate">{authorName}</span>
        </div>

        {/* Overfit risk gauge */}
        {(indicator.sample_size || 0) > 0 && indicator.score != null && (
          <div className="flex items-center gap-1 mt-1.5">
            <span className="text-[8px] text-muted-foreground w-10 shrink-0">过拟合</span>
            <div className="flex-1 h-1 bg-quant-bg-tertiary rounded-full overflow-hidden">
              <div className={cn('h-full rounded-full',
                (indicator.score ?? 0) >= 80 ? 'bg-quant-green' : (indicator.score ?? 0) >= 50 ? 'bg-quant-gold' : 'bg-quant-red'
              )} style={{ width: `${Math.max(4, 100 - (indicator.score ?? 0))}%` }} />
            </div>
            <span className={cn('text-[8px] font-medium w-5 text-right',
              (indicator.score ?? 0) >= 80 ? 'text-quant-green' : (indicator.score ?? 0) >= 50 ? 'text-quant-gold' : 'text-quant-red'
            )}>{(indicator.score ?? 0) >= 80 ? '低' : (indicator.score ?? 0) >= 50 ? '中' : '高'}</span>
          </div>
        )}

        {/* Stats + Action */}
        <div className="flex items-center justify-between mt-auto pt-2">
          {statsRow}
          {actionButton}
        </div>
      </div>
    </div>
  )
})

/* ── Main Page ───────────────────────────────────────────────────── */

export function IndicatorCommunity() {
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState<'market' | 'author' | 'purchases'>('market')
  const [keywordInput, setKeywordInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [pricingFilter, setPricingFilter] = useState<PricingFilter>('all')
  const [sortBy, setSortBy] = useState<SortKey>('score')
  const [viewMode, setViewMode] = useState<ViewMode>('grid')
  const [showPurchaseModal, setShowPurchaseModal] = useState<IndicatorItem | null>(null)
  const [page, setPage] = useState(1)
  const [purchasingId, setPurchasingId] = useState<number | null>(null)
  const [refreshTick, setRefreshTick] = useState(0)

  // 搜索防抖，避免每次按键都请求接口
  useEffect(() => {
    const t = setTimeout(() => setKeyword(keywordInput.trim()), 300)
    return () => clearTimeout(t)
  }, [keywordInput])

  const { data: marketData, loading: marketLoading } = useMarketIndicators(
    keyword,
    pricingFilter,
    sortBy,
    page,
    refreshTick
  )
  const filtered = useMemo(() => marketData?.items ?? [], [marketData])
  const totalPages = marketData?.total_pages || 0

  const resetToFirstPage = useCallback(() => setPage(1), [])

  const handleKeywordChange = useCallback(
    (v: string) => {
      setKeywordInput(v)
      resetToFirstPage()
    },
    [resetToFirstPage]
  )

  const handlePricingFilter = useCallback(
    (f: PricingFilter) => {
      setPricingFilter(f)
      resetToFirstPage()
    },
    [resetToFirstPage]
  )

  const handleSortChange = useCallback(
    (s: SortKey) => {
      setSortBy(s)
      resetToFirstPage()
    },
    [resetToFirstPage]
  )

  const clearFilters = useCallback(() => {
    setKeywordInput('')
    setPricingFilter('all')
    resetToFirstPage()
  }, [resetToFirstPage])

  const handlePurchase = useCallback(
    (id: number) => {
      const item = filtered.find((i) => i.id === id)
      if (!item) return
      if (item.pricing_type === 'free' || item.is_purchased || item.is_own) return
      setShowPurchaseModal(item)
    },
    [filtered]
  )

  const confirmPurchase = useCallback(async () => {
    if (!showPurchaseModal) return
    setPurchasingId(showPurchaseModal.id)
    try {
      await communityApi.purchase(showPurchaseModal.id)
      setShowPurchaseModal(null)
      setPage(1)
      setRefreshTick((t) => t + 1)
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '购买失败')
    } finally {
      setPurchasingId(null)
    }
  }, [showPurchaseModal])

  const myIndicators = useMemo(() => filtered.filter((i) => i.is_own), [filtered])
  const myPurchases = useMemo(() => filtered.filter((i) => i.is_purchased && !i.is_own), [filtered])

  const hasActiveFilters = keyword !== '' || pricingFilter !== 'all'

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title="指标市场"
          subtitle="发现、购买和分享量化交易策略指标"
          icon={<Store className="w-5 h-5" />}
          actions={
            <div className="flex items-center gap-2">
              <button
                onClick={() => setViewMode(viewMode === 'grid' ? 'list' : 'grid')}
                className="flex h-9 w-9 items-center justify-center rounded-lg border border-quant-border text-muted-foreground hover:text-foreground hover:border-quant-gold/40 transition-colors"
                title={viewMode === 'grid' ? '切换为列表视图' : '切换为网格视图'}
              >
                {viewMode === 'grid' ? <List className="h-4 w-4" /> : <Grid3X3 className="h-4 w-4" />}
              </button>
              <button
                onClick={() => { navigate('/indicator-ide') }}
                className="flex items-center gap-1.5 rounded-lg bg-quant-gold px-3 py-2 text-xs font-medium text-black hover:opacity-90 transition-opacity"
              >
                <Plus className="h-3.5 w-3.5" /> 发布指标
              </button>
            </div>
          }
        />

        {/* Tabs */}
        <div className="flex border-b border-quant-border">
          {([
            { key: 'market', label: '指标市场' },
            { key: 'author', label: '我的指标' },
            { key: 'purchases', label: '我的购买' },
          ] as const).map((t) => (
            <button
              key={t.key}
              onClick={() => setActiveTab(t.key)}
              className={cn(
                'px-4 py-2.5 text-xs font-medium transition-colors relative',
                activeTab === t.key ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {t.label}
              {activeTab === t.key && <span className="absolute bottom-0 left-3 right-3 h-0.5 rounded-full bg-quant-gold" />}
            </button>
          ))}
        </div>

        {/* ── Market Tab ── */}
        {activeTab === 'market' && (
          <>
            {/* Toolbar */}
            <div className="flex flex-wrap items-center gap-3">
              <div className="relative flex-1 min-w-[220px] max-w-sm">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                <input
                  value={keywordInput}
                  onChange={(e) => handleKeywordChange(e.target.value)}
                  placeholder="搜索指标名称或描述..."
                  className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary pl-9 pr-8 py-2 text-xs text-foreground placeholder:text-muted-foreground/70 focus:outline-none focus:border-quant-gold"
                />
                {keywordInput && (
                  <button
                    onClick={() => handleKeywordChange('')}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                    title="清空搜索"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>

              <div className="flex rounded-lg border border-quant-border overflow-hidden">
                {([
                  { key: 'all', label: '全部' },
                  { key: 'free', label: '免费' },
                  { key: 'paid', label: '付费' },
                ] as const).map((f) => (
                  <button
                    key={f.key}
                    onClick={() => handlePricingFilter(f.key)}
                    className={cn(
                      'px-3.5 py-2 text-[11px] font-medium transition-colors',
                      pricingFilter === f.key ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground hover:bg-quant-hover'
                    )}
                  >
                    {f.label}
                  </button>
                ))}
              </div>

              <div className="relative">
                <select
                  value={sortBy}
                  onChange={(e) => handleSortChange(e.target.value as SortKey)}
                  className="appearance-none rounded-lg border border-quant-border bg-quant-bg-secondary pl-3 pr-8 py-2 text-[11px] text-foreground focus:outline-none focus:border-quant-gold cursor-pointer"
                >
                  {MARKET_SORTS.map((s) => (
                    <option key={s.key} value={s.key}>{s.label}</option>
                  ))}
                </select>
                <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
              </div>
            </div>

            {/* Grid / List */}
            {marketLoading ? (
              <MarketSkeleton variant={viewMode} />
            ) : filtered.length > 0 ? (
              <>
                <div
                  className={cn(
                    viewMode === 'grid'
                      ? 'grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4'
                      : 'flex flex-col gap-3'
                  )}
                >
                  {filtered.map((item) => (
                    <IndicatorCard
                      key={item.id}
                      indicator={item}
                      isPurchased={item.is_purchased || false}
                      isOwn={item.is_own || false}
                      variant={viewMode}
                      onPurchase={handlePurchase}
                    />
                  ))}
                </div>
                {/* Pagination */}
                {totalPages > 1 && (
                  <div className="flex items-center justify-center gap-3 pt-2">
                    <button
                      onClick={() => setPage((p) => Math.max(1, p - 1))}
                      disabled={page <= 1}
                      className="px-3 py-1.5 rounded-lg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/40 transition-colors disabled:opacity-30 disabled:hover:border-quant-border"
                    >
                      上一页
                    </button>
                    <span className="text-[11px] text-muted-foreground tabular-nums">
                      {page} / {totalPages}
                    </span>
                    <button
                      onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                      disabled={page >= totalPages}
                      className="px-3 py-1.5 rounded-lg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/40 transition-colors disabled:opacity-30 disabled:hover:border-quant-border"
                    >
                      下一页
                    </button>
                  </div>
                )}
              </>
            ) : (
              <div className="rounded-xl border border-quant-border bg-quant-card py-10">
                <EmptyState
                  icon={<SearchX className="h-6 w-6" />}
                  title="未找到相关指标"
                  description={hasActiveFilters ? '尝试更换关键词，或清除当前筛选条件' : '暂时没有已发布的指标，稍后再来看看'}
                  action={
                    hasActiveFilters ? (
                      <button
                        onClick={clearFilters}
                        className="px-4 py-2 text-xs rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/30 hover:bg-quant-gold/20 transition-colors"
                      >
                        清除筛选
                      </button>
                    ) : undefined
                  }
                />
              </div>
            )}
          </>
        )}

        {/* ── Author Tab ── */}
        {activeTab === 'author' && (
          <div className="space-y-4">
            <SectionCard title="我的指标" bodyClassName="space-y-3">
              {myIndicators.length > 0 ? (
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                  {myIndicators.map((item) => (
                    <IndicatorCard
                      key={item.id}
                      indicator={item}
                      isPurchased={false}
                      isOwn={true}
                      onPurchase={handlePurchase}
                    />
                  ))}
                </div>
              ) : (
                <EmptyState
                  icon={<Plus className="h-6 w-6" />}
                  title="暂无发布的指标"
                  description="点击右上角「发布指标」创建您的第一个策略指标"
                  action={
                    <button
                      onClick={() => { navigate('/indicator-ide') }}
                      className="flex items-center gap-1.5 px-4 py-2 text-xs rounded-lg bg-quant-gold text-black font-medium hover:opacity-90 transition-opacity"
                    >
                      <Plus className="h-3.5 w-3.5" /> 发布指标
                    </button>
                  }
                />
              )}
            </SectionCard>

            <SectionCard title="销售统计" bodyClassName="space-y-3">
              <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                {[
                  { label: '总销售额', value: '¥0', icon: TrendingUp },
                  { label: '总下载', value: '0', icon: Download },
                  { label: '平均评分', value: '-', icon: Star },
                  { label: '指标数量', value: String(myIndicators.length), icon: Layers },
                ].map((s) => (
                  <div key={s.label} className="rounded-lg border border-quant-border bg-quant-bg p-3">
                    <div className="flex items-center gap-2 text-muted-foreground">
                      <s.icon className="h-3.5 w-3.5" />
                      <span className="text-[10px]">{s.label}</span>
                    </div>
                    <div className="mt-1 text-sm font-bold text-foreground">{s.value}</div>
                  </div>
                ))}
              </div>
            </SectionCard>
          </div>
        )}

        {/* ── Purchases Tab ── */}
        {activeTab === 'purchases' && (
          <SectionCard title="我的购买" bodyClassName="space-y-3">
            {myPurchases.length > 0 ? (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                {myPurchases.map((item) => (
                  <IndicatorCard
                    key={item.id}
                    indicator={item}
                    isPurchased={true}
                    isOwn={false}
                    onPurchase={handlePurchase}
                  />
                ))}
              </div>
            ) : (
              <EmptyState
                icon={<PackageOpen className="h-6 w-6" />}
                title="暂无购买记录"
                description="去指标市场发现优质策略指标"
                action={
                  <button
                    onClick={() => setActiveTab('market')}
                    className="px-4 py-2 text-xs rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/30 hover:bg-quant-gold/20 transition-colors"
                  >
                    去市场看看
                  </button>
                }
              />
            )}
          </SectionCard>
        )}

        {/* Purchase Modal */}
        {showPurchaseModal && (
          <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
            <div className="w-full max-w-sm rounded-xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden">
              <div className="flex items-center justify-between px-5 py-4 border-b border-quant-border">
                <h3 className="text-sm font-bold text-foreground">确认购买</h3>
                <button
                  onClick={() => setShowPurchaseModal(null)}
                  className="text-muted-foreground hover:text-foreground transition-colors"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
              <div className="px-5 py-4 space-y-3">
                <div className="flex items-center gap-3 rounded-lg bg-quant-bg p-3">
                  <div className={cn('h-10 w-10 shrink-0 rounded-lg bg-gradient-to-br flex items-center justify-center text-foreground text-xs font-bold', getGradient(showPurchaseModal.id))}>
                    {getInitials(showPurchaseModal.name)}
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="text-xs font-semibold text-foreground truncate">{showPurchaseModal.name}</div>
                    <div className="text-[10px] text-muted-foreground truncate">
                      {showPurchaseModal.author_name || '社区作者'} ·{' '}
                      {showPurchaseModal.pricing_type === 'free' ? '免费' : `${showPurchaseModal.price} 积分`}
                    </div>
                  </div>
                  {showPurchaseModal.pricing_type !== 'free' && (
                    <span className="shrink-0 text-sm font-bold text-quant-gold tabular-nums">{showPurchaseModal.price} 积分</span>
                  )}
                </div>
                <p className="text-[11px] text-muted-foreground leading-relaxed">
                  购买后可永久使用该指标，并在「我的购买」中随时查看；评价后可解锁作者后续更新提醒。
                </p>
              </div>
              <div className="flex items-center justify-end gap-2 px-5 py-4 bg-quant-bg-secondary/50">
                <button
                  onClick={() => setShowPurchaseModal(null)}
                  className="rounded-lg border border-quant-border bg-quant-bg px-4 py-2 text-xs text-muted-foreground hover:text-foreground transition-colors"
                >
                  取消
                </button>
                <button
                  onClick={confirmPurchase}
                  disabled={purchasingId != null}
                  className="rounded-lg bg-quant-gold px-4 py-2 text-xs font-medium text-black hover:opacity-90 transition-opacity disabled:opacity-50 flex items-center gap-1"
                >
                  {purchasingId === showPurchaseModal.id && <Loader2 className="h-3 w-3 animate-spin" />}
                  确认购买
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
