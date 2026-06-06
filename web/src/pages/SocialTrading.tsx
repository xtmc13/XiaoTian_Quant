import { useState, memo, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Users, Signal, TrendingUp, TrendingDown, Star, Plus, Trash2, Zap,
  ChevronUp, ChevronDown, Loader2, Bell, Settings, Copy, CheckCircle2,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { socialApi } from '@/lib/api'
import { useToastStore } from '@/stores/toastStore'
import { useAuthStore } from '@/stores/authStore'

interface Provider {
  provider_id: number
  total_signals: number
  win_count: number
  loss_count: number
  win_rate: number
  avg_return_pct: number
  sharpe_ratio: number
  max_drawdown_pct: number
  follower_count: number
  monthly_fee: number
  is_public: boolean
}

interface SignalItem {
  id: string
  provider_id: number
  provider_name: string
  symbol: string
  direction: string
  price: number
  stop_loss: number
  take_profit: number
  size: number
  confidence: number
  strategy: string
  reason: string
  timestamp: number
  expires_at: number
}

function useFollowerId(): number {
  const { user } = useAuthStore()
  return user?.id ?? 1
}

/**
 * ProviderCard — memoized signal-provider card.
 * Only re-renders when its own props change, not when the list filters/sorts.
 */
const ProviderCard = memo(function ProviderCard({
  provider,
  isFollowing,
  onToggleFollow,
  isPending,
}: {
  provider: Provider
  isFollowing: boolean
  onToggleFollow: (id: number) => void
  isPending: boolean
}) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-4 shadow-sm">
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-full bg-quant-gold/10 flex items-center justify-center text-quant-gold text-xs font-bold">
            {provider.provider_id}
          </div>
          <div>
            <div className="text-sm font-semibold text-foreground">Provider #{provider.provider_id}</div>
            <div className="text-[10px] text-muted-foreground">{provider.total_signals} 信号 · {provider.follower_count} 关注</div>
          </div>
        </div>
        {provider.monthly_fee > 0 && (
          <span className="text-[10px] px-2 py-0.5 rounded bg-quant-gold/10 text-quant-gold border border-quant-gold/20">
            ${provider.monthly_fee}/月
          </span>
        )}
      </div>

      <div className="grid grid-cols-3 gap-2 mb-3">
        <div className="text-center p-2 bg-quant-bg-secondary rounded-lg">
          <div className={cn('text-sm font-bold', provider.win_rate >= 0.5 ? 'text-quant-green' : 'text-quant-red')}>
            {(provider.win_rate * 100).toFixed(1)}%
          </div>
          <div className="text-[9px] text-muted-foreground">胜率</div>
        </div>
        <div className="text-center p-2 bg-quant-bg-secondary rounded-lg">
          <div className="text-sm font-bold text-quant-green">{provider.avg_return_pct?.toFixed(2)}%</div>
          <div className="text-[9px] text-muted-foreground">平均收益</div>
        </div>
        <div className="text-center p-2 bg-quant-bg-secondary rounded-lg">
          <div className="text-sm font-bold text-quant-blue">{provider.sharpe_ratio?.toFixed(2)}</div>
          <div className="text-[9px] text-muted-foreground">Sharpe</div>
        </div>
      </div>

      <button
        onClick={() => onToggleFollow(provider.provider_id)}
        disabled={isPending}
        className={cn(
          'w-full py-1.5 rounded-lg text-xs font-medium transition-colors',
          isFollowing
            ? 'bg-quant-bg-secondary text-muted-foreground hover:text-quant-red'
            : 'bg-quant-gold text-white hover:opacity-90'
        )}
      >
        {isFollowing ? '已关注' : '关注'}
      </button>
    </div>
  )
})

/**
 * SignalCard — memoized trading-signal card.
 */
const SignalCard = memo(function SignalCard({ signal }: { signal: SignalItem }) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3 shadow-sm">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className={cn(
            'text-xs font-bold px-2 py-0.5 rounded',
            signal.direction === 'buy' ? 'bg-quant-green/10 text-quant-green' : 'bg-quant-red/10 text-quant-red'
          )}>
            {signal.direction === 'buy' ? '买入' : '卖出'}
          </span>
          <span className="text-sm font-semibold text-foreground">{signal.symbol}</span>
          <span className="text-[10px] text-muted-foreground">@{signal.price}</span>
        </div>
        <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
          <CheckCircle2 className="w-3 h-3" />
          {signal.confidence}%
        </div>
      </div>
      <div className="flex items-center gap-4 mt-2 text-[10px] text-muted-foreground">
        <span>止损: {signal.stop_loss || '--'}</span>
        <span>止盈: {signal.take_profit || '--'}</span>
        <span>仓位: {(signal.size * 100).toFixed(1)}%</span>
        <span>策略: {signal.strategy}</span>
      </div>
      {signal.reason && <div className="mt-1 text-[10px] text-muted-foreground">{signal.reason}</div>}
    </div>
  )
})

export function SocialTrading() {
  const queryClient = useQueryClient()
  const addToast = useToastStore((s) => s.addToast)
  const followerId = useFollowerId()
  const [activeTab, setActiveTab] = useState<'providers' | 'signals' | 'following'>('providers')
  const [following, setFollowing] = useState<Set<number>>(new Set())
  const [showPublishModal, setShowPublishModal] = useState(false)
  const [publishForm, setPublishForm] = useState({
    symbol: '', direction: 'buy', price: 0, stop_loss: 0, take_profit: 0,
    size: 0.05, confidence: 80, strategy: 'manual', reason: '',
  })

  /* ── Queries ── */
  const { data: providers = [], isLoading: providersLoading } = useQuery<Provider[]>({
    queryKey: ['social-providers'],
    queryFn: () => socialApi.providers(),
    staleTime: 60_000,
    retry: 2,
  })

  const { data: signals = [], isLoading: signalsLoading } = useQuery<SignalItem[]>({
    queryKey: ['social-signals'],
    queryFn: () => socialApi.signals(undefined, 50),
    staleTime: 30_000,
    retry: 2,
    refetchInterval: 30_000,
  })

  /* ── Mutations ── */
  const followMut = useMutation({
    mutationFn: (providerId: number) => socialApi.follow(providerId, followerId),
    onSuccess: (_, providerId) => {
      setFollowing((prev) => new Set(prev).add(providerId))
      addToast({ type: 'success', message: '关注成功', duration: 3000 })
    },
    onError: (err: Error) => addToast({ type: 'error', message: '关注失败: ' + err.message, duration: 5000 }),
  })

  const unfollowMut = useMutation({
    mutationFn: (providerId: number) => socialApi.unfollow(providerId, followerId),
    onSuccess: (_, providerId) => {
      setFollowing((prev) => {
        const next = new Set(prev)
        next.delete(providerId)
        return next
      })
      addToast({ type: 'success', message: '已取消关注', duration: 3000 })
    },
    onError: (err: Error) => addToast({ type: 'error', message: '取消关注失败: ' + err.message, duration: 5000 }),
  })

  const publishMut = useMutation({
    mutationFn: () => socialApi.publishSignal({
      provider_id: followerId,
      provider_name: 'Me',
      ...publishForm,
    }),
    onSuccess: () => {
      setShowPublishModal(false)
      queryClient.invalidateQueries({ queryKey: ['social-signals'] })
      addToast({ type: 'success', message: '信号发布成功', duration: 3000 })
    },
    onError: (err: Error) => addToast({ type: 'error', message: '发布失败: ' + err.message, duration: 5000 }),
  })

  const loading = providersLoading || signalsLoading

  /* ── Memoized callbacks ── */
  const handleToggleFollow = useCallback((providerId: number) => {
    if (following.has(providerId)) {
      unfollowMut.mutate(providerId)
    } else {
      followMut.mutate(providerId)
    }
  }, [following, followMut, unfollowMut])

  const isFollowPending = followMut.isPending || unfollowMut.isPending

  return (
    <div className="h-full flex flex-col p-4 gap-4 overflow-auto">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Users className="w-5 h-5 text-quant-gold" />
          <h1 className="text-lg font-bold text-foreground">社交交易</h1>
        </div>
        <button
          onClick={() => setShowPublishModal(true)}
          className="inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity"
        >
          <Signal className="w-3.5 h-3.5" /> 发布信号
        </button>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
        {[
          { k: 'providers' as const, label: '信号源', icon: Users },
          { k: 'signals' as const, label: '信号流', icon: Zap },
          { k: 'following' as const, label: '我的关注', icon: Star },
        ].map(t => (
          <button key={t.k} onClick={() => setActiveTab(t.k)}
            className={cn('flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium transition-colors',
              activeTab === t.k ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground')}>
            <t.icon className="h-3 w-3" />{t.label}
          </button>
        ))}
      </div>

      {/* Providers Tab */}
      {activeTab === 'providers' && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {providers.length === 0 ? (
            <div className="col-span-full text-center py-12 text-muted-foreground text-sm">
              <Users className="w-10 h-10 mx-auto mb-3 opacity-30" />
              暂无公开信号源
            </div>
          ) : providers.map(p => (
            <ProviderCard
              key={p.provider_id}
              provider={p}
              isFollowing={following.has(p.provider_id)}
              onToggleFollow={handleToggleFollow}
              isPending={isFollowPending}
            />
          ))}
        </div>
      )}

      {/* Signals Tab */}
      {activeTab === 'signals' && (
        <div className="space-y-2">
          {signalsLoading ? (
            <div className="flex items-center justify-center py-12 gap-2 text-muted-foreground">
              <Loader2 className="w-4 h-4 animate-spin" /> 加载中...
            </div>
          ) : signals.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground text-sm">
              <Zap className="w-10 h-10 mx-auto mb-3 opacity-30" />
              暂无信号
            </div>
          ) : signals.map(s => (
            <SignalCard key={s.id} signal={s} />
          ))}
        </div>
      )}

      {/* Following Tab */}
      {activeTab === 'following' && (
        <div className="text-center py-12 text-muted-foreground text-sm">
          <Star className="w-10 h-10 mx-auto mb-3 opacity-30" />
          {following.size === 0 ? '暂无关注的信号源' : `已关注 ${following.size} 个信号源`}
        </div>
      )}

      {/* Publish Modal */}
      {showPublishModal && (
        <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
          <div className="bg-quant-card border border-quant-border rounded-xl shadow-xl w-[480px] max-w-[90vw] max-h-[80vh] flex flex-col">
            <div className="flex items-center justify-between px-4 py-3 border-b border-quant-border">
              <h3 className="text-sm font-bold text-foreground">发布交易信号</h3>
              <button onClick={() => setShowPublishModal(false)} className="p-1 rounded text-muted-foreground hover:text-foreground">
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
            <div className="p-4 space-y-3 overflow-auto">
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className="text-[10px] text-muted-foreground mb-0.5 block">标的</label>
                  <input value={publishForm.symbol} onChange={e => setPublishForm(p => ({ ...p, symbol: e.target.value }))}
                    className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-0.5 block">方向</label>
                  <select value={publishForm.direction} onChange={e => setPublishForm(p => ({ ...p, direction: e.target.value }))}
                    className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold">
                    <option value="buy">买入</option>
                    <option value="sell">卖出</option>
                  </select>
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-0.5 block">价格</label>
                  <input type="number" value={publishForm.price} onChange={e => setPublishForm(p => ({ ...p, price: Number(e.target.value) }))}
                    className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-0.5 block">仓位比例</label>
                  <input type="number" step={0.01} max={1} value={publishForm.size} onChange={e => setPublishForm(p => ({ ...p, size: Number(e.target.value) }))}
                    className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-0.5 block">止损</label>
                  <input type="number" value={publishForm.stop_loss} onChange={e => setPublishForm(p => ({ ...p, stop_loss: Number(e.target.value) }))}
                    className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-0.5 block">止盈</label>
                  <input type="number" value={publishForm.take_profit} onChange={e => setPublishForm(p => ({ ...p, take_profit: Number(e.target.value) }))}
                    className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold" />
                </div>
              </div>
              <div>
                <label className="text-[10px] text-muted-foreground mb-0.5 block">理由</label>
                <textarea value={publishForm.reason} onChange={e => setPublishForm(p => ({ ...p, reason: e.target.value }))}
                  rows={3} className="w-full rounded border border-quant-border bg-quant-bg px-2 py-1.5 text-xs outline-none focus:border-quant-gold resize-none" />
              </div>
              <button
                onClick={() => publishMut.mutate()}
                disabled={publishMut.isPending}
                className="w-full py-2 rounded-lg bg-quant-gold text-white text-xs font-semibold hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {publishMut.isPending ? '发布中...' : '发布信号'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
