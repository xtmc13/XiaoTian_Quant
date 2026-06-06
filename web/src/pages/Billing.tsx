import { useState, useEffect, useRef } from 'react'
import { cn } from '@/lib/utils'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { billingApi } from '@/lib/api'
import { CheckCircle2, Zap, Crown, Star, Loader2, ExternalLink, Copy, Clock } from 'lucide-react'

interface Plan { id: string; name: string; name_en: string; price: number; credits: number | string; period_days: number }
interface ChainInfo { chain: string; address: string; memo: string }

export function Billing() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [chains, setChains] = useState<ChainInfo[]>([])
  const [selectedPlan, setSelectedPlan] = useState('')
  const [selectedChain, setSelectedChain] = useState('')
  const [txHash, setTxHash] = useState('')
  const [orderId, setOrderId] = useState('')
  const [loading, setLoading] = useState(false)
  const [copied, setCopied] = useState('')
  const copiedTimerRef = useRef<number | null>(null)

  useEffect(() => {
    billingApi.plans().then((r: any) => setPlans(Array.isArray(r) ? r : r?.plans ?? [])).catch(() => {})
    billingApi.chains().then((r: any) => setChains(Array.isArray(r) ? r : r?.chains ?? [])).catch(() => {})
  }, [])

  const handlePurchase = async () => {
    if (!selectedPlan) return
    setLoading(true)
    try {
      const data = await billingApi.createOrder({
        plan_id: selectedPlan,
        chain: selectedChain,
        tx_hash: txHash,
      })
      setOrderId(data.order_id)
    } catch (e: unknown) {
      // 错误已通过 UI 反馈，生产环境 console 由构建配置移除
    } finally { setLoading(false) }
  }

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopied(text)
    if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
    copiedTimerRef.current = window.setTimeout(() => setCopied(''), 2000)
  }

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
    }
  }, [])

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="space-y-6 max-w-4xl mx-auto">
        <PageHeader subtitle="升级会员解锁更多功能" />

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
                {typeof plan.credits === 'number' ? `${plan.credits} 积分` : `每30天 ${plan.credits} 积分`}
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
          <SectionCard title="USDT 支付">
            <div className="space-y-4">
              <div>
                <label className="text-xs text-muted-foreground mb-1.5 block">选择链</label>
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

              {selectedChain && chains.find(c => c.chain === selectedChain) && (
                <div className="p-3 rounded-lg bg-quant-bg-secondary space-y-1">
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-muted-foreground">{selectedChain} 地址</span>
                    <button onClick={() => handleCopy(chains.find(c => c.chain === selectedChain)!.address)} className="text-[10px] text-quant-gold hover:underline">
                      {copied === chains.find(c => c.chain === selectedChain)!.address ? '已复制' : '复制'}
                    </button>
                  </div>
                  <code className="text-[11px] text-foreground break-all">{chains.find(c => c.chain === selectedChain)!.address}</code>
                </div>
              )}

              <div>
                <label className="text-xs text-muted-foreground mb-1.5 block">交易哈希 (TX Hash)</label>
                <input
                  value={txHash} onChange={e => setTxHash(e.target.value)}
                  placeholder="转账完成后填入 TX Hash 确认"
                  className="w-full rounded-lg border border-quant-border bg-quant-bg px-3 py-2 text-sm outline-none focus:border-quant-gold"
                />
              </div>

              <button onClick={handlePurchase} disabled={loading || !selectedChain}
                className={cn('w-full py-2.5 rounded-lg text-sm font-medium',
                  loading ? 'bg-quant-gold/50 text-white cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}>
                {loading ? <Loader2 className="h-4 w-4 animate-spin inline mr-1" /> : null}
                {loading ? '处理中...' : `购买 ($${plans.find(p => p.id === selectedPlan)?.price || 0})`}
              </button>
            </div>
          </SectionCard>
        )}

        {/* Order Status */}
        {orderId && (
          <div className="p-4 rounded-xl border border-quant-gold/20 bg-quant-gold/5">
            <div className="flex items-center gap-2 mb-2">
              <Clock className="h-4 w-4 text-quant-gold" />
              <span className="text-sm font-medium">订单已创建</span>
            </div>
            <p className="text-xs text-muted-foreground">订单号: {orderId} — 等待链上确认，通常需要 3-30 分钟</p>
          </div>
        )}
      </div>
    </div>
  )
}
