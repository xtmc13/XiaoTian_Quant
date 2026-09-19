import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Layers, Play, Plus, Square, Trash2 } from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { layeredMartinApi } from '@/lib/api'
import type { LayeredMartinBot, LayeredMartinBotDetail, LayeredMartinBotPayload } from '@/lib/api'
import { SectionCard } from '@/components/ui/SectionCard'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { toast } from '@/lib/useToast'

/* ── 分层马丁格尔机器人管理页（A1.3）──
   多分组独立马丁循环：每组独立 {quote_amount, multiplier, max_layers,
   budget_cap}，层数/预算硬限 + 成交确认推进，对标 QuantDinger layered
   martingale。 */

const inputCls =
  'w-full bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-quant-gold'

function statusText(b: LayeredMartinBot) {
  if (b.is_running || b.status === 'running') return { text: '● 运行中', cls: 'text-quant-green' }
  if (b.status === 'finished') return { text: '◆ 已完成', cls: 'text-quant-gold' }
  return { text: '○ 已停止', cls: 'text-muted-foreground' }
}

interface GroupDraft {
  quote_amount: string
  multiplier: string
  max_layers: string
  budget_cap: string
}

const defaultGroup = (): GroupDraft => ({ quote_amount: '100', multiplier: '2', max_layers: '5', budget_cap: '0' })

export function LayeredMartinBots() {
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [actionId, setActionId] = useState<string | null>(null)

  const { data: bots = [], isLoading } = useQuery({
    queryKey: ['layered-martin-bots'],
    queryFn: layeredMartinApi.list,
    refetchInterval: 8000,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['layered-martin-bots'] })

  const runAction = async (bot: LayeredMartinBot, action: () => Promise<unknown>, ok: string, errPrefix: string) => {
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
            <Layers className="w-4 h-4 text-quant-gold" /> 分层马丁格尔机器人
          </span>
          <span className="text-[11px] text-muted-foreground">多分组独立马丁循环 · 层数/预算硬限 · 成交确认推进</span>
          <span className="flex-1" />
          <Button variant="primary" size="sm" leftIcon={<Plus className="w-3 h-3" />} onClick={() => setCreating(true)}>
            新建机器人
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
              icon={<Layers className="w-8 h-8" />}
              title="暂无分层马丁机器人"
              description="点击右上角「新建机器人」创建"
              actionLabel="新建机器人"
              onAction={() => setCreating(true)}
            />
          ) : (
            <div className="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-3">
              {bots.map((bot) => {
                const st = statusText(bot)
                const isLoadingAction = actionId === bot.id
                const runningGroups = bot.groups.filter((g) => (g.status === 'running' || (g.layer ?? 0) > 0) && g.status !== 'finished').length
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
                      {bot.symbol} · {bot.groups.length} 组 · 每 {(bot.price_deviation_pct * 100).toFixed(1)}% 加一层
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
                      活跃组 {runningGroups}/{bot.groups.length} · 成交 {bot.total_trades} 笔
                    </div>
                    <div className="flex gap-1 mt-2">
                      {bot.is_running || bot.status === 'running' ? (
                        <button
                          className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50"
                          disabled={isLoadingAction}
                          onClick={() => runAction(bot, () => layeredMartinApi.stop(bot.id), '已停止', '停止失败')}
                        >
                          <Square className="w-3 h-3 inline mr-1" />停止
                        </button>
                      ) : (
                        <button
                          className="flex-1 py-1 rounded bg-quant-bg border border-quant-border text-[11px] text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors disabled:opacity-50"
                          disabled={isLoadingAction || bot.status === 'finished'}
                          onClick={() => runAction(bot, () => layeredMartinApi.start(bot.id), '已启动', '启动失败')}
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
                          if (!confirm(`确定删除机器人 "${bot.name}"？`)) return
                          runAction(bot, () => layeredMartinApi.remove(bot.id), '已删除', '删除失败')
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

      {creating && <LMCreateModal onClose={() => setCreating(false)} onCreated={invalidate} />}
      {detailId && <LMDetailModal id={detailId} onClose={() => setDetailId(null)} />}
    </div>
  )
}

/* ── 创建表单（多分组编辑）── */
function LMCreateModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => Promise<unknown> }) {
  const [name, setName] = useState('')
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [deviation, setDeviation] = useState('3')
  const [takeProfitPct, setTakeProfitPct] = useState('5')
  const [stopLossPct, setStopLossPct] = useState('10')
  const [trailing, setTrailing] = useState(false)
  const [groups, setGroups] = useState<GroupDraft[]>([defaultGroup()])

  const createMutation = useMutation({
    mutationFn: (payload: LayeredMartinBotPayload) => layeredMartinApi.create(payload),
    onSuccess: async () => {
      await onCreated()
      toast('success', '分层马丁机器人创建成功')
      onClose()
    },
    onError: (e) => toast('error', e instanceof Error ? e.message : '创建失败'),
  })

  const patchGroup = (i: number, patch: Partial<GroupDraft>) =>
    setGroups((prev) => prev.map((g, idx) => (idx === i ? { ...g, ...patch } : g)))

  const handleSubmit = () => {
    const dev = parseFloat(deviation) / 100
    const tp = parseFloat(takeProfitPct) / 100
    const sl = parseFloat(stopLossPct) / 100
    if (!name.trim()) return toast('warning', '请填写机器人名称')
    if (!symbol.trim()) return toast('warning', '请填写交易对')
    if (!isFinite(dev) || dev <= 0 || dev >= 1) return toast('warning', '加仓跌幅需在 0-100 之间')
    if (!isFinite(tp) || tp < 0 || tp >= 1) return toast('warning', '止盈比例需在 0-100 之间')
    if (!isFinite(sl) || sl < 0 || sl >= 1) return toast('warning', '止损比例需在 0-100 之间')
    const parsedGroups = groups.map((g, i) => {
      const quote = parseFloat(g.quote_amount)
      const mult = parseFloat(g.multiplier)
      const layers = parseInt(g.max_layers, 10)
      const cap = parseFloat(g.budget_cap)
      if (!isFinite(quote) || quote <= 0) throw new Error(`第 ${i + 1} 组首单金额必须大于 0`)
      if (!isFinite(mult) || mult < 1) throw new Error(`第 ${i + 1} 组倍投系数至少为 1`)
      if (!isFinite(layers) || layers < 1) throw new Error(`第 ${i + 1} 组最大层数至少为 1`)
      if (!isFinite(cap) || cap < 0) throw new Error(`第 ${i + 1} 组预算上限不能为负`)
      return { quote_amount: quote, multiplier: mult, max_layers: layers, budget_cap: cap }
    })
    createMutation.mutate({
      name: name.trim(),
      symbol: symbol.trim().toUpperCase(),
      price_deviation_pct: dev,
      take_profit_pct: tp,
      stop_loss_pct: sl,
      trailing_enabled: trailing,
      groups: parsedGroups,
    })
  }

  return (
    <ModalShell title="新建分层马丁格尔机器人" subtitle="每个分组独立跑一条马丁阶梯" wide onClose={onClose}>
      <div className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">名称</label>
            <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} placeholder="例如：BTC 三层马丁" />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">交易对</label>
            <input value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} className={inputCls} placeholder="BTCUSDT" />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">每层加仓跌幅 (%)</label>
            <input type="number" value={deviation} onChange={(e) => setDeviation(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">止盈比例 (%)</label>
            <input type="number" value={takeProfitPct} onChange={(e) => setTakeProfitPct(e.target.value)} className={inputCls} />
          </div>
          <div>
            <label className="text-[11px] text-muted-foreground mb-1 block">止损比例 (%)</label>
            <input type="number" value={stopLossPct} onChange={(e) => setStopLossPct(e.target.value)} className={inputCls} />
          </div>
          <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer self-end pb-2">
            <input type="checkbox" checked={trailing} onChange={(e) => setTrailing(e.target.checked)} className="accent-quant-gold" />
            追踪止盈
          </label>
        </div>

        <div>
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs font-semibold">分组（{groups.length}/20）</span>
            <Button
              variant="ghost"
              size="sm"
              leftIcon={<Plus className="w-3 h-3" />}
              disabled={groups.length >= 20}
              onClick={() => setGroups((prev) => [...prev, defaultGroup()])}
            >
              添加分组
            </Button>
          </div>
          <div className="space-y-2 max-h-72 overflow-y-auto pr-1">
            {groups.map((g, i) => (
              <div key={i} className="grid grid-cols-2 sm:grid-cols-5 gap-2 p-2 rounded-lg bg-quant-bg border border-quant-border items-end">
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1 block">首单金额</label>
                  <input type="number" value={g.quote_amount} onChange={(e) => patchGroup(i, { quote_amount: e.target.value })} className={inputCls} />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1 block">倍投系数</label>
                  <input type="number" value={g.multiplier} onChange={(e) => patchGroup(i, { multiplier: e.target.value })} className={inputCls} />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1 block">最大层数</label>
                  <input type="number" value={g.max_layers} onChange={(e) => patchGroup(i, { max_layers: e.target.value })} className={inputCls} />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground mb-1 block">预算上限(0=不限)</label>
                  <input type="number" value={g.budget_cap} onChange={(e) => patchGroup(i, { budget_cap: e.target.value })} className={inputCls} />
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={groups.length <= 1}
                  onClick={() => setGroups((prev) => prev.filter((_, idx) => idx !== i))}
                >
                  删除
                </Button>
              </div>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-2 pt-1">
          <Button
            variant="primary"
            className="flex-1"
            isLoading={createMutation.isPending}
            onClick={() => {
              try {
                handleSubmit()
              } catch (e) {
                toast('warning', e instanceof Error ? e.message : '参数错误')
              }
            }}
          >
            创建机器人
          </Button>
          <Button variant="ghost" onClick={onClose}>
            取消
          </Button>
        </div>
      </div>
    </ModalShell>
  )
}

/* ── 详情（每组状态 + 成交记录）── */
function LMDetailModal({ id, onClose }: { id: string; onClose: () => void }) {
  const { data: bot, isLoading } = useQuery({
    queryKey: ['layered-martin-bots', id],
    queryFn: () => layeredMartinApi.get(id),
    refetchInterval: 5000,
  })

  return (
    <ModalShell title={bot?.name || '机器人详情'} subtitle={bot?.symbol ?? ''} onClose={onClose} wide>
      {isLoading || !bot ? (
        <Skeleton className="h-40 rounded-xl" />
      ) : (
        <div className="space-y-4">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            {[
              { label: '已实现盈亏', value: `${bot.realized_pnl >= 0 ? '+' : ''}$${formatCurrency(bot.realized_pnl)}`, color: bot.realized_pnl >= 0 ? 'text-quant-green' : 'text-quant-red' },
              { label: '成交笔数', value: String(bot.total_trades) },
              { label: '分组数', value: String(bot.groups.length) },
              { label: '止盈/止损', value: `${(bot.take_profit_pct * 100).toFixed(1)}% / ${(bot.stop_loss_pct * 100).toFixed(1)}%` },
            ].map((k) => (
              <div key={k.label} className="p-3 rounded-lg bg-quant-bg border border-quant-border">
                <div className="text-[10px] text-muted-foreground">{k.label}</div>
                <div className={cn('text-sm font-bold font-mono', k.color || 'text-foreground')}>{k.value}</div>
              </div>
            ))}
          </div>

          <div>
            <div className="text-xs font-semibold mb-2">分组状态</div>
            <div className="space-y-1">
              {bot.groups.map((g) => (
                <div key={g.id ?? g.group_index} className="flex items-center justify-between text-[11px] px-3 py-2 rounded bg-quant-bg border border-quant-border">
                  <span className="font-semibold">组 {(g.group_index ?? 0) + 1}</span>
                  <span className="text-muted-foreground">
                    层 {g.layer ?? 0}/{g.max_layers} · 已投 ${formatCurrency(g.total_invested ?? 0)}
                    {(g.budget_cap ?? 0) > 0 ? ` / 上限 $${formatCurrency(g.budget_cap ?? 0)}` : ''}
                  </span>
                  <span className="text-muted-foreground">
                    {g.avg_price ? `均价 ${g.avg_price}` : '-'}
                    {g.pending_layer ? ' · 在途' : ''}
                  </span>
                  <span className={g.status === 'finished' ? 'text-quant-gold' : g.status === 'running' ? 'text-quant-green' : 'text-muted-foreground'}>
                    {g.status ?? 'idle'}
                  </span>
                </div>
              ))}
            </div>
          </div>

          <div>
            <div className="text-xs font-semibold mb-2">成交记录</div>
            {bot.orders.length === 0 ? (
              <div className="text-[11px] text-muted-foreground py-4 text-center">暂无成交</div>
            ) : (
              <div className="max-h-64 overflow-y-auto space-y-1">
                {bot.orders.map((o) => (
                  <div key={o.id} className="flex items-center justify-between text-[11px] px-3 py-1.5 rounded bg-quant-bg border border-quant-border">
                    <span className="text-muted-foreground">组 {o.group_index + 1}</span>
                    <span className={o.side === 'buy' ? 'text-quant-green' : 'text-quant-red'}>
                      {o.side === 'buy' ? `买入 L${o.layer}` : '卖出'} {o.quantity.toFixed(6)}
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

/* ── 通用弹层壳（对齐 DCABots/BotsCenter 惯例）── */
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

export default LayeredMartinBots
