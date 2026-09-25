import React, { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ladderApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { formatLinePrice } from './chartOverlays'
import {
  distributePrices,
  ladderLegStatusLabel,
  ladderProgress,
  ladderStatusLabel,
  splitAmount,
  splitClosePct,
  type LadderDistribution,
} from './ladderUtils'
import type { LadderAmendRequest, LadderOrder } from '@/types'
import { ChevronDown, ChevronUp, Layers, X } from 'lucide-react'

/* 阶梯智能单面板:创建表单(档数/分布/金额/目标/SL) + 活跃单列表(进度/改价/取消/全平)。 */

export const LADDER_QUERY_KEY = ['ladders']

export function useLadders() {
  return useQuery({
    queryKey: LADDER_QUERY_KEY,
    queryFn: () => ladderApi.list(true),
    refetchInterval: 5000,
  })
}

interface CreateProps {
  symbol: string
  currentPrice: number
  pricePrecision: number
  onCreated?: () => void
}

interface EntryRow {
  price: string
  amount: string
}

interface TargetRow {
  price: string
  pct: string
}

export function LadderCreateForm({ symbol, currentPrice, pricePrecision, onCreated }: CreateProps) {
  const queryClient = useQueryClient()
  const [side, setSide] = useState<'BUY' | 'SELL'>('BUY')
  const [legCount, setLegCount] = useState(3)
  const [dist, setDist] = useState<LadderDistribution>('arithmetic')
  const [rangeLow, setRangeLow] = useState('')
  const [rangeHigh, setRangeHigh] = useState('')
  const [totalAmount, setTotalAmount] = useState('100')
  const [entries, setEntries] = useState<EntryRow[]>([])
  const [targetCount, setTargetCount] = useState(2)
  const [targets, setTargets] = useState<TargetRow[]>([])
  const [stopLoss, setStopLoss] = useState('')
  const [breakevenAfter, setBreakevenAfter] = useState(0)
  const [trailingStep, setTrailingStep] = useState('0')

  // 以现价为锚初始化区间(买入阶梯默认挂在现价下方,卖出在上方)
  useEffect(() => {
    if (!(currentPrice > 0)) return
    if (rangeLow === '' && rangeHigh === '') {
      const lo = side === 'BUY' ? currentPrice * 0.95 : currentPrice * 1.005
      const hi = side === 'BUY' ? currentPrice * 0.995 : currentPrice * 1.05
      setRangeLow(lo.toFixed(pricePrecision))
      setRangeHigh(hi.toFixed(pricePrecision))
      setStopLoss((side === 'BUY' ? lo * 0.97 : hi * 1.03).toFixed(pricePrecision))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentPrice])

  // 档位参数变化 → 重新生成可编辑行(覆盖手工微调)
  useEffect(() => {
    const lo = parseFloat(rangeLow)
    const hi = parseFloat(rangeHigh)
    const prices = distributePrices(dist, legCount, lo, hi)
    const amounts = splitAmount(parseFloat(totalAmount) || 0, prices.length)
    setEntries(
      prices.map((p, i) => ({
        price: p.toFixed(pricePrecision),
        amount: amounts[i] ? amounts[i].toFixed(2) : '',
      }))
    )
  }, [dist, legCount, rangeLow, rangeHigh, totalAmount, pricePrecision])

  // 目标参数变化 → 重新生成目标行(默认在最后一档之外等距铺开)
  useEffect(() => {
    const lo = parseFloat(rangeLow)
    const hi = parseFloat(rangeHigh)
    if (!(lo > 0) || !(hi > 0)) {
      setTargets([])
      return
    }
    const span = Math.abs(hi - lo) || hi * 0.02
    const base = side === 'BUY' ? Math.max(lo, hi) : Math.min(lo, hi)
    const pcts = splitClosePct(targetCount)
    setTargets(
      pcts.map((pct, i) => {
        const p = side === 'BUY' ? base + span * 0.5 * (i + 1) : base - span * 0.5 * (i + 1)
        return { price: Math.max(p, 0).toFixed(pricePrecision), pct: String(pct) }
      })
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [targetCount, rangeLow, rangeHigh, side, pricePrecision])

  const pctSum = useMemo(() => targets.reduce((s, t) => s + (parseFloat(t.pct) || 0), 0), [targets])
  const amountSum = useMemo(() => entries.reduce((s, e) => s + (parseFloat(e.amount) || 0), 0), [entries])

  const createMut = useMutation({
    mutationFn: () => {
      const body = {
        symbol,
        side,
        entries: entries.map((e) => ({ price: parseFloat(e.price), amount_usdt: parseFloat(e.amount) })),
        targets: targets.map((t) => ({ price: parseFloat(t.price), close_pct: parseFloat(t.pct) })),
        stop_loss: parseFloat(stopLoss) || 0,
        breakeven_after_target: breakevenAfter,
        trailing_step_pct: parseFloat(trailingStep) || 0,
      }
      return ladderApi.create(body)
    },
    onSuccess: () => {
      toast('success', '阶梯单已创建')
      queryClient.invalidateQueries({ queryKey: LADDER_QUERY_KEY })
      queryClient.invalidateQueries({ queryKey: ['orders'] })
      onCreated?.()
    },
    onError: (e: unknown) => {
      const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error
      toast('error', msg || '阶梯单创建失败')
    },
  })

  const canSubmit =
    entries.length >= 1 &&
    entries.every((e) => parseFloat(e.price) > 0 && parseFloat(e.amount) > 0) &&
    targets.length >= 1 &&
    targets.every((t) => parseFloat(t.price) > 0 && parseFloat(t.pct) > 0) &&
    Math.abs(pctSum - 100) <= 0.01 &&
    !createMut.isPending

  const inputCls =
    'w-full bg-quant-bg border border-quant-border rounded px-2 h-7 text-[11px] font-mono focus:border-quant-gold outline-none'
  const labelCls = 'text-[10px] text-muted-foreground'

  return (
    <div className="flex flex-col gap-2.5" data-testid="ladder-create-form">
      {/* 方向 */}
      <div className="flex gap-1.5">
        {(['BUY', 'SELL'] as const).map((s) => (
          <button
            key={s}
            onClick={() => setSide(s)}
            className={cn(
              'flex-1 py-1.5 text-[11px] font-bold rounded transition-colors',
              side === s
                ? s === 'BUY'
                  ? 'bg-[#0ECB81] text-black'
                  : 'bg-[#F6465D] text-foreground'
                : 'bg-quant-bg text-muted-foreground border border-quant-border'
            )}
          >
            {s === 'BUY' ? '买入阶梯' : '卖出阶梯'}
          </button>
        ))}
      </div>

      {/* 档数 + 分布 */}
      <div className="flex gap-2">
        <div className="flex-1">
          <div className={labelCls}>档数 (2-10)</div>
          <input
            type="number"
            min={2}
            max={10}
            value={legCount}
            onChange={(e) => setLegCount(Math.max(2, Math.min(10, Number(e.target.value) || 2)))}
            className={inputCls}
          />
        </div>
        <div className="flex-1">
          <div className={labelCls}>价格分布</div>
          <div className="flex gap-1 bg-quant-bg p-0.5 rounded border border-quant-border">
            {(['arithmetic', 'geometric'] as const).map((m) => (
              <button
                key={m}
                onClick={() => setDist(m)}
                className={cn(
                  'flex-1 py-1 text-[10px] rounded',
                  dist === m ? 'bg-quant-bg-secondary text-foreground' : 'text-muted-foreground'
                )}
              >
                {m === 'arithmetic' ? '等差' : '等比'}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* 价格区间 + 总金额 */}
      <div className="flex gap-2">
        <div className="flex-1">
          <div className={labelCls}>最低价</div>
          <input value={rangeLow} onChange={(e) => setRangeLow(e.target.value)} className={inputCls} />
        </div>
        <div className="flex-1">
          <div className={labelCls}>最高价</div>
          <input value={rangeHigh} onChange={(e) => setRangeHigh(e.target.value)} className={inputCls} />
        </div>
        <div className="flex-1">
          <div className={labelCls}>总金额 USDT</div>
          <input value={totalAmount} onChange={(e) => setTotalAmount(e.target.value)} className={inputCls} />
        </div>
      </div>

      {/* 档位明细 */}
      <div className="rounded border border-quant-border/60 bg-quant-bg/40 p-1.5 max-h-36 overflow-y-auto">
        <div className="grid grid-cols-[1.2rem_1fr_1fr] gap-1 text-[9px] text-muted-foreground px-0.5 pb-1">
          <span>#</span>
          <span>价格</span>
          <span>金额 USDT</span>
        </div>
        {entries.map((row, i) => (
          <div key={i} className="grid grid-cols-[1.2rem_1fr_1fr] gap-1 items-center py-0.5">
            <span className="text-[10px] text-muted-foreground text-center">{i + 1}</span>
            <input
              value={row.price}
              onChange={(e) =>
                setEntries((arr) => arr.map((r, j) => (j === i ? { ...r, price: e.target.value } : r)))
              }
              className={inputCls}
            />
            <input
              value={row.amount}
              onChange={(e) =>
                setEntries((arr) => arr.map((r, j) => (j === i ? { ...r, amount: e.target.value } : r)))
              }
              className={inputCls}
            />
          </div>
        ))}
      </div>

      {/* 目标 */}
      <div className="flex items-center justify-between">
        <span className={labelCls}>止盈目标（合计需=100%，当前 {pctSum.toFixed(2)}%）</span>
        <div className="flex items-center gap-1">
          <span className={labelCls}>个数</span>
          <input
            type="number"
            min={1}
            max={10}
            value={targetCount}
            onChange={(e) => setTargetCount(Math.max(1, Math.min(10, Number(e.target.value) || 1)))}
            className={cn(inputCls, 'w-12')}
          />
        </div>
      </div>
      <div className="rounded border border-quant-border/60 bg-quant-bg/40 p-1.5 max-h-28 overflow-y-auto">
        {targets.map((row, i) => (
          <div key={i} className="grid grid-cols-[2.6rem_1fr_4rem] gap-1 items-center py-0.5">
            <span className="text-[10px] text-muted-foreground">目标{i + 1}</span>
            <input
              value={row.price}
              onChange={(e) =>
                setTargets((arr) => arr.map((r, j) => (j === i ? { ...r, price: e.target.value } : r)))
              }
              className={inputCls}
            />
            <input
              value={row.pct}
              onChange={(e) =>
                setTargets((arr) => arr.map((r, j) => (j === i ? { ...r, pct: e.target.value } : r)))
              }
              className={inputCls}
            />
          </div>
        ))}
      </div>

      {/* SL + 规则 */}
      <div className="flex gap-2">
        <div className="flex-1">
          <div className={labelCls}>止损价 SL（0=不用）</div>
          <input value={stopLoss} onChange={(e) => setStopLoss(e.target.value)} className={inputCls} />
        </div>
        <div className="flex-1">
          <div className={labelCls}>保本@目标</div>
          <select
            value={breakevenAfter}
            onChange={(e) => setBreakevenAfter(Number(e.target.value))}
            className={cn(inputCls, 'h-7')}
          >
            <option value={0}>禁用</option>
            {targets.map((_, i) => (
              <option key={i} value={i + 1}>
                第 {i + 1} 个后
              </option>
            ))}
          </select>
        </div>
        <div className="flex-1">
          <div className={labelCls}>追踪步长 %</div>
          <input value={trailingStep} onChange={(e) => setTrailingStep(e.target.value)} className={inputCls} />
        </div>
      </div>

      <div className="flex justify-between text-[10px] text-muted-foreground">
        <span>
          合计 {amountSum.toFixed(2)} USDT · {entries.length} 档 · {targets.length} 目标
        </span>
        {Math.abs(pctSum - 100) > 0.01 && <span className="text-red-400">目标比例需合计 100%</span>}
      </div>

      <button
        disabled={!canSubmit}
        onClick={() => createMut.mutate()}
        className={cn(
          'w-full py-2 rounded-lg text-[12px] font-bold transition-colors',
          canSubmit
            ? 'bg-quant-gold text-black hover:bg-quant-gold/90'
            : 'bg-quant-bg-tertiary text-muted-foreground cursor-not-allowed'
        )}
      >
        {createMut.isPending ? '创建中…' : '创建阶梯单'}
      </button>
    </div>
  )
}

/* ── 活跃/历史阶梯单列表 ── */

interface ListProps {
  symbol?: string // 传入时只显示该交易对
  pricePrecision?: number
}

export function LadderList({ symbol, pricePrecision = 2 }: ListProps) {
  const queryClient = useQueryClient()
  const { data: ladders, isLoading } = useLadders()
  const [expanded, setExpanded] = useState<string | null>(null)
  const [editPrice, setEditPrice] = useState<Record<string, string>>({})

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: LADDER_QUERY_KEY })
    queryClient.invalidateQueries({ queryKey: ['orders'] })
  }

  const cancelMut = useMutation({
    mutationFn: (id: string) => ladderApi.cancel(id),
    onSuccess: () => {
      toast('success', '阶梯单已撤销（仓位保留）')
      refresh()
    },
    onError: () => toast('error', '撤销失败'),
  })
  const flattenMut = useMutation({
    mutationFn: (id: string) => ladderApi.flatten(id),
    onSuccess: () => {
      toast('success', '已下发一键全平')
      refresh()
    },
    onError: () => toast('error', '全平失败'),
  })
  const amendMut = useMutation({
    mutationFn: ({ id, body }: { id: string; body: LadderAmendRequest }) => ladderApi.amend(id, body),
    onSuccess: () => {
      toast('success', '改价已提交')
      refresh()
    },
    onError: (e: unknown) => {
      const msg = (e as { response?: { data?: { error?: string } } })?.response?.data?.error
      toast('error', msg || '改价失败')
    },
  })

  const rows = useMemo(
    () => (ladders ?? []).filter((l) => !symbol || l.symbol === symbol),
    [ladders, symbol]
  )

  if (isLoading) {
    return <div className="py-6 text-center text-muted-foreground text-xs">加载中…</div>
  }
  if (rows.length === 0) {
    return (
      <div className="py-8 text-center text-muted-foreground text-xs" data-testid="ladder-empty">
        暂无阶梯单
      </div>
    )
  }

  const amendLeg = (lad: LadderOrder, kind: 'entry' | 'target', index: number) => {
    const key = `${lad.id}-${kind}-${index}`
    const price = parseFloat(editPrice[key] ?? '')
    if (!(price > 0)) {
      toast('error', '请输入有效价格')
      return
    }
    const body: LadderAmendRequest =
      kind === 'entry' ? { entries: [{ index, price }] } : { targets: [{ index, price }] }
    amendMut.mutate({ id: lad.id, body })
    setEditPrice((m) => ({ ...m, [key]: '' }))
  }

  return (
    <div className="divide-y divide-quant-border/50" data-testid="ladder-list">
      {rows.map((lad) => {
        const prog = ladderProgress(lad)
        const active = lad.status === 'active'
        const open = expanded === lad.id
        return (
          <div key={lad.id} className="px-3 py-2">
            <div className="flex items-center gap-2 text-[11px]">
              <Layers className="w-3.5 h-3.5 text-quant-gold shrink-0" />
              <span className="font-mono">{lad.symbol}</span>
              <span className={cn('font-bold', lad.side === 'BUY' ? 'text-[#0ECB81]' : 'text-[#F6465D]')}>
                {lad.side === 'BUY' ? '买入' : '卖出'}
              </span>
              <span
                className={cn(
                  'px-1.5 rounded text-[10px]',
                  active ? 'bg-quant-gold/15 text-quant-gold' : 'bg-quant-bg-tertiary text-muted-foreground'
                )}
              >
                {ladderStatusLabel(lad.status)}
              </span>
              <span className="text-muted-foreground">
                {lad.entries.length}档/{lad.targets.length}目标
              </span>
              <span className="flex-1" />
              <span className="font-mono text-muted-foreground">
                入场 {lad.filled_qty.toFixed(4)}/{lad.total_qty.toFixed(4)}
              </span>
              <span className="font-mono text-muted-foreground">已止盈 {(prog.closedPct * 100).toFixed(0)}%</span>
              {lad.avg_entry > 0 && (
                <span className="font-mono">均价 {formatLinePrice(lad.avg_entry, pricePrecision)}</span>
              )}
              {lad.current_sl > 0 && (
                <span className="font-mono text-[#F6465D]">
                  SL {formatLinePrice(lad.current_sl, pricePrecision)}
                  {lad.breakeven_armed ? '(保本)' : ''}
                </span>
              )}
              {active && (
                <>
                  <button
                    onClick={() => cancelMut.mutate(lad.id)}
                    className="px-2 py-0.5 rounded border border-quant-border text-muted-foreground hover:text-foreground hover:border-foreground/30"
                  >
                    撤销
                  </button>
                  <button
                    onClick={() => {
                      if (window.confirm(`确认市价全平 ${lad.symbol} 阶梯单剩余仓位？`)) flattenMut.mutate(lad.id)
                    }}
                    className="px-2 py-0.5 rounded bg-[#F6465D]/15 text-[#F6465D] border border-[#F6465D]/40 hover:bg-[#F6465D]/25"
                  >
                    全平
                  </button>
                </>
              )}
              <button onClick={() => setExpanded(open ? null : lad.id)} className="p-0.5 text-muted-foreground">
                {open ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
              </button>
            </div>
            {/* 入场进度条 */}
            <div className="mt-1.5 h-1 rounded bg-quant-bg-tertiary overflow-hidden">
              <div
                className="h-full bg-[#4A9CFF] transition-all"
                style={{ width: `${(prog.filledPct * 100).toFixed(1)}%` }}
              />
            </div>
            {open && (
              <div className="mt-2 grid grid-cols-2 gap-2 text-[10px]">
                <div>
                  <div className="text-muted-foreground mb-1">入场档</div>
                  {lad.entries.map((e, i) => {
                    const key = `${lad.id}-entry-${i}`
                    const amendable = active && !(e.filled > 0) && e.status !== 'cancelled'
                    return (
                      <div key={i} className="flex items-center gap-1 py-0.5 font-mono">
                        <span className="text-muted-foreground">#{i + 1}</span>
                        <span>{formatLinePrice(e.price, pricePrecision)}</span>
                        <span className="text-muted-foreground">
                          {e.filled}/{e.qty}
                        </span>
                        <span className="text-muted-foreground">{ladderLegStatusLabel(e.status)}</span>
                        {amendable && (
                          <span className="flex items-center gap-0.5 ml-auto">
                            <input
                              value={editPrice[key] ?? ''}
                              placeholder="改价"
                              onChange={(ev) => setEditPrice((m) => ({ ...m, [key]: ev.target.value }))}
                              className="w-16 bg-quant-bg border border-quant-border rounded px-1 h-5 text-[10px] font-mono"
                            />
                            <button
                              onClick={() => amendLeg(lad, 'entry', i)}
                              className="px-1 rounded border border-quant-border text-quant-gold"
                            >
                              改
                            </button>
                          </span>
                        )}
                      </div>
                    )
                  })}
                </div>
                <div>
                  <div className="text-muted-foreground mb-1">止盈目标</div>
                  {lad.targets.map((t, i) => {
                    const key = `${lad.id}-target-${i}`
                    const amendable = active && !(t.filled > 0) && t.status !== 'cancelled'
                    return (
                      <div key={i} className="flex items-center gap-1 py-0.5 font-mono">
                        <span className="text-muted-foreground">T{i + 1}</span>
                        <span className="text-[#0ECB81]">{formatLinePrice(t.price, pricePrecision)}</span>
                        <span className="text-muted-foreground">{t.close_pct}%</span>
                        <span className="text-muted-foreground">
                          {t.filled}/{t.assigned || 0}
                        </span>
                        <span className="text-muted-foreground">{ladderLegStatusLabel(t.status)}</span>
                        {amendable && (
                          <span className="flex items-center gap-0.5 ml-auto">
                            <input
                              value={editPrice[key] ?? ''}
                              placeholder="改价"
                              onChange={(ev) => setEditPrice((m) => ({ ...m, [key]: ev.target.value }))}
                              className="w-16 bg-quant-bg border border-quant-border rounded px-1 h-5 text-[10px] font-mono"
                            />
                            <button
                              onClick={() => amendLeg(lad, 'target', i)}
                              className="px-1 rounded border border-quant-border text-quant-gold"
                            >
                              改
                            </button>
                          </span>
                        )}
                      </div>
                    )
                  })}
                  {lad.fail_reason && <div className="text-[#F6465D] mt-1">{lad.fail_reason}</div>}
                </div>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

/* 右列整合面板:创建 / 列表 小页签切换。 */
export function LadderPanel({
  symbol,
  currentPrice,
  pricePrecision,
  onClose,
}: {
  symbol: string
  currentPrice: number
  pricePrecision: number
  onClose?: () => void
}) {
  const [tab, setTab] = useState<'create' | 'list'>('create')
  const { data: ladders } = useLadders()
  const activeCount = (ladders ?? []).filter((l) => l.status === 'active').length
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-1 bg-quant-bg p-0.5 rounded">
        <button
          onClick={() => setTab('create')}
          className={cn(
            'flex-1 py-1 text-[11px] rounded',
            tab === 'create' ? 'bg-quant-bg-secondary text-foreground' : 'text-muted-foreground'
          )}
        >
          创建阶梯单
        </button>
        <button
          onClick={() => setTab('list')}
          className={cn(
            'flex-1 py-1 text-[11px] rounded',
            tab === 'list' ? 'bg-quant-bg-secondary text-foreground' : 'text-muted-foreground'
          )}
        >
          进行中{activeCount > 0 ? ` (${activeCount})` : ''}
        </button>
        {onClose && (
          <button onClick={onClose} className="px-1.5 text-muted-foreground hover:text-foreground">
            <X className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
      {tab === 'create' ? (
        <LadderCreateForm
          symbol={symbol}
          currentPrice={currentPrice}
          pricePrecision={pricePrecision}
          onCreated={() => setTab('list')}
        />
      ) : (
        <LadderList symbol={symbol} pricePrecision={pricePrecision} />
      )}
    </div>
  )
}
