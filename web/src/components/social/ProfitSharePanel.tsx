import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Clock, CheckCircle2, XCircle, Loader2, Coins, ArrowDownToLine } from 'lucide-react'
import { cn } from '@/lib/utils'
import { socialMarketApi } from '@/lib/api'
import { useToastStore } from '@/stores/toastStore'

/**
 * ProfitSharePanel — 开放信号市场 provider 侧面板。
 * 入驻申请（免费/低价入驻，平台按跟单盈利抽成 10%-30%）+ 收益汇总 + 提现。
 * 挂在 SocialTrading 页（provider 视角），follower 侧暂不需要。
 *
 * 依赖 api client 片段（由主控合入 web/src/lib/api.ts）：
 *   export const socialMarketApi = { ... }  // 见本文件底部注释或交接片段
 */

interface ProviderApply {
  id: number
  user_id: number
  name: string
  description: string
  monthly_fee: number
  profit_share_pct?: number | null
  fee_mode: 'monthly' | 'profit_share' | 'hybrid'
  apply_status: 'pending' | 'approved' | 'rejected'
  apply_note?: string
  approved_at?: number
  created_at: number
}

interface Earnings {
  today: number
  total: number
  payable: number
  withdrawn: number
  pending_withdrawals: number
  available: number
}

interface Withdrawal {
  id: string
  amount: number
  chain: string
  address: string
  tx_hash: string
  status: 'pending' | 'paid' | 'rejected'
  admin_note: string
  created_at: number
  processed_at?: number
}

const CHAINS = [
  { value: 'trc20', label: 'TRC20 (Tron)' },
  { value: 'bep20', label: 'BEP20 (BNB Chain)' },
  { value: 'erc20', label: 'ERC20 (Ethereum)' },
  { value: 'sol', label: 'SOL (Solana)' },
] as const

const STATUS_STYLE: Record<string, string> = {
  pending: 'bg-amber-500/10 text-amber-500 border-amber-500/30',
  approved: 'bg-emerald-500/10 text-emerald-500 border-emerald-500/30',
  rejected: 'bg-rose-500/10 text-rose-500 border-rose-500/30',
  paid: 'bg-emerald-500/10 text-emerald-500 border-emerald-500/30',
}

function fmt(n?: number | null) {
  return (n ?? 0).toLocaleString(undefined, { maximumFractionDigits: 4 })
}

function StatusBadge({ status }: { status: string }) {
  return (
    <span className={cn('inline-flex items-center rounded-full border px-2 py-0.5 text-xs capitalize', STATUS_STYLE[status] ?? 'bg-muted text-muted-foreground border-border')}>
      {status}
    </span>
  )
}

function deriveFeeMode(monthlyFee: number, pct: number | ''): string {
  const hasFee = monthlyFee > 0
  const hasPct = pct !== '' && Number(pct) > 0
  if (hasFee && hasPct) return 'hybrid（月费 + 分成）'
  if (hasFee) return 'monthly（月费订阅）'
  return 'profit_share（免费订阅，盈利分成）'
}

function ApplyForm({ onDone }: { onDone: () => void }) {
  const addToast = useToastStore((s) => s.addToast)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [pct, setPct] = useState<string>('20')
  const [monthlyFee, setMonthlyFee] = useState<string>('0')

  const mutation = useMutation({
    mutationFn: () =>
      socialMarketApi.apply({
        name,
        description,
        profit_share_pct: pct === '' ? null : Number(pct),
        monthly_fee: Number(monthlyFee || 0),
      }),
    onSuccess: () => {
      addToast({ type: 'success', message: '入驻申请已提交，等待管理员审核' })
      onDone()
    },
    onError: (e: Error) => addToast({ type: 'error', message: e.message || '申请失败' }),
  })

  const pctNum = pct === '' ? '' : Number(pct)
  const feeNum = Number(monthlyFee || 0)
  const pctValid = pctNum === '' || (pctNum >= 0 && pctNum <= 30)

  return (
    <form
      className="space-y-3 rounded-lg border border-border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault()
        mutation.mutate()
      }}
    >
      <h3 className="text-sm font-semibold">申请成为信号 Provider</h3>
      <p className="text-xs text-muted-foreground">
        免费/低价入驻，平台按跟单盈利抽成（0-30%）。提交后需管理员审核，通过后开放订阅。
      </p>
      <input
        className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
        placeholder="名称（必填）"
        value={name}
        onChange={(e) => setName(e.target.value)}
        required
      />
      <textarea
        className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
        placeholder="策略描述"
        rows={2}
        value={description}
        onChange={(e) => setDescription(e.target.value)}
      />
      <div className="grid grid-cols-2 gap-3">
        <label className="text-xs text-muted-foreground">
          分成比例 %（0-30，留空 = 不收分成）
          <input
            type="number"
            min={0}
            max={30}
            step="0.5"
            className={cn('mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm', pctValid ? 'border-input' : 'border-rose-500')}
            value={pct}
            onChange={(e) => setPct(e.target.value)}
          />
        </label>
        <label className="text-xs text-muted-foreground">
          月费 USDT（0 = 免费）
          <input
            type="number"
            min={0}
            step="0.01"
            className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            value={monthlyFee}
            onChange={(e) => setMonthlyFee(e.target.value)}
          />
        </label>
      </div>
      <p className="text-xs text-muted-foreground">收费模式：{deriveFeeMode(feeNum, pctNum)}</p>
      <button
        type="submit"
        disabled={mutation.isPending || !name.trim() || !pctValid}
        className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
      >
        {mutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
        提交申请
      </button>
    </form>
  )
}

function EarningsPanel() {
  const addToast = useToastStore((s) => s.addToast)
  const qc = useQueryClient()
  const [amount, setAmount] = useState('')
  const [chain, setChain] = useState<string>('trc20')
  const [address, setAddress] = useState('')

  const earningsQ = useQuery({
    queryKey: ['social-earnings'],
    queryFn: () => socialMarketApi.earnings(),
    refetchInterval: 30_000,
  })
  const withdrawalsQ = useQuery({
    queryKey: ['social-withdrawals'],
    queryFn: () => socialMarketApi.withdrawals(),
  })

  const withdrawM = useMutation({
    mutationFn: () => socialMarketApi.withdraw({ amount: Number(amount), chain, address }),
    onSuccess: () => {
      addToast({ type: 'success', message: '提现申请已提交，等待平台打款' })
      setAmount('')
      setAddress('')
      qc.invalidateQueries({ queryKey: ['social-earnings'] })
      qc.invalidateQueries({ queryKey: ['social-withdrawals'] })
    },
    onError: (e: Error) => addToast({ type: 'error', message: e.message || '提现失败' }),
  })

  const e: Earnings = earningsQ.data?.earnings ?? {
    today: 0, total: 0, payable: 0, withdrawn: 0, pending_withdrawals: 0, available: 0,
  }
  const withdrawals: Withdrawal[] = withdrawalsQ.data?.withdrawals ?? []
  const amountNum = Number(amount || 0)

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          { label: '今日分成', value: e.today },
          { label: '累计分成', value: e.total },
          { label: '可提现余额', value: e.available },
          { label: '在途提现', value: e.pending_withdrawals },
        ].map((item) => (
          <div key={item.label} className="rounded-lg border border-border bg-card p-3">
            <div className="text-xs text-muted-foreground">{item.label}</div>
            <div className="mt-1 text-lg font-semibold">{fmt(item.value)}</div>
          </div>
        ))}
      </div>

      <form
        className="space-y-3 rounded-lg border border-border bg-card p-4"
        onSubmit={(ev) => {
          ev.preventDefault()
          withdrawM.mutate()
        }}
      >
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <ArrowDownToLine className="h-4 w-4" /> 申请提现（USDT）
        </h3>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <label className="text-xs text-muted-foreground">
            金额
            <input
              type="number"
              min={0}
              step="0.01"
              className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              placeholder={`可用 ${fmt(e.available)}`}
              value={amount}
              onChange={(ev) => setAmount(ev.target.value)}
              required
            />
          </label>
          <label className="text-xs text-muted-foreground">
            链
            <select
              className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              value={chain}
              onChange={(ev) => setChain(ev.target.value)}
            >
              {CHAINS.map((c) => (
                <option key={c.value} value={c.value}>{c.label}</option>
              ))}
            </select>
          </label>
          <label className="text-xs text-muted-foreground">
            提币地址
            <input
              className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              value={address}
              onChange={(ev) => setAddress(ev.target.value)}
              required
            />
          </label>
        </div>
        <button
          type="submit"
          disabled={withdrawM.isPending || amountNum <= 0 || amountNum > e.available}
          className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
        >
          {withdrawM.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
          提交提现申请
        </button>
      </form>

      <div className="rounded-lg border border-border bg-card p-4">
        <h3 className="mb-2 text-sm font-semibold">提现记录</h3>
        {withdrawals.length === 0 ? (
          <p className="text-xs text-muted-foreground">暂无提现记录</p>
        ) : (
          <ul className="divide-y divide-border text-sm">
            {withdrawals.map((w) => (
              <li key={w.id} className="flex items-center justify-between gap-2 py-2">
                <div className="min-w-0">
                  <div className="font-medium">{fmt(w.amount)} USDT · {w.chain.toUpperCase()}</div>
                  <div className="truncate text-xs text-muted-foreground">{w.address}</div>
                  {w.tx_hash && <div className="truncate text-xs text-muted-foreground">tx: {w.tx_hash}</div>}
                  {w.status === 'rejected' && w.admin_note && (
                    <div className="text-xs text-rose-500">驳回：{w.admin_note}</div>
                  )}
                </div>
                <StatusBadge status={w.status} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

export default function ProfitSharePanel() {
  const qc = useQueryClient()
  const myQ = useQuery({
    queryKey: ['social-my-provider'],
    queryFn: () => socialMarketApi.myProvider(),
  })
  const provider: ProviderApply | null = myQ.data?.provider ?? null
  const approved = provider?.apply_status === 'approved'

  return (
    <section className="space-y-4 rounded-xl border border-border bg-background p-4">
      <header className="flex items-center justify-between">
        <h2 className="flex items-center gap-2 text-base font-semibold">
          <Coins className="h-5 w-5" /> 信号市场 · 利润分成
        </h2>
        {provider && <StatusBadge status={provider.apply_status} />}
      </header>

      {myQ.isLoading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> 加载中…
        </div>
      ) : !provider ? (
        <ApplyForm onDone={() => qc.invalidateQueries({ queryKey: ['social-my-provider'] })} />
      ) : (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            <span className="font-medium">{provider.name}</span>
            <span className="text-xs text-muted-foreground">
              模式：{provider.fee_mode}
              {provider.profit_share_pct != null && ` · 分成 ${provider.profit_share_pct}%`}
              {provider.monthly_fee > 0 && ` · 月费 ${provider.monthly_fee}`}
            </span>
            {provider.apply_status === 'pending' && (
              <span className="inline-flex items-center gap-1 text-xs text-amber-500">
                <Clock className="h-3.5 w-3.5" /> 等待管理员审核，通过前无法被订阅
              </span>
            )}
            {provider.apply_status === 'rejected' && (
              <span className="inline-flex items-center gap-1 text-xs text-rose-500">
                <XCircle className="h-3.5 w-3.5" /> {provider.apply_note || '审核未通过'}
              </span>
            )}
            {approved && (
              <span className="inline-flex items-center gap-1 text-xs text-emerald-500">
                <CheckCircle2 className="h-3.5 w-3.5" /> 已上架，开放订阅
              </span>
            )}
          </div>
          {approved ? (
            <EarningsPanel />
          ) : (
            <p className="text-xs text-muted-foreground">
              收益面板在审核通过后开放。分成按日结算：T 日跟单盈利 → T+3 锁定期 → 可提现。
            </p>
          )}
        </div>
      )}
    </section>
  )
}

