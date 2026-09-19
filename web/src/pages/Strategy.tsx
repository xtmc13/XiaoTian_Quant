import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { strategyApi } from '@/lib/api'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import { toast } from '@/lib/useToast'
import type { StrategyItem } from '@/types'
import {
  Pause,
  Plus,
  Activity,
  TrendingUp,
  Sparkles,
  X,
  Layers,
  Play,
  Search,
  Trash2,
} from 'lucide-react'
import { StatusBadge, getStatusDot } from '@/components/strategy/StrategyList'

/* ─── Helpers ─── */
function useLocalStorage<T>(key: string, initial: T): [T, (v: T) => void] {
  const [val, setVal] = useState<T>(() => {
    try {
      return JSON.parse(localStorage.getItem(key) || 'null') ?? initial
    } catch {
      return initial
    }
  })
  useEffect(() => {
    localStorage.setItem(key, JSON.stringify(val))
  }, [key, val])
  return [val, setVal]
}

/* ─── Main Component ─── */
export function Strategy() {
  const [guideDismissed, setGuideDismissed] = useLocalStorage('strategy-guide-dismissed', false)

  return (
    <div className="h-full flex flex-col min-w-0">
      {/* Top Guide Bar */}
      {!guideDismissed && <GuideBar onDismiss={() => setGuideDismissed(true)} />}
      {/* Content */}
      <div className="flex-1 overflow-hidden">
        <StrategyManager />
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Guide Bar
   ═══════════════════════════════════════════════════════════════ */
function GuideBar({ onDismiss }: { onDismiss: () => void }) {
  const navigate = useNavigate()
  const steps = [
    { idx: 1, title: '编写或生成策略', desc: '在指标 IDE 用 AI 生成指标与策略代码' },
    { idx: 2, title: '回测验证', desc: '在历史数据上验证策略表现与风险' },
    { idx: 3, title: '部署运行', desc: '一键部署为实盘 / 模拟盘自动运行' },
  ]
  return (
    <div className="shrink-0 flex items-center gap-4 px-5 py-3 border-b border-quant-border bg-quant-bg-secondary">
      <div className="flex items-center gap-2 shrink-0">
        <Sparkles className="w-4 h-4 text-quant-gold" />
        <span className="text-sm font-semibold">快速入门</span>
      </div>
      <div className="flex-1 grid grid-cols-1 md:grid-cols-3 gap-2">
        {steps.map((s) => (
          <div
            key={s.idx}
            className="flex-1 flex items-start gap-2.5 rounded-xl bg-quant-bg/60 border border-quant-border/40 p-3"
          >
            <div className="w-6 h-6 rounded-full bg-gradient-to-br from-quant-gold to-purple-500 flex items-center justify-center text-[10px] font-bold text-foreground shrink-0">
              {s.idx}
            </div>
            <div className="min-w-0">
              <div className="text-xs font-semibold text-foreground">{s.title}</div>
              <div className="text-[10px] text-muted-foreground leading-relaxed mt-0.5">{s.desc}</div>
            </div>
          </div>
        ))}
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <button
          onClick={() => navigate('/create')}
          className="px-3 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 text-xs font-medium hover:bg-quant-gold/20 transition-colors flex items-center gap-1.5"
        >
          <Plus className="w-3.5 h-3.5" /> 启动策略机器人
        </button>
        <button
          onClick={onDismiss}
          aria-label="关闭提示"
          className="w-8 h-8 rounded-lg border border-quant-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Strategy Manager
   ═══════════════════════════════════════════════════════════════ */
type StatusFilter = 'all' | 'running' | 'stopped'

const STATUS_FILTERS: { key: StatusFilter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'running', label: '运行中' },
  { key: 'stopped', label: '已停止' },
]

function StrategyManager() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null)

  const { data: strategies, isLoading } = useQuery({ queryKey: ['strategies'], queryFn: () => strategyApi.list() })
  const list = (strategies || []) as StrategyItem[]
  const running = list.filter((s) => s.status === 'running').length
  const stopped = list.filter((s) => s.status === 'stopped').length
  const totalPnl = list.reduce((sum, s) => sum + (s.total_pnl || 0), 0)

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['strategies'] })

  const handleToggle = async (s: StrategyItem) => {
    setActionLoadingId(s.id)
    try {
      if (s.status === 'running') {
        await strategyApi.stop(s.id)
        toast('success', `已停止: ${s.name}`)
      } else {
        await strategyApi.start(s.id)
        toast('success', `已启动: ${s.name}`)
      }
      refresh()
    } catch (e: unknown) {
      toast('error', '操作失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setActionLoadingId(null)
    }
  }

  const handleDelete = async (s: StrategyItem) => {
    if (!confirm(`确定删除策略「${s.name}」？该操作不可恢复。`)) return
    setActionLoadingId(s.id)
    try {
      await strategyApi.delete(s.id)
      toast('success', `已删除: ${s.name}`)
      refresh()
    } catch (e: unknown) {
      toast('error', '删除失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setActionLoadingId(null)
    }
  }

  const countOf = (f: StatusFilter) =>
    f === 'all' ? list.length : list.filter((s) => s.status === f).length

  const filtered = list.filter((s) => {
    if (statusFilter === 'running' && s.status !== 'running') return false
    if (statusFilter === 'stopped' && s.status !== 'stopped') return false
    if (search) {
      const q = search.toLowerCase()
      if (!s.name.toLowerCase().includes(q) && !(s.symbol || '').toLowerCase().includes(q)) return false
    }
    return true
  })

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1200px] space-y-5">
        {/* 统计 */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          {isLoading ? (
            Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} variant="card" height={72} />)
          ) : (
            <>
              <StatCard icon={Layers} label="总策略数" value={String(list.length)} />
              <StatCard icon={Activity} label="运行中" value={String(running)} color="text-quant-green" />
              <StatCard icon={Pause} label="已停止" value={String(stopped)} color="text-muted-foreground" />
              <StatCard
                icon={TrendingUp}
                label="总盈亏"
                value={`${totalPnl >= 0 ? '+' : '-'}$${formatCurrency(Math.abs(totalPnl))}`}
                color={totalPnl >= 0 ? 'text-quant-green' : 'text-quant-red'}
              />
            </>
          )}
        </div>

        {/* 工具条 */}
        <div className="flex items-center gap-3 bg-quant-card border border-quant-border rounded-xl px-4 py-2 flex-wrap">
          <span className="font-bold text-sm flex items-center gap-1.5">
            <Layers className="w-4 h-4 text-quant-gold" /> 策略管理
          </span>
          <div className="relative flex-1 min-w-[140px] max-w-xs">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索名称 / 交易对"
              className="w-full bg-quant-bg border border-quant-border rounded-lg pl-8 pr-3 py-1.5 text-xs focus:outline-none focus:border-quant-gold"
            />
          </div>
          <span className="flex-1" />
          <button
            onClick={() => navigate('/create')}
            className="px-3 py-2 rounded-lg bg-quant-gold text-black text-xs font-medium hover:opacity-90 transition-opacity flex items-center gap-1.5"
          >
            <Plus className="w-3.5 h-3.5" /> 启动策略机器人
          </button>
        </div>

        {/* 状态筛选 */}
        <div className="flex flex-wrap items-center gap-2">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.key}
              onClick={() => setStatusFilter(f.key)}
              className={cn(
                'px-3 py-1.5 rounded-full text-xs font-medium border transition-colors',
                statusFilter === f.key
                  ? 'bg-quant-gold/10 text-quant-gold border-quant-gold/30'
                  : 'border-quant-border text-muted-foreground hover:text-foreground hover:border-quant-gold/20'
              )}
            >
              {f.label}
              <span className="ml-1.5 text-[10px] opacity-70">{countOf(f.key)}</span>
            </button>
          ))}
        </div>

        {/* 策略列表 */}
        <SectionCard title="策略列表" headerAction={<span className="text-xs text-muted-foreground">{filtered.length} 个</span>}>
          {isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} variant="rect" height={44} />
              ))}
            </div>
          ) : list.length === 0 ? (
            <EmptyState
              icon={<Layers className="w-8 h-8" />}
              title="暂无策略"
              description="前往 指标IDE 创建你的第一个策略"
              actionLabel="前往 指标IDE"
              onAction={() => navigate('/indicator-ide')}
            />
          ) : filtered.length === 0 ? (
            <EmptyState title="没有匹配的策略" description="换个关键词或切换状态筛选试试" />
          ) : (
            <div className="space-y-2">
              {filtered.map((s) => {
                const busy = actionLoadingId === s.id
                const pnl = s.total_pnl ?? 0
                return (
                  <div
                    key={s.id}
                    className="flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg bg-quant-bg-tertiary border border-quant-border hover:border-quant-gold/20 transition-colors"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <span className={cn('w-2 h-2 rounded-full shrink-0', getStatusDot(s.status))} />
                      <span className="text-xs font-medium truncate">{s.name}</span>
                      {s.strategy_type && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg-secondary text-muted-foreground shrink-0">
                          {s.strategy_type}
                        </span>
                      )}
                      {s.symbol && (
                        <span className="text-[10px] text-muted-foreground shrink-0 hidden sm:inline">
                          {s.symbol}
                          {s.timeframe ? ` · ${s.timeframe}` : ''}
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-3 shrink-0">
                      <div className="text-right w-24">
                        <div
                          className={cn(
                            'text-xs font-mono',
                            s.total_pnl != null ? (pnl >= 0 ? 'text-quant-green' : 'text-quant-red') : 'text-muted-foreground'
                          )}
                        >
                          {s.total_pnl != null ? `${pnl >= 0 ? '+' : '-'}$${formatCurrency(Math.abs(pnl))}` : '-'}
                        </div>
                        {s.total_pnl_percent != null && (
                          <div
                            className={cn(
                              'text-[10px] font-mono',
                              s.total_pnl_percent >= 0 ? 'text-quant-green/70' : 'text-quant-red/70'
                            )}
                          >
                            {formatPercent(s.total_pnl_percent)}
                          </div>
                        )}
                      </div>
                      <StatusBadge status={s.status} />
                      <div className="flex items-center gap-0.5">
                        {s.status === 'running' ? (
                          <button
                            onClick={() => handleToggle(s)}
                            disabled={busy}
                            title="停止"
                            aria-label="停止"
                            className="p-1.5 rounded text-muted-foreground hover:text-quant-gold hover:bg-white/5 disabled:opacity-40 transition-colors"
                          >
                            <Pause className="w-3.5 h-3.5" />
                          </button>
                        ) : (
                          <button
                            onClick={() => handleToggle(s)}
                            disabled={busy}
                            title="启动"
                            aria-label="启动"
                            className="p-1.5 rounded text-muted-foreground hover:text-quant-green hover:bg-white/5 disabled:opacity-40 transition-colors"
                          >
                            <Play className="w-3.5 h-3.5" />
                          </button>
                        )}
                        <button
                          onClick={() => handleDelete(s)}
                          disabled={busy}
                          title="删除"
                          aria-label="删除"
                          className="p-1.5 rounded text-muted-foreground hover:text-quant-red hover:bg-white/5 disabled:opacity-40 transition-colors"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </SectionCard>
      </div>
    </div>
  )
}

function StatCard({
  icon: Icon,
  label,
  value,
  color,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
  color?: string
}) {
  return (
    <div className="rounded-xl border border-quant-border bg-quant-card p-4 flex items-center gap-3">
      <div className="w-10 h-10 rounded-lg bg-quant-bg-tertiary flex items-center justify-center text-quant-gold">
        <Icon className="w-5 h-5" />
      </div>
      <div>
        <div className={cn('text-lg font-bold', color || 'text-foreground')}>{value}</div>
        <div className="text-[11px] text-muted-foreground">{label}</div>
      </div>
    </div>
  )
}
