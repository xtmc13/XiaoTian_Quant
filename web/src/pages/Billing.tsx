import { useState, useEffect, useRef, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { billingApi } from '@/lib/api'
import ReferralPanel from '@/components/billing/ReferralPanel'
import type { BillingOrder, BillingPlan, ChainInfo, BillingSubscription, BillingVerificationResponse, StripeConfig } from '@/types'
import { CheckCircle2, Zap, Crown, Star, Loader2, ExternalLink, Copy, Clock, CreditCard, RefreshCw } from 'lucide-react'

// 订单状态 → 徽标展示
const STATUS_META: Record<string, { label: string; variant: 'warning' | 'info' | 'success' | 'error' | 'neutral' }> = {
  pending: { label: '待核验', variant: 'warning' },
  confirming: { label: '确认中', variant: 'info' },
  paid: { label: '已支付', variant: 'success' },
  failed: { label: '失败', variant: 'error' },
  expired: { label: '已过期', variant: 'neutral' },
}

// formatUSDT 微单位(1e6) → 展示字符串
function formatUSDT(micro?: number): string {
  if (!micro) return '0'
  return (micro / 1e6).toFixed(2)
}

function formatTime(ts?: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

export function Billing() {
  const [plans, setPlans] = useState<BillingPlan[]>([])
  const [chains, setChains] = useState<ChainInfo[]>([])
  const [subscription, setSubscription] = useState<BillingSubscription | null>(null)
  const [stripe, setStripe] = useState<StripeConfig>({ enabled: false })
  const [orders, setOrders] = useState<BillingOrder[]>([])
  const [selectedPlan, setSelectedPlan] = useState('')
  const [selectedChain, setSelectedChain] = useState('')
  const [txHash, setTxHash] = useState('')
  const [activeOrder, setActiveOrder] = useState<BillingOrder | null>(null)
  const [verification, setVerification] = useState<BillingVerificationResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [stripeLoading, setStripeLoading] = useState(false)
  const [copied, setCopied] = useState('')
  const copiedTimerRef = useRef<number | null>(null)
  const pollTimerRef = useRef<number | null>(null)

  const refreshSubscription = useCallback(() => {
    billingApi.subscription().then(setSubscription).catch(() => {})
  }, [])

  const refreshOrders = useCallback(() => {
    billingApi.orders().then((r) => setOrders(r?.orders ?? [])).catch(() => {})
  }, [])

  useEffect(() => {
    billingApi.plans().then((r: any) => setPlans(Array.isArray(r) ? r : r?.plans ?? [])).catch(() => {})
    billingApi.chains().then((r: any) => setChains(Array.isArray(r) ? r : r?.chains ?? [])).catch(() => {})
    billingApi.stripeConfig().then(setStripe).catch(() => {})
    refreshSubscription()
    refreshOrders()
  }, [refreshSubscription, refreshOrders])

  // 轮询活跃订单状态：pending/confirming 每 10s 查一次
  useEffect(() => {
    if (!activeOrder || (activeOrder.status !== 'pending' && activeOrder.status !== 'confirming')) return
    pollTimerRef.current = window.setInterval(() => {
      billingApi.order(activeOrder.order_id).then((o) => {
        setActiveOrder(o)
        if (o.status === 'paid') {
          refreshSubscription()
          refreshOrders()
        }
      }).catch(() => {})
      // 同步链上核验快照（确认数/实收金额）
      billingApi.verification(activeOrder.order_id).then(setVerification).catch(() => {})
    }, 10000)
    return () => {
      if (pollTimerRef.current) window.clearInterval(pollTimerRef.current)
    }
  }, [activeOrder, refreshSubscription, refreshOrders])

  // 非轮询状态（failed/paid）也拉一次核验快照展示链上细节
  useEffect(() => {
    if (!activeOrder?.tx_hash) return
    billingApi.verification(activeOrder.order_id).then(setVerification).catch(() => {})
  }, [activeOrder?.order_id, activeOrder?.tx_hash])

  // Stripe 支付成功回跳：/billing?paid=<order_id>
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const paidId = params.get('paid')
    if (!paidId) return
    billingApi.order(paidId).then((o) => {
      setActiveOrder(o)
      if (o.status === 'paid') {
        refreshSubscription()
        refreshOrders()
      }
    }).catch(() => {})
    window.history.replaceState({}, '', '/billing')
  }, [refreshSubscription, refreshOrders])

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopied(text)
    if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
    copiedTimerRef.current = window.setTimeout(() => setCopied(''), 2000)
  }

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
      if (pollTimerRef.current) window.clearInterval(pollTimerRef.current)
    }
  }, [])

  // 创建 USDT 订单
  const handleCreateOrder = async () => {
    if (!selectedPlan || !selectedChain) return
    setLoading(true)
    try {
      const o = await billingApi.createOrder({ plan_id: selectedPlan, chain: selectedChain })
      setActiveOrder(o)
      refreshOrders()
    } catch {
      // 错误已由拦截器 toast 反馈
    } finally {
      setLoading(false)
    }
  }

  // 提交/重提交易哈希
  const handleSubmitTx = async () => {
    if (!activeOrder || !txHash.trim()) return
    setLoading(true)
    try {
      const o = await billingApi.submitTx(activeOrder.order_id, txHash.trim())
      setActiveOrder(o)
      setTxHash('')
      refreshOrders()
    } catch {
      /* toast 已反馈 */
    } finally {
      setLoading(false)
    }
  }

  // Stripe 信用卡支付：plan_id 直达收银台（服务端复用/创建 stripe 订单）→ 跳 checkout_url
  const handleStripePay = async () => {
    if (!selectedPlan) return
    setStripeLoading(true)
    try {
      const base = window.location.origin
      const session = await billingApi.stripeCheckout({
        plan_id: selectedPlan,
        success_url: `${base}/billing?stripe=success`,
        cancel_url: `${base}/billing?stripe=cancel`,
      })
      window.location.href = session.checkout_url
    } catch {
      /* toast 已反馈 */
    } finally {
      setStripeLoading(false)
    }
  }

  const currentChain = chains.find((c) => c.chain === selectedChain)
  const currentPlan = plans.find((p) => p.id === selectedPlan)
  const activeStatus = activeOrder ? STATUS_META[activeOrder.status] : null

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-6 max-w-4xl mx-auto">
        <PageHeader subtitle="升级会员解锁更多功能" />

        {/* 当前套餐/积分 */}
        <SectionCard title="我的会员">
          <div className="flex flex-wrap items-center gap-x-8 gap-y-2 text-sm">
            <div>
              <span className="text-xs text-muted-foreground mr-2">当前套餐</span>
              <span className="font-medium">
                {subscription?.plan
                  ? (plans.find((p) => p.id === subscription.plan)?.name ?? subscription.plan)
                  : '免费版'}
              </span>
              {subscription && subscription.vip_expires_at > 0 && (
                <span className="text-xs text-muted-foreground ml-2">
                  有效期至 {formatTime(subscription.vip_expires_at)}
                </span>
              )}
              {subscription && subscription.vip_expires_at === -1 && (
                <span className="text-xs text-quant-gold ml-2">终身有效</span>
              )}
            </div>
            <div>
              <span className="text-xs text-muted-foreground mr-2">积分</span>
              <span className="font-medium text-quant-gold">{subscription?.credits ?? 0}</span>
            </div>
          </div>
        </SectionCard>

        {/* Plans */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {plans.map((plan) => (
            <button
              key={plan.id}
              onClick={() => setSelectedPlan(plan.id)}
              className={cn(
                'text-left p-5 rounded-xl border transition-all',
                selectedPlan === plan.id
                  ? 'border-quant-gold bg-quant-gold/5 shadow-lg shadow-quant-gold/5'
                  : 'border-quant-border bg-quant-card hover:border-quant-gold/30'
              )}
            >
              <div className="flex items-center gap-2 mb-2">
                {plan.id === 'lifetime' ? <Crown className="h-5 w-5 text-quant-gold" /> :
                 plan.id === 'yearly' ? <Star className="h-5 w-5 text-blue-400" /> :
                 <Zap className="h-5 w-5 text-green-400" />}
                <span className="font-semibold">{plan.name}</span>
              </div>
              <div className="text-2xl font-bold mb-1">${plan.price}</div>
              <div className="text-xs text-muted-foreground">
                {typeof plan.credits === 'number' ? `${plan.credits} 积分` : `每30天 ${plan.credits_per_30d ?? plan.credits} 积分`}
              </div>
              <div className="text-[10px] text-muted-foreground mt-1">
                {plan.period_days > 0 ? `${plan.period_days} 天` : '终身有效'}
              </div>
            </button>
          ))}
          {plans.length === 0 && (
            <div className="col-span-3 text-center py-8 text-muted-foreground text-sm">加载会员方案中...</div>
          )}
        </div>

        {/* Payment */}
        {selectedPlan && (
          <SectionCard title="支付">
            <div className="space-y-4">
              {/* USDT 链选择 */}
              <div>
                <label className="text-xs text-muted-foreground mb-1.5 block">USDT 转账（选择链）</label>
                <div className="flex flex-wrap gap-2">
                  {chains.map((ch) => (
                    <button
                      key={ch.chain}
                      onClick={() => setSelectedChain(ch.chain)}
                      className={cn('px-3 py-1.5 rounded-lg text-xs border transition-colors',
                        selectedChain === ch.chain
                          ? 'border-quant-gold bg-quant-gold/10 text-quant-gold'
                          : 'border-quant-border text-muted-foreground hover:border-quant-gold/30'
                      )}
                    >
                      {ch.chain}
                    </button>
                  ))}
                  {chains.length === 0 && <span className="text-xs text-muted-foreground">暂无可用的支付链</span>}
                </div>
              </div>

              {/* 收款地址 + 金额 */}
              {selectedChain && currentChain && (
                <div className="p-3 rounded-lg bg-quant-bg-secondary space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-muted-foreground">{selectedChain} 收款地址</span>
                    <button onClick={() => handleCopy(currentChain.address)} className="text-[10px] text-quant-gold hover:underline">
                      {copied === currentChain.address ? '已复制' : '复制'}
                    </button>
                  </div>
                  <code className="text-[11px] text-foreground break-all">{currentChain.address}</code>
                  <div className="text-xs text-muted-foreground pt-1 border-t border-quant-border">
                    转账金额：<span className="text-quant-gold font-medium">${currentPlan?.price ?? 0} USDT</span>
                    <span className="text-[10px] ml-2">（务必足额到账，超额不退）</span>
                  </div>
                </div>
              )}

              {selectedChain && (
                <button onClick={handleCreateOrder} disabled={loading}
                  className={cn('w-full py-2.5 rounded-lg text-sm font-medium',
                    loading ? 'bg-quant-gold/50 text-white cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
                  {loading ? <Loader2 className="h-4 w-4 animate-spin inline mr-1" /> : null}
                  {loading ? '处理中...' : `创建订单并获取充值地址 ($${currentPlan?.price ?? 0})`}
                </button>
              )}

              {/* Stripe 信用卡支付 */}
              {stripe.enabled && (
                <button onClick={handleStripePay} disabled={stripeLoading}
                  className={cn('w-full py-2.5 rounded-lg text-sm font-medium border',
                    'border-quant-gold/40 text-quant-gold hover:bg-quant-gold/10',
                    stripeLoading && 'opacity-50 cursor-wait')}>
                  {stripeLoading ? <Loader2 className="h-4 w-4 animate-spin inline mr-1" /> :
                    <CreditCard className="h-4 w-4 inline mr-1" />}
                  {stripeLoading ? '跳转中...' : '信用卡支付 (Stripe)'}
                </button>
              )}
            </div>
          </SectionCard>
        )}

        {/* 活跃订单状态 */}
        {activeOrder && activeStatus && (
          <div className={cn('p-4 rounded-xl border space-y-2',
            activeOrder.status === 'paid' ? 'border-green-500/30 bg-green-500/5'
            : activeOrder.status === 'failed' ? 'border-red-500/30 bg-red-500/5'
            : 'border-quant-gold/20 bg-quant-gold/5')}>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                {activeOrder.status === 'paid' ? <CheckCircle2 className="h-4 w-4 text-green-500" /> :
                 activeOrder.status === 'failed' ? <RefreshCw className="h-4 w-4 text-red-400" /> :
                 <Clock className="h-4 w-4 text-quant-gold" />}
                <span className="text-sm font-medium">订单 {activeOrder.order_id}</span>
              </div>
              <Badge variant={activeStatus.variant}>{activeStatus.label}</Badge>
            </div>
            <p className="text-xs text-muted-foreground">
              {activeOrder.plan_id} · {activeOrder.chain} · {formatUSDT(activeOrder.amount_usdt)} USDT
            </p>
            {activeOrder.status === 'pending' && (
              <p className="text-xs text-muted-foreground">已完成转账？请在下方填入交易哈希（TX Hash）提交核验。</p>
            )}
            {activeOrder.status === 'confirming' && (
              <p className="text-xs text-muted-foreground">交易已上链，等待区块确认，通常需要 3-30 分钟...</p>
            )}
            {activeOrder.status === 'paid' && (
              <p className="text-xs text-green-500">支付成功，会员/积分已发放。</p>
            )}
            {activeOrder.status === 'failed' && (
              <p className="text-xs text-red-400">核验失败：{activeOrder.fail_reason || '未知原因'}。可在下方重新提交交易哈希。</p>
            )}
            {activeOrder.status === 'expired' && (
              <p className="text-xs text-muted-foreground">订单已过期（30 分钟未付款），请重新创建订单。</p>
            )}

            {/* 链上核验详情（C4.1）：确认数 / 实收金额 / 区块 */}
            {activeOrder.tx_hash && verification?.verification && (
              <div className="mt-1 rounded-lg border border-quant-border/60 bg-quant-bg/40 px-3 py-2 space-y-1 text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">链上核验</span>
                  <Badge variant={
                    verification.stage === 'confirmed' || verification.stage === 'paid' ? 'success'
                    : verification.stage === 'invalid' ? 'error'
                    : 'neutral'
                  }>
                    {verification.stage === 'confirming' ? '确认中'
                    : verification.stage === 'confirmed' ? '已确认'
                    : verification.stage === 'invalid' ? '核验不符'
                    : verification.stage === 'paid' ? '已到账'
                    : '链上查询中'}
                  </Badge>
                </div>
                <p className="text-muted-foreground font-mono break-all">TX: {activeOrder.tx_hash}</p>
                {verification.verification.confirmations !== undefined && verification.verification.required_confirmations ? (
                  <p className="text-muted-foreground">
                    区块确认：{verification.verification.confirmations}/{verification.verification.required_confirmations}
                    {verification.verification.block_number ? `（区块 #${verification.verification.block_number}）` : ''}
                  </p>
                ) : null}
                {verification.verification.received_micro ? (
                  <p className="text-muted-foreground">
                    实收 {formatUSDT(verification.verification.received_micro)} / {formatUSDT(verification.verification.expected_micro ?? 0)} USDT
                  </p>
                ) : null}
                {verification.verification.fail_reason && (
                  <p className="text-red-400">{verification.verification.fail_reason}</p>
                )}
              </div>
            )}

            {/* 提交/重提交易哈希（pending/failed 可用） */}
            {(activeOrder.status === 'pending' || activeOrder.status === 'failed') && activeOrder.chain !== 'stripe' && (
              <div className="flex gap-2 pt-1">
                <input
                  value={txHash} onChange={(e) => setTxHash(e.target.value)}
                  placeholder="粘贴 TX Hash（交易哈希）"
                  className="flex-1 rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm outline-none focus:border-quant-gold"
                />
                <button onClick={handleSubmitTx} disabled={loading || !txHash.trim()}
                  className={cn('px-4 py-2 rounded-lg text-sm font-medium',
                    loading ? 'bg-quant-gold/50 text-white' : 'bg-quant-gold text-white hover:opacity-90')}>
                  {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : '提交核验'}
                </button>
              </div>
            )}
          </div>
        )}

        {/* 订单历史 */}
        <SectionCard title="订单历史" headerAction={
          <button onClick={refreshOrders} className="text-xs text-quant-gold hover:underline">刷新</button>
        }>
          {orders.length === 0 ? (
            <div className="text-center py-6 text-muted-foreground text-xs">暂无订单</div>
          ) : (
            <div className="space-y-2">
              {orders.map((o) => {
                const meta = STATUS_META[o.status] ?? { label: o.status, variant: 'neutral' as const }
                return (
                  <button key={o.order_id} onClick={() => setActiveOrder(o)}
                    className="w-full flex items-center justify-between gap-3 p-3 rounded-lg border border-quant-border bg-quant-bg hover:border-quant-gold/30 text-left">
                    <div className="min-w-0">
                      <div className="text-xs font-medium truncate">
                        {plans.find((p) => p.id === o.plan_id)?.name ?? o.plan_id}
                        <span className="text-muted-foreground ml-2">{o.chain}</span>
                      </div>
                      <div className="text-[10px] text-muted-foreground truncate">
                        {o.order_id} · {formatTime(o.created_at)}
                        {o.status === 'failed' && o.fail_reason ? ` · ${o.fail_reason}` : ''}
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <span className="text-xs">{formatUSDT(o.amount_usdt)} USDT</span>
                      <Badge variant={meta.variant}>{meta.label}</Badge>
                    </div>
                  </button>
                )
              })}
            </div>
          )}
        </SectionCard>

        <ReferralPanel />

        <p className="text-[10px] text-muted-foreground flex items-center gap-1">
          <ExternalLink className="h-3 w-3" />
          支付完成后系统自动核验链上交易并发放会员/积分，通常需要 3-30 分钟。
        </p>
      </div>
    </div>
  )
}
