import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ClipboardList, Plus, XCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { aiBotApi, marketListingApi, type MarketListing, type MarketListingStatus } from '@/lib/api'
import { useI18n } from '@/i18n'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { Button } from '@/components/ui/Button'
import { toast } from '@/lib/useToast'

const STATUS_META: Record<MarketListingStatus, { labelKey: string; fallback: string; cls: string }> = {
  draft: { labelKey: 'market.statusDraft', fallback: '草稿', cls: 'bg-quant-bg text-muted-foreground border-quant-border' },
  probation: { labelKey: 'market.statusProbation', fallback: '考核中', cls: 'bg-blue-500/15 text-blue-400 border-blue-500/40' },
  pending_review: { labelKey: 'market.statusPendingReview', fallback: '待审核', cls: 'bg-quant-gold/15 text-quant-gold border-quant-gold/40' },
  listed: { labelKey: 'market.statusListed', fallback: '已上架', cls: 'bg-quant-green/15 text-quant-green border-quant-green/40' },
  rejected: { labelKey: 'market.statusRejected', fallback: '已驳回', cls: 'bg-quant-red/15 text-quant-red border-quant-red/40' },
  delisted: { labelKey: 'market.statusDelisted', fallback: '已下架', cls: 'bg-quant-bg text-muted-foreground border-quant-border' },
}

function StatusBadge({ status }: { status: MarketListingStatus }) {
  const { t } = useI18n()
  const meta = STATUS_META[status] ?? STATUS_META.draft
  return (
    <span className={cn('px-1.5 py-0.5 rounded border text-[10px] font-semibold shrink-0', meta.cls)}>
      {t(meta.labelKey, meta.fallback)}
    </span>
  )
}

/** 考核进度条：天数 / 交易数 / 回撤红线三项达标判定。 */
function ProbationProgressBar({ listing }: { listing: MarketListing }) {
  const { t } = useI18n()
  const p = listing.progress
  if (!p) return null
  const daysPct = Math.min(100, p.min_days > 0 ? (p.days_elapsed / p.min_days) * 100 : 100)
  const tradesPct = Math.min(100, p.min_trades > 0 ? (p.trades_in_window / p.min_trades) * 100 : 100)
  const ddOK = p.drawdown_ok
  return (
    <div className="mt-2 space-y-1.5">
      <div>
        <div className="flex justify-between text-[10px] text-muted-foreground mb-0.5">
          <span>{t('market.probationDays', '考核天数')}</span>
          <span>
            {t('market.dayN', '第')} {p.days_elapsed}/{p.min_days} {t('market.day', '天')}
            {p.remaining_days > 0 && ` · ${t('market.remainingDays', '还差')} ${p.remaining_days} ${t('market.day', '天')}`}
          </span>
        </div>
        <div className="h-1.5 rounded-full bg-quant-bg overflow-hidden">
          <div className={cn('h-full rounded-full', p.days_ok ? 'bg-quant-green' : 'bg-blue-400')} style={{ width: `${daysPct}%` }} />
        </div>
      </div>
      <div>
        <div className="flex justify-between text-[10px] text-muted-foreground mb-0.5">
          <span>{t('market.probationTrades', '交易数')}</span>
          <span>
            {p.trades_in_window}/{p.min_trades}
            {p.remaining_trades > 0 && ` · ${t('market.remainingTrades', '还差')} ${p.remaining_trades} ${t('market.tradeUnit', '笔')}`}
          </span>
        </div>
        <div className="h-1.5 rounded-full bg-quant-bg overflow-hidden">
          <div className={cn('h-full rounded-full', p.trades_ok ? 'bg-quant-green' : 'bg-blue-400')} style={{ width: `${tradesPct}%` }} />
        </div>
      </div>
      <div className="flex justify-between text-[10px]">
        <span className="text-muted-foreground">{t('market.drawdownLimit', '回撤红线')}</span>
        <span className={ddOK ? 'text-quant-green' : 'text-quant-red'}>
          {p.max_drawdown_pct.toFixed(2)}% / {p.max_drawdown_limit}%
          {ddOK ? ` · ${t('market.ok', '达标')}` : ` · ${t('market.breached', '越线')}`}
        </span>
      </div>
    </div>
  )
}

/** 我的上架：提交考核 → 进度跟踪 → 审核状态/驳回原因。 */
export function MyListings() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [showForm, setShowForm] = useState(false)
  const [instanceID, setInstanceID] = useState('')
  const [name, setName] = useState('')
  const [feeModel, setFeeModel] = useState<'free' | 'monthly' | 'profit_share'>('free')
  const [feePercent, setFeePercent] = useState('20')
  const [monthlyFee, setMonthlyFee] = useState('10')

  const { data: listings = [], isLoading } = useQuery({
    queryKey: ['market', 'my-listings'],
    queryFn: () => marketListingApi.myListings(),
    refetchInterval: 30_000,
  })
  const { data: rules } = useQuery({ queryKey: ['market', 'rules'], queryFn: () => marketListingApi.rules(), staleTime: 60_000 })
  const { data: instances = [] } = useQuery({ queryKey: ['ai-bots', 'instances'], queryFn: () => aiBotApi.list(), staleTime: 30_000 })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['market'] })

  const createMut = useMutation({
    mutationFn: () =>
      marketListingApi.create({
        bot_instance_id: instanceID,
        kind: 'robot',
        name: name.trim(),
        fee_model: feeModel,
        fee_percent: feeModel === 'profit_share' ? parseFloat(feePercent) || 0 : 0,
        monthly_fee: feeModel === 'monthly' ? parseFloat(monthlyFee) || 0 : 0,
        submit: true,
      }),
    onSuccess: async () => {
      toast('success', t('market.submitOk', '已提交考核'))
      setShowForm(false)
      setInstanceID('')
      setName('')
      await invalidate()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('market.submitFail', '提交失败')),
  })

  const actionMut = useMutation({
    mutationFn: ({ id, action }: { id: string; action: 'submit' | 'cancel' }) =>
      action === 'submit' ? marketListingApi.submit(id) : marketListingApi.cancel(id),
    onSuccess: async (_, v) => {
      toast('success', v.action === 'submit' ? t('market.submitOk', '已提交考核') : t('market.cancelOk', '已撤回考核'))
      await invalidate()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('market.actionFail', '操作失败')),
  })

  const listedInstanceIDs = new Set(listings.filter((l) => l.status === 'probation' || l.status === 'pending_review' || l.status === 'listed').map((l) => l.bot_instance_id))
  const candidateInstances = instances.filter((i) => !listedInstanceIDs.has(i.id))

  const inputCls =
    'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-xs text-muted-foreground">
          {rules && t('market.rulesHint', `考核规则：≥${rules.min_days} 天 · ≥${rules.min_trades} 笔交易 · 回撤 <${rules.max_drawdown_pct}%`)}
        </span>
        <span className="flex-1" />
        <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setShowForm((v) => !v)}>
          {t('market.submitProbation', '提交考核')}
        </Button>
      </div>

      {showForm && (
        <div className="bg-quant-card border border-quant-border rounded-xl p-4 space-y-3">
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">{t('market.selectInstance', '选择考核实例（paper/实盘均可）')}</label>
            <select value={instanceID} onChange={(e) => setInstanceID(e.target.value)} className={inputCls}>
              <option value="">{t('market.selectInstancePlaceholder', '— 选择 AI 机器人实例 —')}</option>
              {candidateInstances.map((i) => (
                <option key={i.id} value={i.id}>
                  {i.name} · {i.symbol} · {i.execution_mode}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">{t('market.listingName', '条目名称')}</label>
            <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} placeholder={t('market.listingNamePh', '展示在市场卡片上的名称')} />
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div>
              <label className="text-[11px] text-muted-foreground mb-1 block">{t('market.feeModel', '付费模式')}</label>
              <select value={feeModel} onChange={(e) => setFeeModel(e.target.value as typeof feeModel)} className={inputCls}>
                <option value="free">{t('market.free', '免费')}</option>
                <option value="monthly">{t('market.monthlyFee', '月费')}</option>
                <option value="profit_share">{t('market.profitShare', '利润分成')}</option>
              </select>
            </div>
            {feeModel === 'monthly' && (
              <div>
                <label className="text-[11px] text-muted-foreground mb-1 block">{t('market.monthlyFeeAmount', '月费 (USDT)')}</label>
                <input type="number" value={monthlyFee} onChange={(e) => setMonthlyFee(e.target.value)} className={inputCls} />
              </div>
            )}
            {feeModel === 'profit_share' && (
              <div>
                <label className="text-[11px] text-muted-foreground mb-1 block">{t('market.feePercent', '分成比例 (%)')}</label>
                <input type="number" value={feePercent} onChange={(e) => setFeePercent(e.target.value)} className={inputCls} />
              </div>
            )}
          </div>
          <Button
            variant="primary"
            className="w-full"
            isLoading={createMut.isPending}
            onClick={() => {
              if (!instanceID) return toast('warning', t('market.needInstance', '请选择考核实例'))
              createMut.mutate()
            }}
          >
            {t('market.confirmSubmit', '提交并进入考核期')}
          </Button>
        </div>
      )}

      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : listings.length === 0 ? (
        <EmptyState
          icon={<ClipboardList className="w-8 h-8" />}
          title={t('market.myEmptyTitle', '暂无上架条目')}
          description={t('market.myEmptyDesc', '选择一个 AI 机器人实例提交考核，达标并通过审核后即可在市场出售')}
        />
      ) : (
        <div className="space-y-2">
          {listings.map((l) => (
            <div key={l.id} className="bg-quant-card border border-quant-border rounded-xl p-3.5">
              <div className="flex items-center gap-2">
                <span className="font-bold text-xs text-foreground truncate">{l.name}</span>
                <StatusBadge status={l.status} />
                <span className="flex-1" />
                {(l.status === 'draft' || l.status === 'rejected' || l.status === 'delisted') && (
                  <button
                    onClick={() => actionMut.mutate({ id: l.id, action: 'submit' })}
                    className="px-2.5 py-1 rounded bg-quant-gold text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                    disabled={actionMut.isPending}
                  >
                    {l.status === 'draft' ? t('market.submitProbation', '提交考核') : t('market.resubmit', '重新提交考核')}
                  </button>
                )}
                {l.status === 'probation' && (
                  <button
                    onClick={() => actionMut.mutate({ id: l.id, action: 'cancel' })}
                    className="inline-flex items-center gap-1 px-2.5 py-1 rounded border border-quant-border text-[11px] text-muted-foreground hover:text-foreground disabled:opacity-50"
                    disabled={actionMut.isPending}
                  >
                    <XCircle className="w-3 h-3" />
                    {t('market.cancelProbation', '撤回考核')}
                  </button>
                )}
              </div>
              {l.status === 'probation' && <ProbationProgressBar listing={l} />}
              {l.status === 'rejected' && l.reject_reason && (
                <div className="mt-2 text-[11px] text-quant-red bg-quant-red/10 border border-quant-red/20 rounded-lg px-2.5 py-1.5">
                  {t('market.rejectReason', '驳回原因')}：{l.reject_reason}
                </div>
              )}
              {l.status === 'delisted' && l.delist_reason && (
                <div className="mt-2 text-[11px] text-muted-foreground bg-quant-bg border border-quant-border rounded-lg px-2.5 py-1.5">
                  {t('market.delistReason', '下架原因')}：{l.delist_reason}
                </div>
              )}
              {l.status === 'listed' && l.stats && (
                <div className="mt-2 flex gap-3 text-[10px] text-muted-foreground flex-wrap">
                  <span>{t('market.monthlyReturn', '月均收益')} {l.stats.monthly_return_pct >= 0 ? '+' : ''}{l.stats.monthly_return_pct.toFixed(2)}%</span>
                  <span>{t('market.winRate', '胜率')} {l.stats.win_rate.toFixed(1)}%</span>
                  <span>{t('market.followers', '跟踪')} {l.stats.followers}</span>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default MyListings
