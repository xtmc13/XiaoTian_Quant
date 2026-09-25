import { useState, useEffect, useCallback, useRef } from 'react'
import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { api } from '@/lib/api'
import { Copy, Check, Users, Coins, Gift, Loader2, Link2 } from 'lucide-react'

// ── Affiliate 推荐码面板（0020）──
// 数据直接走通用 api client（@/lib/api 的 `api`），如需 referralApi 封装
// 可由主控按下方注释片段并入 api.ts（组件无需改动）。

export interface ReferralCodeInfo {
  code: string
  commission_pct: number
  active: boolean
  created_at: number
  referral_path: string
  referral_link: string
}

export interface ReferralSummary {
  code: string | null
  commission_pct: number
  active: boolean
  referral_count: number
  total_credits: number
  pending_credits: number
}

export interface ReferralEarning {
  id: number
  referrer_user_id: number
  referred_user_id: number
  referred_username?: string
  order_id: string
  order_amount: number
  credits: number
  status: string
  created_at: number
}

// 推荐链接优先用当前站点 origin（与后端 FRONTEND_BASE_URL 配置的绝对链接互为兜底）；
// FRONTEND_BASE_URL 未配置时后端 referral_link 退化为相对路径，此时也必须补 origin，
// 否则复制出来是不可用的相对地址。
function buildReferralLink(info: ReferralCodeInfo): string {
  if (info.referral_link && info.referral_link.startsWith('http')) return info.referral_link
  const path = info.referral_path || info.referral_link || ''
  return `${window.location.origin}${path}`
}

function formatTime(ts?: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

function formatUSDT(micro?: number): string {
  if (!micro) return '0'
  return (micro / 1e6).toFixed(2)
}

export function ReferralPanel() {
  const [codeInfo, setCodeInfo] = useState<ReferralCodeInfo | null>(null)
  const [summary, setSummary] = useState<ReferralSummary | null>(null)
  const [earnings, setEarnings] = useState<ReferralEarning[]>([])
  const [loading, setLoading] = useState(false)
  const [savingPct, setSavingPct] = useState(false)
  const [pctInput, setPctInput] = useState<string>('')
  const [copied, setCopied] = useState('')
  const copiedTimerRef = useRef<number | null>(null)

  const refresh = useCallback(() => {
    api.get<ReferralCodeInfo>('/billing/referral/code').then(setCodeInfo).catch(() => {})
    api.get<ReferralSummary>('/billing/referral/summary').then(setSummary).catch(() => {})
    api.get<{ earnings: ReferralEarning[] }>('/billing/referral/earnings')
      .then((r) => setEarnings(r?.earnings ?? []))
      .catch(() => {})
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    return () => {
      if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
    }
  }, [])

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text)
    setCopied(text)
    if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
    copiedTimerRef.current = window.setTimeout(() => setCopied(''), 2000)
  }

  // 保存佣金比例（0-50，服务端校验；缺省提交表示保持不变）
  const handleSavePct = async () => {
    if (!codeInfo) return
    const pct = pctInput.trim() === '' ? undefined : Number(pctInput)
    if (pct !== undefined && (!Number.isInteger(pct) || pct < 0 || pct > 50)) return
    setSavingPct(true)
    try {
      const updated = await api.post<ReferralCodeInfo>('/billing/referral/code',
        pct === undefined ? {} : { commission_pct: pct })
      setCodeInfo(updated)
      setPctInput('')
      refresh()
    } catch {
      /* 错误已由拦截器 toast 反馈 */
    } finally {
      setSavingPct(false)
    }
  }

  const referralLink = codeInfo ? buildReferralLink(codeInfo) : ''
  const displayPct = pctInput.trim() === '' ? codeInfo?.commission_pct ?? 10 : pctInput

  return (
    <div className="space-y-4">
      {/* 我的推荐码 */}
      <SectionCard title="我的推荐码 · 邀请好友赚积分">
        {!codeInfo ? (
          <div className="flex items-center justify-center py-6 text-muted-foreground text-sm">
            <Loader2 className="h-4 w-4 animate-spin mr-2" /> 加载中...
          </div>
        ) : (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-3">
              <code className="px-3 py-1.5 rounded-lg bg-quant-bg-secondary text-sm font-semibold tracking-wider">
                {codeInfo.code}
              </code>
              <button
                onClick={() => handleCopy(codeInfo.code)}
                className="flex items-center gap-1 text-xs text-quant-gold hover:underline"
              >
                {copied === codeInfo.code ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                {copied === codeInfo.code ? '已复制' : '复制推荐码'}
              </button>
              {!codeInfo.active && <Badge variant="neutral">已停用</Badge>}
            </div>

            <div className="p-3 rounded-lg bg-quant-bg-secondary space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs text-muted-foreground flex items-center gap-1">
                  <Link2 className="h-3 w-3" /> 推荐链接
                </span>
                <button onClick={() => handleCopy(referralLink)} className="text-[10px] text-quant-gold hover:underline">
                  {copied === referralLink ? '已复制' : '复制'}
                </button>
              </div>
              <code className="text-[11px] text-foreground break-all">{referralLink}</code>
            </div>

            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="text-muted-foreground flex items-center gap-1">
                <Gift className="h-3.5 w-3.5" /> 佣金比例
              </span>
              <input
                value={displayPct}
                onChange={(e) => setPctInput(e.target.value.replace(/[^0-9]/g, ''))}
                className="w-16 rounded-lg border border-quant-border bg-quant-bg px-2 py-1 text-sm outline-none focus:border-quant-gold"
              />
              <span className="text-muted-foreground">%（0-50）</span>
              <button
                onClick={handleSavePct}
                disabled={savingPct}
                className={cn('px-3 py-1 rounded-lg text-xs font-medium',
                  savingPct ? 'bg-quant-gold/50 text-white cursor-wait' : 'bg-quant-gold text-white hover:opacity-90')}
              >
                {savingPct ? <Loader2 className="h-3 w-3 animate-spin inline" /> : '保存'}
              </button>
            </div>
          </div>
        )}
      </SectionCard>

      {/* 统计 */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <div className="p-4 rounded-xl border border-quant-border bg-quant-card">
          <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
            <Users className="h-3.5 w-3.5" /> 推荐人数
          </div>
          <div className="text-xl font-bold">{summary?.referral_count ?? 0}</div>
        </div>
        <div className="p-4 rounded-xl border border-quant-border bg-quant-card">
          <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
            <Coins className="h-3.5 w-3.5" /> 累计获得积分
          </div>
          <div className="text-xl font-bold text-quant-gold">{summary?.total_credits ?? 0}</div>
        </div>
        <div className="p-4 rounded-xl border border-quant-border bg-quant-card">
          <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
            <Loader2 className="h-3.5 w-3.5" /> 待入账
          </div>
          <div className="text-xl font-bold">{summary?.pending_credits ?? 0}</div>
        </div>
      </div>

      {/* 佣金明细 */}
      <SectionCard title="佣金明细" headerAction={
        <button onClick={refresh} className="text-xs text-quant-gold hover:underline">刷新</button>
      }>
        {earnings.length === 0 ? (
          <div className="text-center py-6 text-muted-foreground text-xs">
            暂无佣金记录，分享推荐链接给好友吧
          </div>
        ) : (
          <div className="space-y-2">
            {earnings.map((e) => (
              <div key={e.id} className="flex items-center justify-between gap-3 p-3 rounded-lg border border-quant-border bg-quant-bg">
                <div className="min-w-0">
                  <div className="text-xs font-medium truncate">
                    {e.referred_username || `用户 #${e.referred_user_id}`}
                    <span className="text-muted-foreground ml-2">+{e.credits} 积分</span>
                  </div>
                  <div className="text-[10px] text-muted-foreground truncate">
                    订单 {e.order_id} · 金额 {formatUSDT(e.order_amount)} USDT · {formatTime(e.created_at)}
                  </div>
                </div>
                <Badge variant={e.status === 'credited' ? 'success' : 'neutral'}>
                  {e.status === 'credited' ? '已入账' : e.status === 'reversed' ? '已冲正' : e.status}
                </Badge>
              </div>
            ))}
          </div>
        )}
      </SectionCard>
    </div>
  )
}

export default ReferralPanel

// ── 可选：api client 封装片段（供主控并入 web/src/lib/api.ts）──
// 组件当前直接调用 api.get/api.post，不依赖该封装；并入后可将组件内
// 调用替换为 referralApi 以保持全站风格一致。
//
// export const referralApi = {
//   code: () => api.get<ReferralCodeInfo>('/billing/referral/code'),
//   updateCode: (data: { commission_pct?: number }) =>
//     api.post<ReferralCodeInfo>('/billing/referral/code', data),
//   summary: () => api.get<ReferralSummary>('/billing/referral/summary'),
//   earnings: () => api.get<{ earnings: ReferralEarning[] }>('/billing/referral/earnings'),
// }
