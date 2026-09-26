import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldCheck } from 'lucide-react'
import { cn } from '@/lib/utils'
import { adminMarketApi, marketListingApi, type MarketListing, type MarketListingStatus } from '@/lib/api'
import { useI18n } from '@/i18n'
import { Skeleton } from '@/components/ui/Skeleton'
import { toast } from '@/lib/useToast'

const QUEUE_TABS: { key: MarketListingStatus; labelKey: string }[] = [
  { key: 'pending_review', labelKey: 'market.status.pendingReview' },
  { key: 'probation', labelKey: 'market.status.probation' },
  { key: 'listed', labelKey: 'market.status.listed' },
  { key: 'rejected', labelKey: 'market.status.rejected' },
  { key: 'delisted', labelKey: 'market.status.delisted' },
]

/** 管理员上架审核队列 + 考核规则配置（嵌入 UserManage 的「上架审核」tab）。 */
export function AdminMarketReview() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<MarketListingStatus>('pending_review')
  const [rulesForm, setRulesForm] = useState<{ min_days: string; min_trades: string; max_drawdown_pct: string } | null>(null)

  const { data: listings = [], isLoading } = useQuery({
    queryKey: ['admin-market', 'listings', status],
    queryFn: () => adminMarketApi.listings(status),
    refetchInterval: 30_000,
  })
  const { data: rules } = useQuery({ queryKey: ['market', 'rules'], queryFn: () => marketListingApi.rules() })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin-market'] })

  const reviewMut = useMutation({
    mutationFn: ({ id, action, reason }: { id: string; action: 'approve' | 'reject' | 'delist'; reason?: string }) => {
      if (action === 'approve') return adminMarketApi.approve(id)
      if (action === 'reject') return adminMarketApi.reject(id, reason || '')
      return adminMarketApi.delist(id, reason || '')
    },
    onSuccess: async (_, v) => {
      const msg =
        v.action === 'approve'
          ? t('market.admin.approveOk')
          : v.action === 'reject'
            ? t('market.admin.rejectOk')
            : t('market.admin.delistOk')
      toast('success', msg)
      await invalidate()
      await queryClient.invalidateQueries({ queryKey: ['market'] })
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('market.admin.actionFail')),
  })

  const rulesMut = useMutation({
    mutationFn: () =>
      adminMarketApi.saveRules({
        min_days: parseInt(rulesForm?.min_days || '30', 10),
        min_trades: parseInt(rulesForm?.min_trades || '10', 10),
        max_drawdown_pct: parseFloat(rulesForm?.max_drawdown_pct || '50'),
      }),
    onSuccess: async () => {
      toast('success', t('market.admin.rulesSaved'))
      setRulesForm(null)
      await queryClient.invalidateQueries({ queryKey: ['market', 'rules'] })
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('market.admin.rulesSaveFail')),
  })

  const act = (l: MarketListing, action: 'approve' | 'reject' | 'delist') => {
    if (action === 'approve') {
      reviewMut.mutate({ id: l.id, action })
      return
    }
    const label = action === 'reject' ? t('market.admin.rejectReasonPrompt') : t('market.admin.delistReasonPrompt')
    const reason = window.prompt(label)
    if (!reason || !reason.trim()) return
    reviewMut.mutate({ id: l.id, action, reason: reason.trim() })
  }

  const statText = (l: MarketListing) => {
    const s = l.stats
    if (!s) return t('market.stats.noStats')
    const pct = (v: number) => `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
    return [
      `${t('market.stats.totalReturn')} ${pct(s.total_return_pct)}`,
      `${t('market.stats.maxDrawdown')} ${s.max_drawdown_pct.toFixed(2)}%`,
      `${t('market.stats.winRate')} ${s.win_rate.toFixed(1)}%`,
      `${t('market.stats.totalTrades')} ${s.total_trades}`,
      `${t('market.stats.runningDays')} ${s.running_days}`,
      `${t('market.stats.followers')} ${s.followers}`,
    ].join(' · ')
  }

  const inputCls = 'bg-quant-bg border border-quant-border rounded-lg px-2 py-1.5 text-xs w-24 focus:outline-none focus:border-quant-gold'

  return (
    <div className="space-y-4">
      {/* 考核规则配置（管理员可缩短考核期做演示） */}
      <div className="rounded-xl border border-quant-border bg-quant-bg-secondary p-4">
        <div className="flex items-center gap-2 flex-wrap">
          <ShieldCheck className="w-4 h-4 text-quant-gold" />
          <span className="text-sm font-medium">{t('market.admin.rulesTitle')}</span>
          <span className="text-xs text-muted-foreground">
            {rules &&
              t('market.admin.rulesCurrent')
                .replace('{days}', String(rules.min_days))
                .replace('{trades}', String(rules.min_trades))
                .replace('{dd}', String(rules.max_drawdown_pct))}
          </span>
          <span className="flex-1" />
          {rulesForm == null ? (
            <button
              onClick={() =>
                setRulesForm({
                  min_days: String(rules?.min_days ?? 30),
                  min_trades: String(rules?.min_trades ?? 10),
                  max_drawdown_pct: String(rules?.max_drawdown_pct ?? 50),
                })
              }
              className="px-3 py-1.5 rounded-lg border border-quant-border text-xs text-muted-foreground hover:text-foreground"
            >
              {t('market.admin.editRules')}
            </button>
          ) : (
            <div className="flex items-center gap-2">
              <input value={rulesForm.min_days} onChange={(e) => setRulesForm({ ...rulesForm, min_days: e.target.value })} className={inputCls} aria-label="min days" type="number" />
              <input value={rulesForm.min_trades} onChange={(e) => setRulesForm({ ...rulesForm, min_trades: e.target.value })} className={inputCls} aria-label="min trades" type="number" />
              <input value={rulesForm.max_drawdown_pct} onChange={(e) => setRulesForm({ ...rulesForm, max_drawdown_pct: e.target.value })} className={inputCls} aria-label="max drawdown" type="number" />
              <button
                onClick={() => rulesMut.mutate()}
                disabled={rulesMut.isPending}
                className="px-3 py-1.5 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 disabled:opacity-50"
              >
                {t('market.admin.save')}
              </button>
              <button onClick={() => setRulesForm(null)} className="px-3 py-1.5 rounded-lg border border-quant-border text-xs text-muted-foreground">
                {t('market.admin.cancel')}
              </button>
            </div>
          )}
        </div>
      </div>

      {/* 状态切换 */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit flex-wrap">
        {QUEUE_TABS.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setStatus(tab.key)}
            className={cn(
              'px-3 py-1.5 rounded text-xs font-medium transition-colors',
              status === tab.key ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground'
            )}
          >
            {t(tab.labelKey)}
          </button>
        ))}
      </div>

      {/* 审核队列 */}
      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : listings.length === 0 ? (
        <div className="text-center py-10 text-muted-foreground text-sm">{t('market.admin.queueEmpty')}</div>
      ) : (
        <div className="space-y-2">
          {listings.map((l) => (
            <div key={l.id} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-3.5">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-bold text-xs text-foreground">{l.name}</span>
                <span className="text-[10px] text-muted-foreground">
                  #{l.id} · {t('market.admin.author')} {l.author_user_id} · {l.kind === 'signal' ? t('market.board.kindSignal') : t('market.board.kindRobot')}
                </span>
                <span className="flex-1" />
                {l.status === 'pending_review' && (
                  <>
                    <button
                      onClick={() => act(l, 'approve')}
                      disabled={reviewMut.isPending}
                      className="px-3 py-1 rounded bg-quant-green text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                    >
                      {t('market.admin.approve')}
                    </button>
                    <button
                      onClick={() => act(l, 'reject')}
                      disabled={reviewMut.isPending}
                      className="px-3 py-1 rounded bg-quant-red text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                    >
                      {t('market.admin.reject')}
                    </button>
                  </>
                )}
                {l.status === 'listed' && (
                  <button
                    onClick={() => act(l, 'delist')}
                    disabled={reviewMut.isPending}
                    className="px-3 py-1 rounded bg-quant-red text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                  >
                    {t('market.admin.delist')}
                  </button>
                )}
              </div>
              <div className="mt-1.5 text-[10px] text-muted-foreground">{statText(l)}</div>
              {l.progress && (l.status === 'probation' || l.status === 'pending_review') && (
                <div className="mt-1 text-[10px]">
                  <span className={l.progress.days_ok ? 'text-quant-green' : 'text-muted-foreground'}>
                    {t('market.probation.days')} {l.progress.days_elapsed}/{l.progress.min_days}
                  </span>
                  <span className="mx-1.5 text-muted-foreground">·</span>
                  <span className={l.progress.trades_ok ? 'text-quant-green' : 'text-muted-foreground'}>
                    {t('market.stats.totalTrades')} {l.progress.trades_in_window}/{l.progress.min_trades}
                  </span>
                  <span className="mx-1.5 text-muted-foreground">·</span>
                  <span className={l.progress.drawdown_ok ? 'text-quant-green' : 'text-quant-red'}>
                    {t('market.probation.drawdownLimit')} {l.progress.max_drawdown_pct.toFixed(1)}%/{l.progress.max_drawdown_limit}%
                  </span>
                </div>
              )}
              {l.status === 'rejected' && l.reject_reason && (
                <div className="mt-1.5 text-[10px] text-quant-red">{t('market.status.rejectReason')}：{l.reject_reason}</div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default AdminMarketReview
