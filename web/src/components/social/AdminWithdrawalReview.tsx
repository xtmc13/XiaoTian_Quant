import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowDownToLine } from 'lucide-react'
import { cn } from '@/lib/utils'
import { socialMarketApi, type SocialWithdrawal } from '@/lib/api'
import { useI18n } from '@/i18n'
import { Skeleton } from '@/components/ui/Skeleton'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { toast } from '@/lib/useToast'

type WithdrawalStatus = SocialWithdrawal['status']

const QUEUE_TABS: { key: WithdrawalStatus; labelKey: string }[] = [
  { key: 'pending', labelKey: 'admin.withdrawals.statusPending' },
  { key: 'paid', labelKey: 'admin.withdrawals.statusPaid' },
  { key: 'rejected', labelKey: 'admin.withdrawals.statusRejected' },
]

const STATUS_LABEL_KEY: Record<WithdrawalStatus, string> = {
  pending: 'admin.withdrawals.statusPending',
  paid: 'admin.withdrawals.statusPaid',
  rejected: 'admin.withdrawals.statusRejected',
}

const STATUS_CLS: Record<WithdrawalStatus, string> = {
  pending: 'bg-quant-gold/15 text-quant-gold border-quant-gold/40',
  paid: 'bg-quant-green/15 text-quant-green border-quant-green/40',
  rejected: 'bg-quant-red/15 text-quant-red border-quant-red/40',
}

function fmtMs(ms?: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN')
}

/** 管理员提现打款队列（嵌入 UserManage 的「提现审核」tab）。打款需填链上 tx_hash。 */
export function AdminWithdrawalReview() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const { prompt, Dialog } = useConfirmDialog()
  const [status, setStatus] = useState<WithdrawalStatus>('pending')

  const { data: withdrawals = [], isLoading } = useQuery({
    queryKey: ['admin-social-withdrawals', status],
    queryFn: () => socialMarketApi.adminWithdrawals(status),
    refetchInterval: 30_000,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin-social-withdrawals'] })

  const payMut = useMutation({
    mutationFn: ({ id, txHash }: { id: string; txHash: string }) => socialMarketApi.adminPayWithdrawal(id, txHash),
    onSuccess: async () => {
      toast('success', t('admin.withdrawals.payOk'))
      await invalidate()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('admin.withdrawals.actionFail')),
  })

  const rejectMut = useMutation({
    mutationFn: ({ id, note }: { id: string; note: string }) => socialMarketApi.adminRejectWithdrawal(id, note),
    onSuccess: async () => {
      toast('success', t('admin.withdrawals.rejectOk'))
      await invalidate()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : t('admin.withdrawals.actionFail')),
  })

  const pay = async (w: SocialWithdrawal) => {
    const txHash = await prompt({
      title: `${t('admin.withdrawals.payTitle')} · ${w.amount.toFixed(2)} USDT (${w.chain.toUpperCase()})`,
      inputLabel: t('admin.withdrawals.payTxLabel'),
      confirmText: t('admin.withdrawals.pay'),
    })
    if (!txHash || !txHash.trim()) return
    payMut.mutate({ id: w.id, txHash: txHash.trim() })
  }

  const reject = async (w: SocialWithdrawal) => {
    const note = await prompt({
      title: t('admin.withdrawals.rejectTitle'),
      inputLabel: t('admin.withdrawals.rejectNoteLabel'),
      confirmText: t('admin.withdrawals.reject'),
      variant: 'danger',
    })
    if (note === null || !note.trim()) return
    rejectMut.mutate({ id: w.id, note: note.trim() })
  }

  const pending = payMut.isPending || rejectMut.isPending

  return (
    <div className="space-y-4">
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

      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : withdrawals.length === 0 ? (
        <div className="text-center py-10 text-muted-foreground text-sm">{t('admin.withdrawals.queueEmpty')}</div>
      ) : (
        <div className="space-y-2">
          {withdrawals.map((w) => (
            <div key={w.id} className="rounded-xl border border-quant-border bg-quant-bg-secondary p-3.5">
              <div className="flex items-center gap-2 flex-wrap">
                <ArrowDownToLine className="w-3.5 h-3.5 text-quant-gold shrink-0" />
                <span className="font-bold text-xs text-foreground">{w.amount.toFixed(2)} USDT</span>
                <span className="px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-400 text-[10px] uppercase">{w.chain}</span>
                <span className={cn('px-1.5 py-0.5 rounded border text-[10px] font-semibold', STATUS_CLS[w.status] ?? '')}>
                  {t(STATUS_LABEL_KEY[w.status] ?? 'admin.withdrawals.statusPending')}
                </span>
                <span className="text-[10px] text-muted-foreground">
                  #{w.id} · {t('admin.withdrawals.applicant')} {w.provider_user_id} · {fmtMs(w.created_at)}
                </span>
                <span className="flex-1" />
                {w.status === 'pending' && (
                  <>
                    <button
                      onClick={() => pay(w)}
                      disabled={pending}
                      className="px-3 py-1 rounded bg-quant-green text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                    >
                      {t('admin.withdrawals.pay')}
                    </button>
                    <button
                      onClick={() => reject(w)}
                      disabled={pending}
                      className="px-3 py-1 rounded bg-quant-red text-white text-[11px] font-medium hover:opacity-90 disabled:opacity-50"
                    >
                      {t('admin.withdrawals.reject')}
                    </button>
                  </>
                )}
              </div>
              <div className="mt-1.5 text-[10px] text-muted-foreground font-mono break-all">
                {t('admin.withdrawals.address')}: {w.address}
              </div>
              {w.tx_hash && (
                <div className="mt-1 text-[10px] text-muted-foreground font-mono break-all">
                  {t('admin.withdrawals.txHash')}: {w.tx_hash}
                </div>
              )}
              {w.status === 'rejected' && w.admin_note && (
                <div className="mt-1 text-[10px] text-quant-red">
                  {t('admin.withdrawals.reject')}: {w.admin_note}
                </div>
              )}
              {w.processed_at ? (
                <div className="mt-1 text-[10px] text-muted-foreground">
                  {t('admin.withdrawals.processedAt')}: {fmtMs(w.processed_at)}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      )}
      <Dialog />
    </div>
  )
}

export default AdminWithdrawalReview
