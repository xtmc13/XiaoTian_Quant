import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Loader2, X } from 'lucide-react'
import { marketListingApi, type MarketListing, type MarketSubscribeResult } from '@/lib/api'
import { useI18n } from '@/i18n'
import { useToastStore } from '@/stores/toastStore'

/**
 * 市场条目月费订阅弹窗（迁移 0059 扣费链路）：展示金额（条目快照，服务端权威）、
 * 链选择，创建 pending_payment 订阅 + billing 订单后展示收款地址/金额/有效期，
 * 引导到 Billing 页完成转账哈希提交与链上核验进度。
 * free/profit_share 条目不走此弹窗（详情页确认框直订）。
 */

const CHAINS = [
  { value: 'TRC20', label: 'TRC20 (Tron)' },
  { value: 'BEP20', label: 'BEP20 (BNB Chain)' },
  { value: 'ERC20', label: 'ERC20 (Ethereum)' },
  { value: 'SOL', label: 'SOL (Solana)' },
] as const

function fmtTime(sec?: number): string {
  if (!sec) return '-'
  return new Date(sec * 1000).toLocaleString()
}

export function MarketSubscribeModal({
  listing,
  onClose,
  onSubscribed,
}: {
  listing: MarketListing
  onClose: () => void
  onSubscribed: () => void
}) {
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const addToast = useToastStore((s) => s.addToast)
  const [chain, setChain] = useState<string>('TRC20')
  const [result, setResult] = useState<MarketSubscribeResult | null>(null)

  const mut = useMutation({
    mutationFn: () => marketListingApi.subscribe(listing.id, { chain }),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['market', 'my-subscriptions'] })
      if (res?.order) {
        setResult(res)
        addToast({ type: 'success', message: t('market.detail.orderCreated'), duration: 5000 })
      } else {
        addToast({ type: 'success', message: t('market.detail.subscribeOk'), duration: 3000 })
        onSubscribed()
        onClose()
      }
    },
    onError: (e: Error) =>
      addToast({ type: 'error', message: e.message || t('market.detail.subscribeFail'), duration: 5000 }),
  })

  const order = result?.order

  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      onClick={onClose}
    >
      <div
        className="bg-quant-card border border-quant-border rounded-xl shadow-xl w-[440px] max-w-[90vw] p-4 space-y-3"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-bold text-foreground">{t('market.detail.payTitle')}</h3>
          <button onClick={onClose} className="p-1 rounded text-muted-foreground hover:text-foreground" aria-label="close">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="text-xs text-muted-foreground">
          {t('market.detail.payDesc')
            .replace('{name}', listing.name)
            .replace('{fee}', String(listing.monthly_fee))}
        </div>

        {!order && (
          <div>
            <label className="text-[10px] text-muted-foreground mb-1 block">{t('market.detail.payChain')}</label>
            <select
              value={chain}
              onChange={(e) => setChain(e.target.value)}
              className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold"
            >
              {CHAINS.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
        )}

        {/* 计费订单支付信息（创建后展示，金额/地址由服务端返回） */}
        {order && (
          <div className="rounded-lg border border-quant-gold/30 bg-quant-gold/5 p-2.5 text-xs space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('market.detail.orderAmount')}</span>
              <span className="font-bold text-quant-gold" data-testid="market-sub-order-amount">
                {(order.amount_usdt / 1e6).toFixed(2)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground shrink-0">{t('market.detail.orderAddress')}</span>
              <span className="font-mono text-[10px] text-foreground break-all text-right">{order.address}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('market.detail.orderExpiresAt')}</span>
              <span className="text-foreground">{fmtTime(result?.expires_at)}</span>
            </div>
            {result?.period_days != null && (
              <div className="text-[10px] text-muted-foreground">
                {t('market.detail.periodDays').replace('{n}', String(result.period_days))}
              </div>
            )}
          </div>
        )}

        {order ? (
          <button
            onClick={() => navigate('/billing')}
            className="w-full py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity"
          >
            {t('market.detail.goBilling')}
          </button>
        ) : (
          <button
            onClick={() => mut.mutate()}
            disabled={mut.isPending}
            className="w-full py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity disabled:opacity-50 inline-flex items-center justify-center gap-1.5"
          >
            {mut.isPending && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            {t('market.detail.confirmPay')}
          </button>
        )}
      </div>
    </div>
  )
}

export default MarketSubscribeModal
