import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CalendarClock, Play, Plus, Square, Trash2 } from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { dcaBotApi } from '@/lib/api'
import type { DCABot, DCABotDetail, DCABotPayload } from '@/lib/api'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { toast } from '@/lib/useToast'

/* ── DCA 定投机器人管理页（A1.2）──
   列表 / 创建表单 / 详情（持仓+定投记录），对标 QuantDinger DCA bot。 */

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

function statusText(b: DCABot) {
  if (b.is_running || b.status === 'running') return { text: '● 运行中', cls: 'text-quant-green' }
  if (b.status === 'finished') return { text: '◆ 已完成', cls: 'text-quant-gold' }
  return { text: '○ 已停止', cls: 'text-muted-foreground' }
}

export function DCABots() {
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [actionId, setActionId] = useState<string | null>(null)

  const { data: bots = [], isLoading } = useQuery({
    queryKey: ['dca-bots'],
    queryFn: dcaBotApi.list,
    refetchInterval: 8000,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['dca-bots'] })

  const runAction = async (bot: DCABot, action: () => Promise<unknown>, ok: string, errPrefix: string) => {
    setActionId(bot.id)
    try {
      await action()
      toast('success', ok)
      await invalidate()
    } catch (e) {
      toast('error', `${errPrefix}: ${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setActionId(null)
    }
  }

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <div className="flex items-center gap-3 bg-quant-card border border-quant-border rounded-xl px-4 py-2 flex-wrap">
          <span className="font-bold text-sm flex items-center gap-1.5">
            <CalendarClock className="w-4 h-4 text-quant-gold" /> DCA 定投机器人
          </span>
          <span className="text-[11px] text-muted-foreground">按固定间隔买入固定金额，达标止盈 / 硬止损</span>
          <span className="flex-1" />
          <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setCreating(true)}>
            新建定投
          </Button>
        </div>

        <SectionCard title="机器人列表" headerAction={<span className="text-xs text-[#8a8a8a]">{bots.length} 个</span>}>
          {isLoading ? (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-32 rounded-xl" />
              ))}
            </div>
          ) : bots.length === 0 ? (
            <EmptyState
              icon={<CalendarClock className="w-8 h-8" />}
              title="暂无定投机器人"
              description="点击右上角「新建定投」创建"
              actionLabel="新建定投"
              onAction={() => setCreating(true)}
            />
          ) : (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
              {bots.map((bot) => {
                const st = statusText(bot)
                const isLoadingAction = actionId === bot.id
                return (
                  <div
                    key={bot.id}
                    className={cn(
                      'bg-quant-card border border-quant-border rounded-xl p-3 transition-all hover:border-quant-gold/20',
                      !bot.is_running && bot.status !== 'running' && 'opacity-80'
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-bold text-xs text-foreground truncate">{bot.name || '未命名'}</span>
                      <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px] shrink-0">
                        {bot.exchange === 'paper' || !bot.exchange ? '模拟盘' : '实盘'}
                      </span>
                    </div>
                    <div className="text-muted-foreground mt-1 text-[10px] truncate">
                      {bot.symbol} · 每 {bot.interval_minutes} 分钟买 {bot.quote_amount} USDT
                    </div>
                    <div className="flex items-center justify-between mt-2">
                      <span
                        className={cn(
                          'text-sm font-bold',
                          bot.realized_pnl >= 0 ? 'text-quant-green' : 'text-quant-red'
                        )}
                      >
                        {bot.realized_pnl >= 0 ? '+' : ''}${formatCurrency(bot.realized_pnl)}
                      </span>
                      <span className={cn('text-[10px]', st.cls)}>{st.text}</span>
                    </div>
                    <div className="text-[10px] text-muted-foreground mt-1">
                      已投 {bot.filled_orders} 单 · 累计 ${formatCurrency(bot.total_invested)}
                      {bot.avg_price > 0 ? ` · 持仓均价 ${bot.avg_price}` : ''}
                    </div>
                    <div className="flex gap-1 mt-2">
                      {bot.is_running || bot.status === 'running' ? (
                        <button
                          className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50"
                          disabled={isLoadingAction}
                          onClick={() => runAction(bot, () => dcaBotApi.stop(bot.id), '已停止', '停止失败')}
                        >
                          <Square className="w-3 h-3 inline mr-1" />停止
                        </button>
                      ) : (
                        <button
                          className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50"
                          disabled={isLoadingAction || bot.status === 'finished'}
                          onClick={() => runAction(bot, () => dcaBotApi.start(bot.id), '已启动', '启动失败')}
                        >
                          <Play className="w-3 h-3 inline mr-1" />启动
                        </button>
                      )}
                      <button
                        className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
                        onClick={() => setDetailId(bot.id)}
                      >
                        详情
                      </button>
                      <button
                        className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-quant-red hover:border-quant-red/30 transition-colors disabled:opacity-50"
                        disabled={isLoadingAction}
                        onClick={() => {
                          if (!confirm(`确定删除定投机器人 "${bot.name}"？`)) return
                          runAction(bot, () => dcaBotApi.remove(bot.id), '已删除', '删除失败')
                        }}
                      >
                        <Trash2 className="w-3 h-3 inline mr-1" />删除
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </SectionCard>
      </div>

      {creating && <DCACreateModal onClose={() => setCreating(false)} onCreated={invalidate} />}
      {detailId && <DCADetailModal id={detailId} onClose={() => setDetailId(null)} />}
    </div>
  )
}

/* ── 创建表单 ── */
function DCACreateModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => Promise<unknown> }) {
  const [name, setName] = useState('')
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [quoteAmount, setQuoteAmount] = useState('100')
  const [intervalMinutes, setIntervalMinutes] = useState('60')
  const [maxOrders, setMaxOrders] = useState('0')
  const [periodBudget, setPeriodBudget] = useState('0')
  const [takeProfitPct, setTakeProfitPct] = useState('5')
  const [stopLossPct, setStopLossPct] = useState('10')
  const [trailing, setTrailing] = useState(false)

  const createMutation = useMutation({
    mutationFn: (payload: DCABotPayload) => dcaBotApi.create(payload),
    onSuccess: async () => {
      await onCreated()
      toast('success', '定投机器人创建成功')
      onClose()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : '创建失败'),
  })

  const handleSubmit = () => {
    const quote = parseFloat(quoteAmount)
    const interval = parseInt(intervalMinutes, 10)
    const maxOrd = parseInt(maxOrders, 10)
    const budget = parseFloat(periodBudget)
    const tp = parseFloat(takeProfitPct) / 100
    const sl = parseFloat(stopLossPct) / 100
    if (!name.trim()) return toast('warning', '请填写机器人名称')
    if (!symbol.trim()) return toast('warning', '请填写交易对')
    if (!isFinite(quote) || quote <= 0) return toast('warning', '每单金额必须大于 0')
    if (!isFinite(interval) || interval < 1) return toast('warning', '定投间隔至少 1 分钟')
    if (!isFinite(maxOrd) || maxOrd < 0) return toast('warning', '最大订单数不能为负（0=不限）')
    if (!isFinite(budget) || budget < 0) return toast('warning', '周期预算不能为负（0=不限）')
    if (!isFinite(tp) || tp < 0 || tp >= 1) return toast('warning', '止盈比例需在 0-100 之间')
    if (!isFinite(sl) || sl < 0 || sl >= 1) return toast('warning', '止损比例需在 0-100 之间')
    createMutation.mutate({
      name: name.trim(),
      symbol: symbol.trim().toUpperCase(),
      quote_amount: quote,
      interval_minutes: interval,
      max_orders: maxOrd,
      period_budget: budget,
      take_profit_pct: tp,
      stop_loss_pct: sl,
      trailing_enabled: trailing,
    })
  }

  return (
    <ModalShell title="新建 DCA 定投机器人" subtitle="按固定间隔自动买入固定金额（只做多）" onClose={onClose}>
      <div className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">名称</label>
            <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} placeholder="例如：BTC 每日定投" />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">交易对</label>
            <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className={inputCls} placeholder="BTCUSDT" />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">每单金额 (USDT)</label>
            <input type="number" value={quoteAmount} onChange={(e) => setQuoteAmount(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">定投间隔（分钟）</label>
            <input type="number" value={intervalMinutes} onChange={(e) => setIntervalMinutes(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">最大订单数（0=不限）</label>
            <input type="number" value={maxOrders} onChange={(e) => setMaxOrders(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">周期预算 (USDT，0=不限)</label>
            <input type="number" value={periodBudget} onChange={(e) => setPeriodBudget(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">止盈比例 (%)</label>
            <input type="number" value={takeProfitPct} onChange={(e) => setTakeProfitPct(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">止损比例 (%)</label>
            <input type="number" value={stopLossPct} onChange={(e) => setStopLossPct(e.target.value)} className={inputCls} />
          </div>
        </div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
          <input type="checkbox" checked={trailing} onChange={(e) => setTrailing(e.target.checked)} className="accent-quant-gold" />
          启用追踪止盈（进入盈利区后按最高点回撤止盈）
        </label>
        <div className="flex items-center gap-2 pt-1">
          <Button variant="primary" className="flex-1" isLoading={createMutation.isPending} onClick={handleSubmit}>
            创建定投机器人
          </Button>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
        </div>
      </div>
    </ModalShell>
  )
}

/* ── 详情（持仓 + 定投记录）── */
function DCADetailModal({ id, onClose }: { id: string; onClose: () => void }) {
  const { data: bot, isLoading } = useQuery({
    queryKey: ['dca-bots', id],
    queryFn: () => dcaBotApi.get(id),
    refetchInterval: 5000,
  })

  return (
    <ModalShell title={bot?.name || '定投详情'} subtitle={bot?.symbol ?? ''} onClose={onClose} wide>
      {isLoading || !bot ? (
        <Skeleton className="h-40 rounded-xl" />
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            {[
              { label: '累计投入', value: `$${formatCurrency(bot.total_invested)}` },
              { label: '已实现盈亏', value: `${bot.realized_pnl >= 0 ? '+' : ''}$${formatCurrency(bot.realized_pnl)}`, color: bot.realized_pnl >= 0 ? 'text-quant-green' : 'text-quant-red' },
              { label: '持仓数量', value: bot.base_qty > 0 ? bot.base_qty.toFixed(6) : '-' },
              { label: '持仓均价', value: bot.avg_price > 0 ? bot.avg_price : '-' },
            ].map((k) => (
              <div key={k.label} className="p-3 rounded-lg bg-quant-bg border border-quant-border">
                <div className="text-[10px] text-muted-foreground">{k.label}</div>
                <div className={cn('text-sm font-bold font-mono', k.color || 'text-foreground')}>{k.value}</div>
              </div>
            ))}
          </div>
          <div className="text-[11px] text-muted-foreground">
            已成交 {bot.filled_orders} 单 · 每 {bot.interval_minutes} 分钟买 {bot.quote_amount} USDT · 止盈{' '}
            {(bot.take_profit_pct * 100).toFixed(1)}% · 止损 {(bot.stop_loss_pct * 100).toFixed(1)}%
            {bot.trailing_enabled ? ' · 追踪止盈' : ''}
          </div>
          <div>
            <div className="text-xs font-semibold mb-2">定投记录</div>
            {bot.orders.length === 0 ? (
              <div className="text-[11px] text-muted-foreground py-4 text-center">暂无成交</div>
            ) : (
              <div className="max-h-64 overflow-y-auto space-y-1">
                {bot.orders.map((o) => (
                  <div key={o.id} className="flex items-center justify-between text-[11px] px-3 py-1.5 rounded bg-quant-bg border border-quant-border">
                    <span className={o.side === 'buy' ? 'text-quant-green' : 'text-quant-red'}>
                      {o.side === 'buy' ? '买入' : '卖出'} {o.quantity.toFixed(6)}
                    </span>
                    <span className="text-muted-foreground">
                      价 {o.price} · ${formatCurrency(o.quote_qty)}
                      {o.reason ? ` · ${o.reason}` : ''}
                    </span>
                    <span className="text-muted-foreground">{new Date(o.ts).toLocaleString()}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </ModalShell>
  )
}

/* ── 通用弹层壳（对齐 BotsCenter 惯例）── */
function ModalShell({
  title,
  subtitle,
  wide,
  onClose,
  children,
}: {
  title: string
  subtitle?: string
  wide?: boolean
  onClose: () => void
  children: React.ReactNode
}) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
      tabIndex={-1}
    >
      <div
        role="document"
        className={cn(
          'w-full flex flex-col rounded-2xl border border-quant-border bg-quant-card shadow-2xl overflow-hidden',
          wide ? 'max-w-3xl max-h-[90vh]' : 'max-w-xl max-h-[85vh]'
        )}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-6 py-4 border-b border-quant-border shrink-0">
          <div>
            <h3 className="text-sm font-bold">{title}</h3>
            {subtitle && <p className="text-[10px] text-muted-foreground mt-0.5">{subtitle}</p>}
          </div>
          <button
            onClick={onClose}
            aria-label="关闭"
            className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
          >
            ✕
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-6">{children}</div>
      </div>
    </div>
  )
}

export default DCABots
