import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, ShieldCheck, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  socialApi,
  type SocialMarketProvider,
  type SocialSubscribeResult,
} from '@/lib/api'
import { useI18n } from '@/i18n'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import { useToastStore } from '@/stores/toastStore'
import { useAuthStore } from '@/stores/authStore'

/**
 * 市场 Provider 双轨订阅区（GET /social/providers 的 market_providers）。
 * subscribe / switch_track / mySubscription 的 :id 一律用 db_id（库内 id），
 * 与 follow/unfollow 的引擎 offset id 不同。挂载：SocialTrading 信号源 tab。
 */

const CHAINS = [
  { value: 'trc20', label: 'TRC20 (Tron)' },
  { value: 'bep20', label: 'BEP20 (BNB Chain)' },
  { value: 'erc20', label: 'ERC20 (Ethereum)' },
  { value: 'sol', label: 'SOL (Solana)' },
] as const

type TFunc = (key: string, fallback?: string) => string

export function trackOptionLabel(t: TFunc, track: string, p: SocialMarketProvider): string {
  return track === 'subscription'
    ? t('social.track.trackSubscription').replace('{fee}', String(p.monthly_fee))
    : t('social.track.trackProfitShare').replace('{pct}', String(p.profit_share_pct ?? 0))
}

function trackName(t: TFunc, track?: string): string {
  return t(`social.track.trackName.${track || ''}`, track || '-')
}

function fmtMs(ms?: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN')
}

function fmtDateMs(ms?: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleDateString('zh-CN')
}

/* ── 订阅 / 切轨弹窗 ── */
export function SubscribeTrackModal({
  provider,
  onClose,
}: {
  provider: SocialMarketProvider
  onClose: () => void
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const addToast = useToastStore((s) => s.addToast)
  const { confirm, Dialog } = useConfirmDialog()
  const tracks = provider.available_tracks?.length ? provider.available_tracks : ['profit_share']
  const [track, setTrack] = useState(tracks[0])
  const [chain, setChain] = useState<string>('trc20')
  const [order, setOrder] = useState<SocialSubscribeResult | null>(null)

  const subQ = useQuery({
    queryKey: ['social-subscription', provider.db_id],
    queryFn: () => socialApi.mySubscription(provider.db_id),
  })
  const sub = subQ.data?.subscription ?? null
  const subExpired = !!sub && sub.track_expires_at > 0 && sub.track_expires_at < Date.now()
  const isSwitch = !!sub && sub.track !== track
  const isRenew = !!sub && sub.track === track && track === 'subscription' && subExpired
  const sameActive = !!sub && sub.track === track && !isRenew

  const mut = useMutation({
    mutationFn: (action: 'subscribe' | 'switch') =>
      action === 'switch'
        ? socialApi.switchTrack(provider.db_id, { track, chain })
        : socialApi.subscribe(provider.db_id, { track, chain }),
    onSuccess: (res, action) => {
      queryClient.invalidateQueries({ queryKey: ['social-subscription', provider.db_id] })
      if (res?.order) {
        setOrder(res)
        addToast({ type: 'success', message: t('social.track.orderCreated'), duration: 5000 })
      } else {
        addToast({
          type: 'success',
          message: action === 'switch' ? t('social.track.switchOk') : t('social.track.subscribeOk'),
          duration: 3000,
        })
      }
    },
    onError: (e: Error) =>
      addToast({ type: 'error', message: e.message || t('social.track.actionFail'), duration: 5000 }),
  })

  const submit = async () => {
    if (sameActive) return
    if (isSwitch) {
      const ok = await confirm({
        title: t('social.track.switchConfirmTitle'),
        message: t('social.track.switchConfirmMsg').replace('{track}', trackName(t, track)),
        confirmText: t('social.track.switch'),
      })
      if (!ok) return
      mut.mutate('switch')
      return
    }
    mut.mutate('subscribe')
  }

  const submitLabel = isSwitch
    ? t('social.track.switch')
    : isRenew
      ? t('social.track.renew')
      : t('social.track.confirm')

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
          <h3 className="text-sm font-bold text-foreground">
            {t('social.track.modalTitle').replace('{name}', provider.name)}
          </h3>
          <button onClick={onClose} className="p-1 rounded text-muted-foreground hover:text-foreground">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* 当前订阅状态 */}
        {subQ.isLoading ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
          </div>
        ) : sub ? (
          <div className="rounded-lg bg-quant-bg-secondary p-2.5 text-xs space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('social.track.currentTrack')}</span>
              <span className="font-medium text-foreground">{trackName(t, sub.track)}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('social.track.status')}</span>
              <span className={cn('font-medium', sub.status === 'active' ? 'text-quant-green' : 'text-muted-foreground')}>
                {sub.status === 'active' ? t('social.track.statusActive') : t('social.track.statusCancelled')}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('social.track.expiresAt')}</span>
              <span className={cn('font-medium', subExpired ? 'text-quant-red' : 'text-foreground')}>
                {sub.track_expires_at > 0 ? fmtMs(sub.track_expires_at) : t('social.track.noExpiry')}
              </span>
            </div>
          </div>
        ) : null}

        {/* 轨道选择 */}
        <div>
          <label className="text-[10px] text-muted-foreground mb-1 block">{t('social.track.chooseTrack')}</label>
          <div className="space-y-1.5">
            {tracks.map((tr) => (
              <label
                key={tr}
                className={cn(
                  'flex items-center gap-2 rounded-lg border px-3 py-2 text-xs cursor-pointer transition-colors',
                  track === tr
                    ? 'border-quant-gold bg-quant-gold/5 text-foreground'
                    : 'border-quant-border text-muted-foreground hover:text-foreground'
                )}
              >
                <input
                  type="radio"
                  name="track"
                  value={tr}
                  checked={track === tr}
                  onChange={() => setTrack(tr)}
                  className="accent-quant-gold"
                />
                {trackOptionLabel(t, tr, provider)}
              </label>
            ))}
          </div>
        </div>

        {/* 支付链（仅订阅轨） */}
        {track === 'subscription' && (
          <div>
            <label className="text-[10px] text-muted-foreground mb-1 block">{t('social.track.chain')}</label>
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

        {/* 计费订单支付信息 */}
        {order?.order && (
          <div className="rounded-lg border border-quant-gold/30 bg-quant-gold/5 p-2.5 text-xs space-y-1">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('social.track.orderAmount')}</span>
              <span className="font-bold text-quant-gold">{(order.order.amount_usdt / 1e6).toFixed(2)}</span>
            </div>
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground shrink-0">{t('social.track.orderAddress')}</span>
              <span className="font-mono text-[10px] text-foreground break-all text-right">{order.order.address}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">{t('social.track.orderExpiresAt')}</span>
              <span className="text-foreground">{order.expires_at ? fmtMs(order.expires_at * 1000) : '-'}</span>
            </div>
            {order.period_days != null && (
              <div className="text-[10px] text-muted-foreground">
                {t('social.track.periodDays').replace('{n}', String(order.period_days))}
              </div>
            )}
          </div>
        )}

        <button
          onClick={submit}
          disabled={mut.isPending || sameActive || subQ.isLoading}
          className="w-full py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity disabled:opacity-50 inline-flex items-center justify-center gap-1.5"
        >
          {mut.isPending && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
          {submitLabel}
        </button>
      </div>
      <Dialog />
    </div>
  )
}

/* ── 市场 Provider 卡片 ── */
function MarketProviderCard({
  provider,
  onOpen,
}: {
  provider: SocialMarketProvider
  onOpen: () => void
}) {
  const { t } = useI18n()
  const subQ = useQuery({
    queryKey: ['social-subscription', provider.db_id],
    queryFn: () => socialApi.mySubscription(provider.db_id),
    staleTime: 30_000,
  })
  const sub = subQ.data?.subscription ?? null

  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-4 shadow-sm">
      <div className="flex items-center justify-between mb-2">
        <div className="min-w-0">
          <div className="text-sm font-semibold text-foreground truncate">{provider.name}</div>
          <div className="text-[10px] text-muted-foreground">
            {provider.follower_count} {t('social.track.followers')}
          </div>
        </div>
        {provider.monthly_fee > 0 && (
          <span className="text-[10px] px-2 py-0.5 rounded bg-quant-gold/10 text-quant-gold border border-quant-gold/20">
            ${provider.monthly_fee}/月
          </span>
        )}
      </div>
      {provider.description && (
        <p className="text-[11px] text-muted-foreground mb-2 line-clamp-2">{provider.description}</p>
      )}
      <div className="flex flex-wrap gap-1 mb-3">
        {(provider.available_tracks || []).map((tr) => (
          <span key={tr} className="px-1.5 py-0.5 rounded text-[10px] bg-blue-500/10 text-blue-400">
            {trackOptionLabel(t, tr, provider)}
          </span>
        ))}
      </div>
      {sub && (
        <div className="mb-2 text-[10px] text-muted-foreground">
          {t('social.track.currentTrack')}:
          <span className="ml-1 text-quant-green font-medium">{trackName(t, sub.track)}</span>
          {sub.track_expires_at > 0 && <span className="ml-1">· {fmtDateMs(sub.track_expires_at)}</span>}
        </div>
      )}
      <button
        onClick={onOpen}
        className="w-full py-1.5 rounded-lg text-xs font-medium bg-quant-gold text-white hover:opacity-90 transition-opacity"
      >
        {sub ? t('social.track.manage') : t('social.track.subscribe')}
      </button>
    </div>
  )
}

/* ── 区块入口 ── */
export function MarketProviders() {
  const { data: providers = [], isLoading } = useQuery({
    queryKey: ['social-market-providers'],
    queryFn: () => socialApi.marketProviders(),
    staleTime: 60_000,
  })
  const [selected, setSelected] = useState<SocialMarketProvider | null>(null)

  if (isLoading || providers.length === 0) return null

  return (
    <div className="space-y-2">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
        {providers.map((p) => (
          <MarketProviderCard key={p.db_id} provider={p} onOpen={() => setSelected(p)} />
        ))}
      </div>
      {selected && <SubscribeTrackModal provider={selected} onClose={() => setSelected(null)} />}
    </div>
  )
}

/* ── 管理员入驻审核（无待审列表端点，按申请 ID 单条操作）── */
export function AdminProviderReview() {
  const { t } = useI18n()
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const addToast = useToastStore((s) => s.addToast)
  const { confirm, prompt, Dialog } = useConfirmDialog()
  const [idInput, setIdInput] = useState('')

  const mut = useMutation({
    mutationFn: ({ id, action, note }: { id: number; action: 'approve' | 'reject'; note?: string }) =>
      action === 'approve' ? socialApi.approveProvider(id) : socialApi.rejectProvider(id, note || ''),
    onSuccess: (_, v) =>
      addToast({
        type: 'success',
        message: v.action === 'approve' ? t('social.adminReview.approveOk') : t('social.adminReview.rejectOk'),
        duration: 3000,
      }),
    onError: (e: Error) =>
      addToast({ type: 'error', message: e.message || t('social.adminReview.actionFail'), duration: 5000 }),
  })

  if (!isAdmin) return null

  const parseId = (): number | null => {
    const id = Number(idInput.trim())
    if (!Number.isInteger(id) || id <= 0) {
      addToast({ type: 'warning', message: t('social.adminReview.invalidId'), duration: 3000 })
      return null
    }
    return id
  }

  const approve = async () => {
    const id = parseId()
    if (id == null) return
    const ok = await confirm({
      title: t('social.adminReview.approveConfirmTitle'),
      message: t('social.adminReview.approveConfirmMsg').replace('{id}', String(id)),
      confirmText: t('social.adminReview.approve'),
    })
    if (ok) mut.mutate({ id, action: 'approve' })
  }

  const reject = async () => {
    const id = parseId()
    if (id == null) return
    const note = await prompt({
      title: t('social.adminReview.rejectTitle'),
      inputLabel: t('social.adminReview.rejectNoteLabel'),
      confirmText: t('social.adminReview.reject'),
      variant: 'danger',
    })
    if (note === null) return
    mut.mutate({ id, action: 'reject', note })
  }

  return (
    <div className="rounded-xl border border-quant-border bg-quant-card p-4 space-y-2">
      <div className="flex items-center gap-2">
        <ShieldCheck className="w-4 h-4 text-quant-gold" />
        <span className="text-sm font-semibold text-foreground">{t('social.adminReview.title')}</span>
        <span className="text-[10px] text-muted-foreground">{t('social.adminReview.note')}</span>
      </div>
      <div className="flex items-center gap-2">
        <input
          value={idInput}
          onChange={(e) => setIdInput(e.target.value)}
          placeholder={t('social.adminReview.providerId')}
          inputMode="numeric"
          className="w-40 rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold"
        />
        <button
          onClick={approve}
          disabled={mut.isPending}
          className="px-3 py-1.5 rounded bg-quant-green text-white text-xs font-medium hover:opacity-90 disabled:opacity-50 inline-flex items-center gap-1"
        >
          {mut.isPending && <Loader2 className="w-3 h-3 animate-spin" />}
          {t('social.adminReview.approve')}
        </button>
        <button
          onClick={reject}
          disabled={mut.isPending}
          className="px-3 py-1.5 rounded bg-quant-red text-white text-xs font-medium hover:opacity-90 disabled:opacity-50"
        >
          {t('social.adminReview.reject')}
        </button>
      </div>
      <Dialog />
    </div>
  )
}

export default MarketProviders
