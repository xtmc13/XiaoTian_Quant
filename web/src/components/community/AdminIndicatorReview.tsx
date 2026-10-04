import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck, Loader2 } from 'lucide-react'
import { communityAdminApi, type PendingReviewItem } from '@/lib/api'
import { useI18n } from '@/i18n'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { Skeleton } from '@/components/ui/Skeleton'
import { toast } from '@/lib/useToast'

/**
 * 社区指标上架审核队列（admin）——审的是 /community/reviews/pending + /community/review/:id，
 * 与 market/AdminMarketReview（market/listings 机器人/信号上架）是两套，勿混。
 * 挂载：UserManage 的「指标审核」tab（与 AdminMarketReview 同模式）。
 */
export function AdminIndicatorReview() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const { confirm, prompt, Dialog } = useConfirmDialog()
  const [page, setPage] = useState(1)

  const { data, isLoading } = useQuery({
    queryKey: ['admin-community', 'pending-indicators', page],
    queryFn: () => communityAdminApi.pendingReviews(page, 20),
    refetchInterval: 30_000,
  })
  const items = data?.items ?? []
  const totalPages = data?.total_pages ?? 0

  const reviewMut = useMutation({
    mutationFn: ({ id, approve, reason }: { id: number; approve: boolean; reason?: string }) =>
      communityAdminApi.review(id, approve, reason || ''),
    onSuccess: async (_, v) => {
      toast('success', v.approve ? t('community.review.approveOk') : t('community.review.rejectOk'))
      await queryClient.invalidateQueries({ queryKey: ['admin-community'] })
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('community.review.actionFail')),
  })

  const approve = async (item: PendingReviewItem) => {
    const ok = await confirm({
      title: t('community.review.approveConfirmTitle'),
      message: t('community.review.approveConfirmMsg').replace('{name}', item.name),
      confirmText: t('community.review.approve'),
    })
    if (ok) reviewMut.mutate({ id: item.id, approve: true })
  }

  const reject = async (item: PendingReviewItem) => {
    const reason = await prompt({
      title: t('community.review.rejectTitle'),
      inputLabel: t('community.review.rejectReasonLabel'),
      confirmText: t('community.review.reject'),
      variant: 'danger',
    })
    if (reason === null) return
    reviewMut.mutate({ id: item.id, approve: false, reason })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <ShieldCheck className="w-4 h-4 text-quant-gold" />
        <span className="text-sm font-medium">{t('community.review.title')}</span>
        <span className="text-xs text-muted-foreground">{t('community.review.subtitle')}</span>
        {data && <span className="text-xs text-muted-foreground">· {data.total}</span>}
      </div>

      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-16 rounded-xl" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <div className="text-center py-10 text-muted-foreground text-sm">{t('community.review.empty')}</div>
      ) : (
        <div className="space-y-2">
          {items.map((item) => (
            <div key={item.id} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-3.5">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-bold text-xs text-foreground">{item.name}</span>
                <span className="text-[10px] text-muted-foreground">
                  #{item.id} · {t('community.review.author')} {item.author_name || item.author_id}
                </span>
                <span className="px-1.5 py-0 rounded text-[10px] bg-quant-gold/10 text-quant-gold">
                  {item.pricing_type === 'free' ? t('community.review.free') : `${item.price}`}
                </span>
                <span className="text-[10px] text-muted-foreground">
                  {t('community.review.submittedAt')} {item.created_at ? new Date(item.created_at * 1000).toLocaleString('zh-CN') : '-'}
                </span>
                <span className="flex-1" />
                <button
                  onClick={() => approve(item)}
                  disabled={reviewMut.isPending}
                  className="px-3 py-1 rounded bg-quant-green text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50 inline-flex items-center gap-1"
                >
                  {reviewMut.isPending && <Loader2 className="w-3 h-3 animate-spin" />}
                  {t('community.review.approve')}
                </button>
                <button
                  onClick={() => reject(item)}
                  disabled={reviewMut.isPending}
                  className="px-3 py-1 rounded bg-quant-red text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                >
                  {t('community.review.reject')}
                </button>
              </div>
              {item.description && (
                <div className="mt-1.5 text-[10px] text-muted-foreground line-clamp-2">{item.description}</div>
              )}
            </div>
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2">
          <button
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={page <= 1}
            className="px-3 py-1 rounded border border-quant-border text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
          >
            ‹
          </button>
          <span className="text-xs text-muted-foreground">{page} / {totalPages}</span>
          <button
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            className="px-3 py-1 rounded border border-quant-border text-xs text-muted-foreground hover:text-foreground disabled:opacity-40"
          >
            ›
          </button>
        </div>
      )}

      <Dialog />
    </div>
  )
}

export default AdminIndicatorReview
