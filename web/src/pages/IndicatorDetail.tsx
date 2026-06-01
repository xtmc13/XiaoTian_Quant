import { useState, useEffect, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { communityApi, indicatorApi } from '@/lib/api'
import {
  Star,
  Download,
  Eye,
  Trophy,
  ShoppingBag,
  ArrowLeft,
  User,
  Calendar,
  Zap,
  CheckCircle2,
  Lock,
  Unlock,
  MessageSquare,
  Loader2,
  TrendingUp,
  TrendingDown,
  Target,
  BarChart3,
} from 'lucide-react'

/* ── Types ───────────────────────────────────────────────────────── */

interface IndicatorComment {
  id: number
  rating: number
  content: string
  created_at: number
  user_nickname: string
}

interface IndicatorDetailData {
  id: number
  name: string
  description?: string
  code?: string
  pricing_type: 'free' | 'paid'
  price: number
  score?: number
  total_return?: number
  sharpe?: number
  max_drawdown?: number
  win_rate?: number
  profit_factor?: number
  sample_size?: number
  applicable_symbols?: string[]
  applicable_timeframes?: string[]
  author_id: number
  author_name: string
  purchase_count: number
  avg_rating: number
  rating_count: number
  view_count: number
  created_at: number
  is_purchased: boolean
  is_own: boolean
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

function scoreBadgeClass(score?: number) {
  const s = score || 0
  if (s >= 80) return 'from-[#f5af19] to-[#f12711]'
  if (s >= 60) return 'from-[#36d1dc] to-[#5b86e5]'
  if (s >= 40) return 'from-[#8e8e8e] to-[#b4b4b4]'
  return 'from-[#333] to-[#555]'
}

/* ── Comment Card ────────────────────────────────────────────────── */

function CommentCard({ comment }: { comment: IndicatorComment }) {
  return (
    <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className="h-6 w-6 rounded-full bg-quant-gold/20 text-quant-gold flex items-center justify-center text-[10px] font-bold">
            {comment.user_nickname?.slice(0, 2) || 'U'}
          </div>
          <span className="text-xs font-medium">{comment.user_nickname}</span>
        </div>
        <div className="flex items-center gap-1">
          {Array.from({ length: 5 }).map((_, i) => (
            <Star
              key={i}
              className={cn('h-3 w-3', i < comment.rating ? 'text-quant-gold fill-quant-gold' : 'text-quant-border')}
            />
          ))}
        </div>
      </div>
      <p className="text-xs text-muted-foreground mt-2">{comment.content}</p>
      <span className="text-[10px] text-muted-foreground/60 mt-1 block">{formatDate(comment.created_at)}</span>
    </div>
  )
}

/* ── Main Page ───────────────────────────────────────────────────── */

export function IndicatorDetail() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [indicator, setIndicator] = useState<IndicatorDetailData | null>(null)
  const [loading, setLoading] = useState(true)
  const [comments, setComments] = useState<IndicatorComment[]>([])
  const [commentsTotal, setCommentsTotal] = useState(0)
  const [purchasing, setPurchasing] = useState(false)
  const [commentPage, setCommentPage] = useState(1)
  // Rating + comment submission
  const [myRating, setMyRating] = useState(0)
  const [myComment, setMyComment] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [ratingHover, setRatingHover] = useState(0)

  const indicatorId = Number(id)

  useEffect(() => {
    if (!indicatorId) return
    setLoading(true)
    Promise.all([
      indicatorApi.get(indicatorId).then((res: any) => {
        const d = res?.data ?? res
        setIndicator(d)
      }).catch(() => setIndicator(null)),
      communityApi.comments(indicatorId, 1, 20).then((res: any) => {
        const d = res?.data ?? res
        setComments(d?.comments || d || [])
        setCommentsTotal(d?.total || 0)
      }).catch(() => {}),
    ]).finally(() => setLoading(false))
  }, [indicatorId])

  const canViewCode = indicator?.is_own || indicator?.is_purchased || indicator?.pricing_type === 'free'

  const handleSubmitRating = async () => {
    if (!myRating || !myComment.trim()) return
    setSubmitting(true)
    try {
      await communityApi.addComment(indicatorId, { rating: myRating, content: myComment.trim() })
      // Refresh comments
      const res: any = await communityApi.comments(indicatorId, 1, 20)
      const d = res?.data ?? res
      setComments(d?.comments || d || [])
      setCommentsTotal(d?.total || 0)
      setMyComment('')
      setMyRating(0)
    } catch (e: any) { alert(e?.message || '提交失败') }
    finally { setSubmitting(false) }
  }

  const handlePurchase = async () => {
    if (!indicator || indicator.pricing_type === 'free' || indicator.is_purchased || indicator.is_own) return
    setPurchasing(true)
    try {
      await communityApi.purchase(indicator.id)
      setIndicator(prev => prev ? { ...prev, is_purchased: true } : prev)
    } catch (e: any) {
      alert(e.message || '购买失败')
    } finally {
      setPurchasing(false)
    }
  }

  const kpiItems = useMemo(() => {
    if (!indicator) return []
    return [
      { label: '总收益', value: formatPct(indicator.total_return), icon: TrendingUp, positive: (indicator.total_return || 0) >= 0 },
      { label: '夏普比率', value: indicator.sharpe != null ? indicator.sharpe.toFixed(2) : '—', icon: Target },
      { label: '最大回撤', value: formatPct(indicator.max_drawdown), icon: TrendingDown, positive: false },
      { label: '胜率', value: indicator.win_rate != null ? `${indicator.win_rate.toFixed(1)}%` : '—', icon: BarChart3 },
      { label: '盈亏比', value: indicator.profit_factor != null ? indicator.profit_factor.toFixed(2) : '—', icon: BarChart3 },
      { label: '样本量', value: indicator.sample_size != null ? String(indicator.sample_size) : '—', icon: BarChart3 },
    ]
  }, [indicator])

  if (loading) {
    return (
      <div className="h-full flex items-center justify-center text-muted-foreground">
        <Loader2 className="h-5 w-5 animate-spin mr-2" /> 加载中...
      </div>
    )
  }

  if (!indicator) {
    return (
      <div className="h-full p-5">
        <EmptyState title="指标不存在" description="该指标可能已被删除或您无权访问" />
        <button
          onClick={() => navigate('/indicator-community')}
          className="mt-4 flex items-center gap-1 text-xs text-quant-gold hover:underline mx-auto"
        >
          <ArrowLeft className="h-3 w-3" /> 返回指标市场
        </button>
      </div>
    )
  }

  return (
    <div className="h-full overflow-y-auto bg-quant-bg p-5">
      <div className="max-w-5xl mx-auto space-y-5">
        {/* Back + Header */}
        <button
          onClick={() => navigate('/indicator-community')}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          <ArrowLeft className="h-3.5 w-3.5" /> 返回市场
        </button>

        <PageHeader
          title={indicator.name}
          subtitle={indicator.description || '暂无描述'}
          actions={
            <div className="flex items-center gap-2">
              {indicator.pricing_type === 'free' ? (
                <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-quant-green text-white">免费</span>
              ) : (
                <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-gradient-to-r from-[#f5af19] to-[#f12711] text-white">
                  {indicator.price} 积分
                </span>
              )}
              {indicator.score != null && (
                <span className={cn('flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold text-white bg-gradient-to-r', scoreBadgeClass(indicator.score))}>
                  <Trophy className="h-3 w-3" /> {indicator.score.toFixed(0)}
                </span>
              )}
            </div>
          }
        />

        {/* Author + Meta */}
        <div className="flex flex-wrap items-center gap-4 text-xs text-muted-foreground">
          <div className="flex items-center gap-1.5">
            <User className="h-3.5 w-3.5" />
            <span className="font-medium text-foreground">{indicator.author_name}</span>
          </div>
          <div className="flex items-center gap-1.5">
            <Calendar className="h-3.5 w-3.5" />
            <span>发布于 {formatDate(indicator.created_at)}</span>
          </div>
          <div className="flex items-center gap-1.5">
            <Eye className="h-3.5 w-3.5" />
            <span>{indicator.view_count || 0} 浏览</span>
          </div>
          <div className="flex items-center gap-1.5">
            <Download className="h-3.5 w-3.5" />
            <span>{indicator.purchase_count || 0} 下载</span>
          </div>
          <div className="flex items-center gap-1.5">
            <Star className="h-3.5 w-3.5 text-quant-gold fill-quant-gold" />
            <span>{indicator.avg_rating > 0 ? indicator.avg_rating.toFixed(1) : '-'} ({indicator.rating_count || 0})</span>
          </div>
        </div>

        {/* KPI Grid */}
        {kpiItems.length > 0 && (
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
            {kpiItems.map((kpi) => (
              <div key={kpi.label} className="rounded-xl border border-quant-border bg-quant-card p-3">
                <div className="flex items-center gap-1.5 text-muted-foreground">
                  <kpi.icon className="h-3.5 w-3.5" />
                  <span className="text-[10px]">{kpi.label}</span>
                </div>
                <div className={cn('mt-1 text-sm font-bold font-mono',
                  kpi.positive === true ? 'text-quant-green' :
                    kpi.positive === false ? 'text-quant-red' : 'text-foreground'
                )}>
                  {kpi.value}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Tags */}
        {(indicator.applicable_symbols?.length || 0) > 0 || (indicator.applicable_timeframes?.length || 0) > 0 ? (
          <div className="flex flex-wrap gap-2">
            {indicator.applicable_symbols?.map((s) => (
              <span key={s} className="px-2 py-0.5 rounded text-[10px] bg-blue-500/10 text-blue-400 border border-blue-500/20">{s}</span>
            ))}
            {indicator.applicable_timeframes?.map((tf) => (
              <span key={tf} className="px-2 py-0.5 rounded text-[10px] bg-quant-green/10 text-quant-green border border-quant-green/20">{tf}</span>
            ))}
          </div>
        ) : null}

        {/* Action Bar */}
        <div className="flex items-center gap-3">
          {indicator.is_own ? (
            <div className="flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold/10 text-quant-gold text-xs font-medium">
              <CheckCircle2 className="h-3.5 w-3.5" /> 我的指标
            </div>
          ) : indicator.is_purchased || indicator.pricing_type === 'free' ? (
            <button
              onClick={() => navigate(`/indicator-ide?id=${indicator.id}`)}
              className="flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold text-black text-xs font-medium hover:opacity-90 transition-opacity"
            >
              <Zap className="h-3.5 w-3.5" /> 在 IDE 中打开
            </button>
          ) : (
            <button
              onClick={handlePurchase}
              disabled={purchasing}
              className="flex items-center gap-1.5 px-4 py-2 rounded-lg bg-white text-quant-bg text-xs font-medium hover:opacity-90 transition-opacity disabled:opacity-50"
            >
              {purchasing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShoppingBag className="h-3.5 w-3.5" />}
              {purchasing ? '购买中...' : `购买 (${indicator.price} 积分)`}
            </button>
          )}

          {canViewCode ? (
            <div className="flex items-center gap-1 text-[10px] text-quant-green">
              <Unlock className="h-3 w-3" /> 已解锁代码
            </div>
          ) : (
            <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
              <Lock className="h-3 w-3" /> 购买后查看代码
            </div>
          )}
        </div>

        {/* ── Rating Form ── */}
        {canViewCode && !indicator.is_own && (
          <SectionCard title="评价此指标">
            <div className="space-y-3">
              <div className="flex items-center gap-1">
                {[1, 2, 3, 4, 5].map((star) => (
                  <button
                    key={star}
                    onClick={() => setMyRating(star)}
                    onMouseEnter={() => setRatingHover(star)}
                    onMouseLeave={() => setRatingHover(0)}
                    className="p-0.5 transition-colors"
                  >
                    <Star className={cn('h-6 w-6',
                      star <= (ratingHover || myRating) ? 'text-quant-gold fill-quant-gold' : 'text-quant-border'
                    )} />
                  </button>
                ))}
                {myRating > 0 && <span className="text-xs text-muted-foreground ml-2">{myRating} 星</span>}
              </div>
              <textarea
                value={myComment}
                onChange={(e) => setMyComment(e.target.value)}
                placeholder="分享你的使用体验..."
                rows={2}
                className="w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold resize-none"
              />
              <button
                onClick={handleSubmitRating}
                disabled={submitting || !myRating || !myComment.trim()}
                className={cn('px-4 py-1.5 rounded-lg text-xs font-medium transition-opacity',
                  !myRating || !myComment.trim() ? 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed' :
                  'bg-quant-gold text-white hover:opacity-90')}
              >
                {submitting ? <Loader2 className="h-3.5 w-3.5 animate-spin inline mr-1" /> : null}
                提交评价
              </button>
            </div>
          </SectionCard>
        )}

        {/* Code Preview */}
        {canViewCode && indicator.code && (
          <SectionCard title="代码预览" bodyClassName="p-0 overflow-hidden">
            <div className="bg-quant-bg-secondary p-3 overflow-x-auto">
              <pre className="text-[11px] font-mono text-foreground/80 whitespace-pre-wrap">{indicator.code}</pre>
            </div>
          </SectionCard>
        )}

        {/* Comments */}
        <SectionCard
          title={`评论 (${commentsTotal})`}
          bodyClassName="space-y-3"
        >
          {comments.length > 0 ? (
            <>
              <div className="space-y-2">
                {comments.map((c) => (
                  <CommentCard key={c.id} comment={c} />
                ))}
              </div>
              {commentsTotal > comments.length && (
                <button
                  onClick={() => setCommentPage((p) => p + 1)}
                  className="w-full py-1.5 rounded border border-quant-border text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                >
                  加载更多评论
                </button>
              )}
            </>
          ) : (
            <EmptyState title="暂无评论" description="购买后发表第一条评论" />
          )}
        </SectionCard>
      </div>
    </div>
  )
}
