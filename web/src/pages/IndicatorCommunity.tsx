import React, { useState, useMemo, useCallback, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import { PageHeader } from '@/components/ui/PageHeader'
import { communityApi, indicatorApi } from '@/lib/api'
import {
  Search,
  Star,
  Download,
  Eye,
  Trophy,
  ShoppingBag,
  Grid3X3,
  List,
  CheckCircle2,
  Clock,
  BarChart3,
  TrendingUp,
  TrendingDown,
  User,
  Plus,
  X,
  Zap,
  Layers,
  ChevronDown,
  Filter,
  Loader2,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

interface IndicatorAuthor {
  username: string
  nickname?: string
  avatar?: string
}

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
  applicable_symbols?: string[]
  applicable_timeframes?: string[]
  author: IndicatorAuthor
  purchase_count?: number
  avg_rating?: number
  view_count?: number
  created_at?: string
  is_purchased?: boolean
  is_own?: boolean
}

/* ── Data fetching helpers ──────────────────────────────────────── */

function useMarketIndicators(keyword: string, pricingFilter: string, sortBy: string, page: number) {
  const [data, setData] = useState<{ items: IndicatorItem[]; total: number; total_pages: number } | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    setLoading(true)
    communityApi.market({
      page,
      page_size: 12,
      keyword: keyword || undefined,
      pricing_type: pricingFilter === 'all' ? undefined : pricingFilter,
      sort_by: sortBy,
    }).then((res: any) => {
      const d = res?.data || res
      setData(d)
    }).catch(() => {
      setData({ items: [], total: 0, total_pages: 0 })
    }).finally(() => setLoading(false))
  }, [keyword, pricingFilter, sortBy, page])

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

function getInitials(name: string) {
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

function scoreBadgeClass(score?: number) {
  const s = score || 0
  if (s >= 80) return 'from-[#f5af19] to-[#f12711]'
  if (s >= 60) return 'from-[#36d1dc] to-[#5b86e5]'
  if (s >= 40) return 'from-[#8e8e8e] to-[#b4b4b4]'
  return 'from-[#333] to-[#555]'
}

/* ── No-op localStorage helpers (replaced by API-driven is_purchased) ── */

function getPurchasedIds(): number[] { return [] }
function addPurchase(_id: number) {}

/* ── Indicator Card ──────────────────────────────────────────────── */

const IndicatorCard = React.memo(function IndicatorCard({
  indicator,
  isPurchased,
  isOwn,
  onPurchase,
}: {
  indicator: IndicatorItem
  isPurchased: boolean
  isOwn: boolean
  onPurchase: (id: number) => void
}) {
  const navigate = useNavigate()
  const hasKpi =
    (indicator.sample_size || 0) > 0 ||
    (indicator.total_return || 0) !== 0 ||
    (indicator.sharpe || 0) !== 0 ||
    (indicator.max_drawdown || 0) !== 0

  const visibleSymbols = (indicator.applicable_symbols || []).slice(0, 2)
  const extraSymbols = Math.max(0, (indicator.applicable_symbols || []).length - 2)
  const visibleTimeframes = (indicator.applicable_timeframes || []).slice(0, 2)
  const extraTimeframes = Math.max(0, (indicator.applicable_timeframes || []).length - 2)

  return (
    <div className="group flex flex-col rounded-xl border border-quant-border bg-quant-card overflow-hidden transition-all hover:border-quant-gold/30 hover:shadow-lg hover:-translate-y-1">
      {/* Cover */}
      <div className={cn('relative h-36 bg-gradient-to-br flex flex-col items-center justify-center text-white', getGradient(indicator.id))}>
        <div className="absolute inset-0 pointer-events-none">
          <div className="absolute -top-6 -right-6 w-24 h-24 rounded-full bg-white/10" />
          <div className="absolute -bottom-8 -left-8 w-20 h-20 rounded-full bg-white/5" />
        </div>

        <span className="text-3xl font-bold tracking-wider z-10 drop-shadow-md">{getInitials(indicator.name)}</span>
        <span className="text-[11px] opacity-90 mt-1 z-10 max-w-[80%] truncate">{indicator.name}</span>

        {/* Price tag */}
        <div
          className={cn(
            'absolute top-2 right-2 px-2 py-0.5 rounded text-[10px] font-bold z-20',
            indicator.pricing_type === 'free'
              ? 'bg-quant-green text-white'
              : 'bg-gradient-to-r from-[#f5af19] to-[#f12711] text-white'
          )}
        >
          {indicator.pricing_type === 'free' ? '免费' : `${indicator.price} 积分`}
        </div>

        {/* Status tags */}
        {isOwn && (
          <div className="absolute bottom-2 left-2 px-2 py-0.5 rounded text-[10px] bg-black/60 text-white z-20">我的指标</div>
        )}
        {!isOwn && isPurchased && (
          <div className="absolute bottom-2 left-2 px-2 py-0.5 rounded text-[10px] bg-quant-green/90 text-white z-20 flex items-center gap-1">
            <CheckCircle2 className="h-3 w-3" /> 已购买
          </div>
        )}

        {/* Score badge */}
        {(indicator.sample_size || 0) > 0 && indicator.score != null && (
          <div
            className={cn(
              'absolute top-2 left-2 flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[10px] font-bold text-white z-20 shadow-md bg-gradient-to-r',
              scoreBadgeClass(indicator.score)
            )}
          >
            <Trophy className="h-3 w-3" />
            {indicator.score.toFixed(0)}
          </div>
        )}
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
        <div className="flex flex-wrap gap-1 mt-2">
          {visibleSymbols.map((s) => (
            <span key={s} className="px-1.5 py-0 rounded text-[10px] bg-blue-500/10 text-blue-400">{s}</span>
          ))}
          {extraSymbols > 0 && (
            <span className="px-1.5 py-0 rounded text-[10px] bg-quant-bg-tertiary text-muted-foreground">+{extraSymbols}</span>
          )}
          {visibleTimeframes.map((tf) => (
            <span key={tf} className="px-1.5 py-0 rounded text-[10px] bg-quant-green/10 text-quant-green">{tf}</span>
          ))}
          {extraTimeframes > 0 && (
            <span className="px-1.5 py-0 rounded text-[10px] bg-quant-bg-tertiary text-muted-foreground">+{extraTimeframes}</span>
          )}
        </div>

        {/* Author */}
        <div className="flex items-center gap-1.5 mt-2">
          <div className="h-5 w-5 rounded-full bg-quant-gold/20 text-quant-gold flex items-center justify-center text-[8px] font-bold">
            {getInitials(indicator.author.nickname || indicator.author.username)}
          </div>
          <span className="text-[11px] text-muted-foreground truncate">{indicator.author.nickname || indicator.author.username}</span>
        </div>

        {/* Overfit risk gauge */}
        {indicator.sample_size != null && indicator.sample_size > 0 && indicator.score != null && (
          <div className="flex items-center gap-1 mt-1.5">
            <span className="text-[8px] text-muted-foreground w-10 shrink-0">过拟合</span>
            <div className="flex-1 h-1 bg-quant-bg-tertiary rounded-full overflow-hidden">
              <div className={cn('h-full rounded-full',
                indicator.score >= 80 ? 'bg-quant-green' : indicator.score >= 50 ? 'bg-quant-gold' : 'bg-quant-red'
              )} style={{ width: `${Math.max(4, 100 - indicator.score)}%` }} />
            </div>
            <span className={cn('text-[8px] font-medium w-5 text-right',
              indicator.score >= 80 ? 'text-quant-green' : indicator.score >= 50 ? 'text-quant-gold' : 'text-quant-red'
            )}>{indicator.score >= 80 ? '低' : indicator.score >= 50 ? '中' : '高'}</span>
          </div>
        )}

        {/* Stats + Action */}
        <div className="flex items-center justify-between mt-auto pt-2">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <Download className="h-3 w-3" /> {indicator.purchase_count || 0}
            </span>
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <Star className="h-3 w-3 text-quant-gold fill-quant-gold" />
              {(indicator.avg_rating || 0) > 0 ? (indicator.avg_rating ?? 0).toFixed(1) : '-'}
            </span>
            <span className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <Eye className="h-3 w-3" /> {indicator.view_count || 0}
            </span>
          </div>

          {indicator.pricing_type === 'free' || isPurchased || isOwn ? (
            <button
              onClick={() => { navigate(`/indicator-ide?id=${indicator.id}`) }}
              className="px-2.5 py-1 rounded-md bg-quant-gold/10 text-quant-gold text-[10px] font-medium hover:bg-quant-gold/20 transition-colors flex items-center gap-1"
            >
              <Zap className="h-3 w-3" /> 使用
            </button>
          ) : (
            <button
              onClick={() => onPurchase(indicator.id)}
              className="px-2.5 py-1 rounded-md bg-white text-quant-bg text-[10px] font-medium hover:opacity-90 transition-opacity flex items-center gap-1"
            >
              <ShoppingBag className="h-3 w-3" /> 购买
            </button>
          )}
        </div>
      </div>
    </div>
  )
})

/* ── Main Page ───────────────────────────────────────────────────── */

export function IndicatorCommunity() {
  const navigate = useNavigate()
  const [activeTab, setActiveTab] = useState<'market' | 'author' | 'purchases'>('market')
  const [keyword, setKeyword] = useState('')
  const [pricingFilter, setPricingFilter] = useState<'all' | 'free' | 'paid'>('all')
  const [sortBy, setSortBy] = useState<'score' | 'newest' | 'hot' | 'rating' | 'price_asc' | 'price_desc'>('score')
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid')
  const [showPurchaseModal, setShowPurchaseModal] = useState<IndicatorItem | null>(null)
  const [page, setPage] = useState(1)
  const [purchasingId, setPurchasingId] = useState<number | null>(null)

  const { data: marketData, loading: marketLoading } = useMarketIndicators(keyword, pricingFilter, sortBy, page)
  const filtered = marketData?.items || []
  const totalPages = marketData?.total_pages || 0

  const handlePurchase = useCallback(async (id: number) => {
    const item = filtered.find((i) => i.id === id)
    if (!item) return
    if (item.pricing_type === 'free' || item.is_purchased || item.is_own) return
    setShowPurchaseModal(item)
  }, [filtered])

  const confirmPurchase = useCallback(async () => {
    if (!showPurchaseModal) return
    setPurchasingId(showPurchaseModal.id)
    try {
      await communityApi.purchase(showPurchaseModal.id)
      setShowPurchaseModal(null)
      setPage(1)
      // Force refresh by mutating dependency slightly
      setKeyword((k) => k)
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', err.message || '购买失败')
    } finally {
      setPurchasingId(null)
    }
  }, [showPurchaseModal])

  const myIndicators = useMemo(() => filtered.filter((i) => i.is_own), [filtered])
  const myPurchases = useMemo(() => filtered.filter((i) => i.is_purchased && !i.is_own), [filtered])

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="space-y-5">
        <PageHeader
          subtitle="发现、购买和分享量化交易策略指标"
          actions={
            <div className="flex items-center gap-2">
              <button
                onClick={() => setViewMode(viewMode === 'grid' ? 'list' : 'grid')}
                className="flex h-8 w-8 items-center justify-center rounded-lg border border-quant-border text-muted-foreground hover:text-foreground transition-colors"
                title={viewMode === 'grid' ? '列表视图' : '网格视图'}
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
              {activeTab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold" />}
            </button>
          ))}
        </div>

        {/* ── Market Tab ── */}
        {activeTab === 'market' && (
          <>
            {/* Toolbar */}
            <div className="flex flex-wrap items-center gap-3">
              <div className="relative flex-1 min-w-[200px] max-w-sm">
                <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
                <input
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  placeholder="搜索指标名称、描述或作者..."
                  className="w-full rounded-lg border border-quant-border bg-quant-bg-secondary pl-8 pr-3 py-2 text-xs text-white placeholder-muted-foreground outline-none focus:border-quant-gold"
                />
              </div>

              <div className="flex rounded-lg border border-quant-border overflow-hidden">
                {([
                  { key: 'all', label: '全部' },
                  { key: 'free', label: '免费' },
                  { key: 'paid', label: '付费' },
                ] as const).map((f) => (
                  <button
                    key={f.key}
                    onClick={() => setPricingFilter(f.key)}
                    className={cn(
                      'px-3 py-1.5 text-[11px] font-medium transition-colors',
                      pricingFilter === f.key ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground'
                    )}
                  >
                    {f.label}
                  </button>
                ))}
              </div>

              <div className="relative">
                <select
                  value={sortBy}
                  onChange={(e) => setSortBy(e.target.value as typeof sortBy)}
                  className="appearance-none rounded-lg border border-quant-border bg-quant-bg-secondary px-3 py-1.5 pr-7 text-[11px] text-white outline-none focus:border-quant-gold"
                >
                  <option value="score">综合评分</option>
                  <option value="newest">最新发布</option>
                  <option value="hot">最热下载</option>
                  <option value="rating">用户评分</option>
                  <option value="price_asc">价格从低到高</option>
                  <option value="price_desc">价格从高到低</option>
                </select>
                <ChevronDown className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
              </div>
            </div>

            {/* Grid */}
            {marketLoading ? (
              <div className="flex items-center justify-center py-20 text-muted-foreground">
                <Loader2 className="h-5 w-5 animate-spin mr-2" /> 加载中...
              </div>
            ) : filtered.length > 0 ? (
              <>
                <div
                  className={cn(
                    'gap-4',
                    viewMode === 'grid' ? 'grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3' : 'flex flex-col'
                  )}
                >
                  {filtered.map((item) => (
                    <IndicatorCard
                      key={item.id}
                      indicator={item}
                      isPurchased={item.is_purchased || false}
                      isOwn={item.is_own || false}
                      onPurchase={handlePurchase}
                    />
                  ))}
                </div>
                {/* Pagination */}
                {totalPages > 1 && (
                  <div className="flex items-center justify-center gap-2 pt-4">
                    <button
                      onClick={() => setPage((p) => Math.max(1, p - 1))}
                      disabled={page <= 1}
                      className="px-3 py-1 rounded border border-quant-border text-[11px] text-muted-foreground hover:text-foreground disabled:opacity-30"
                    >
                      上一页
                    </button>
                    <span className="text-[11px] text-muted-foreground">
                      {page} / {totalPages}
                    </span>
                    <button
                      onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                      disabled={page >= totalPages}
                      className="px-3 py-1 rounded border border-quant-border text-[11px] text-muted-foreground hover:text-foreground disabled:opacity-30"
                    >
                      下一页
                    </button>
                  </div>
                )}
              </>
            ) : (
              <EmptyState title="未找到指标" description="尝试更换搜索词或筛选条件" />
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
                  title="暂无发布的指标"
                  description="点击右上角「发布指标」创建您的第一个策略指标"
                  actionLabel="发布指标"
                  onAction={() => { navigate('/indicator-ide') }}
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
                    <div className="mt-1 text-sm font-bold text-white">{s.value}</div>
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
              <EmptyState title="暂无购买记录" description="去指标市场发现优质策略指标" />
            )}
          </SectionCard>
        )}

        {/* Purchase Modal */}
        {showPurchaseModal && (
          <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
          >
            <div className="w-full max-w-sm rounded-2xl border border-quant-border bg-quant-card p-6 shadow-2xl"
            >
              <div className="flex items-center justify-between mb-4"
              >
                <h3 className="text-base font-bold text-white">确认购买</h3>
                <button
                  onClick={() => setShowPurchaseModal(null)}
                  className="text-muted-foreground hover:text-foreground"
                >
                  <X className="h-4 w-4" />
                </button>
              </div>
              <p className="text-sm text-muted-foreground mb-4"
              >
                您即将购买指标 <span className="font-semibold text-foreground">{showPurchaseModal.name}</span>，价格为{' '}
                <span className="font-semibold text-quant-gold">{showPurchaseModal.price} 积分</span>。
              </p>
              <div className="flex items-center justify-end gap-2"
              >
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
                  {purchasingId === showPurchaseModal?.id && <Loader2 className="h-3 w-3 animate-spin" />}
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
