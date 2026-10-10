import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { BookmarkCheck } from 'lucide-react'
import { cn } from '@/lib/utils'
import { marketListingApi, type MarketSubscription } from '@/lib/api'
import { useI18n } from '@/i18n'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { Button } from '@/components/ui/Button'
import { toast } from '@/lib/useToast'

const STATUS_META: Record<string, { labelKey: string; cls: string }> = {
  active: { labelKey: 'market.subs.statusActive', cls: 'bg-quant-green/15 text-quant-green border-quant-green/40' },
  pending_payment: { labelKey: 'market.subs.statusPendingPayment', cls: 'bg-quant-gold/15 text-quant-gold border-quant-gold/40' },
  expired: { labelKey: 'market.subs.statusExpired', cls: 'bg-quant-bg text-muted-foreground border-quant-border' },
  cancelled: { labelKey: 'market.subs.statusCancelled', cls: 'bg-quant-bg text-muted-foreground border-quant-border' },
}

/** 订阅状态徽章。 */
function SubStatusBadge({ status }: { status: string }) {
  const { t } = useI18n()
  const meta = STATUS_META[status] ?? STATUS_META.cancelled
  return (
    <span className={cn('px-1.5 py-0.5 rounded border text-[10px] font-semibold shrink-0', meta.cls)}>
      {t(meta.labelKey)}
    </span>
  )
}

/** 我的订阅：全部市场条目订阅 + 实例入口（创建实例/查看实例三态与详情页一致）。 */
export function MySubscriptions() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: subs = [], isLoading } = useQuery({
    queryKey: ['market', 'my-subscriptions'],
    queryFn: () => marketListingApi.mySubscriptions(),
    refetchInterval: 30_000,
  })

  const instantiateMut = useMutation({
    mutationFn: (subId: number) => marketListingApi.instantiate(subId),
    onSuccess: async () => {
      toast('success', t('market.detail.instantiateOk'))
      await queryClient.invalidateQueries({ queryKey: ['market', 'my-subscriptions'] })
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('market.detail.instantiateFail')),
  })

  const feeLabel = (s: MarketSubscription) => {
    if (s.fee_type === 'monthly') return `${t('market.my.feeMonthly')} $${s.monthly_fee}`
    if (s.fee_type === 'profit_share') return `${t('market.my.feeProfitShare')} ${s.fee_percent}%`
    return t('market.my.feeFree')
  }

  const expiryLabel = (s: MarketSubscription) => {
    if (s.status !== 'active') return null
    return s.expires_at > 0
      ? `${t('market.detail.expiresAt')}: ${new Date(s.expires_at * 1000).toLocaleDateString()}`
      : t('market.detail.noExpiry')
  }

  const goInstance = (s: MarketSubscription) =>
    navigate('/bots', { state: { type: (s.object_kind ?? 'ai_bot') === 'grid_bot' ? 'grid' : 'ai' } })
  const goDetail = (s: MarketSubscription) => navigate(`/bots/ai/market/${s.listing_id}`)

  return (
    <div className="space-y-2">
      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : subs.length === 0 ? (
        <EmptyState
          icon={<BookmarkCheck className="w-8 h-8" />}
          title={t('market.subs.emptyTitle')}
          description={t('market.subs.emptyDesc')}
        />
      ) : (
        subs.map((s) => (
          <div key={s.id} className="bg-quant-card border border-quant-border rounded-xl p-3.5">
            <div className="flex items-center gap-2 flex-wrap">
              <button
                onClick={() => goDetail(s)}
                className="font-bold text-xs text-foreground truncate hover:text-quant-gold transition-colors"
              >
                {s.listing_name || s.listing_id}
              </button>
              <span className="px-1 py-0.5 rounded bg-violet-500/15 text-violet-400 text-[10px] shrink-0">
                {(s.object_kind ?? 'ai_bot') === 'grid_bot' ? t('market.my.objectGridBot') : t('market.my.objectAiBot')}
              </span>
              <span className="px-1 py-0.5 rounded bg-cyan-500/15 text-cyan-400 text-[10px] shrink-0">{feeLabel(s)}</span>
              <SubStatusBadge status={s.status} />
              <span className="flex-1" />
              {s.status === 'active' &&
                (s.instance_id ? (
                  <Button variant="secondary" size="sm" onClick={() => goInstance(s)}>
                    {t('market.detail.viewInstance')}
                  </Button>
                ) : (
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => instantiateMut.mutate(s.id)}
                    disabled={instantiateMut.isPending}
                    isLoading={instantiateMut.isPending}
                  >
                    {t('market.detail.instantiate')}
                  </Button>
                ))}
              {s.status === 'pending_payment' && (
                <Button variant="primary" size="sm" onClick={() => goDetail(s)}>
                  {t('market.subs.resumePayment')}
                </Button>
              )}
              {(s.status === 'expired' || s.status === 'cancelled') && (
                <Button variant="secondary" size="sm" onClick={() => goDetail(s)}>
                  {t('market.subs.resubscribe')}
                </Button>
              )}
              <Button variant="ghost" size="sm" onClick={() => goDetail(s)}>
                {t('market.subs.openDetail')}
              </Button>
            </div>
            <div className="mt-1.5 text-[10px] text-muted-foreground flex gap-3 flex-wrap">
              <span>
                {t('market.subs.createdAt')}: {new Date(s.created_at * 1000).toLocaleString('zh-CN')}
              </span>
              {expiryLabel(s) && <span>{expiryLabel(s)}</span>}
              {s.instance_id && <span className="font-mono">instance: {s.instance_id}</span>}
            </div>
          </div>
        ))
      )}
    </div>
  )
}

export default MySubscriptions
