import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { strategyApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import {
  Pause,
  Code,
  LayoutDashboard,
  Plus,
  Activity,
  TrendingUp,
  Sparkles,
  X,
  Layers,
  ChevronRight as ChevronRightIcon,
} from 'lucide-react'
import { StatusBadge, getStatusDot } from '@/components/strategy/StrategyList'
import { StrategyEditor } from '@/components/strategy/StrategyEditor'
import type { StrategyItem } from '@/types'

/* ─── Constants ─── */
const TABS = [
  { key: 'overview', label: '概览', icon: LayoutDashboard },
  { key: 'code', label: '代码编辑器', icon: Code },
] as const

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
  const navigate = useNavigate()
  const [tab, setTab] = useState<(typeof TABS)[number]['key']>('overview')
  const [guideDismissed, setGuideDismissed] = useLocalStorage('strategy-guide-dismissed', false)

  return (
    <div className="h-full flex flex-col min-w-0">
      {/* Top Guide Bar */}
      {!guideDismissed && <GuideBar onDismiss={() => setGuideDismissed(true)} onCreate={() => navigate('/strategies')} />}

      {/* Tabs */}
      <div className="flex border-b border-quant-border px-2 shrink-0">
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={cn(
              'flex items-center gap-1.5 px-4 py-2.5 text-xs font-medium transition-colors relative',
              tab === t.key ? 'text-quant-gold' : 'text-muted-foreground hover:text-foreground'
            )}
          >
            <t.icon className="w-3.5 h-3.5" />
            {t.label}
            {tab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-quant-gold" />}
          </button>
        ))}
      </div>

      {/* Content */}
      <div className="flex-1 overflow-hidden">
        {tab === 'overview' && <OverviewTab />}
        {tab === 'code' && <StrategyEditor />}
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════════════════════════
   Guide Bar
   ═══════════════════════════════════════════════════════════════ */
function GuideBar({ onDismiss, onCreate }: { onDismiss: () => void; onCreate: () => void }) {
  const steps = [
    { idx: 1, title: '选择策略类型', desc: '从指标信号、脚本代码或AI生成中选择适合的模式' },
    { idx: 2, title: '配置交易参数', desc: '设置交易对、杠杆、风控及通知渠道' },
    { idx: 3, title: '启动实盘或信号', desc: '连接交易所API，开启自动交易或仅接收信号' },
  ]
  return (
    <div className="shrink-0 mx-4 mt-3 mb-2 rounded-2xl border border-quant-gold/10 bg-gradient-to-r from-quant-gold/5 to-purple-500/5 px-5 py-4 flex flex-wrap gap-4 items-center">
      <div className="flex-1 min-w-[200px]">
        <div className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-quant-gold/10 text-quant-gold text-[11px] font-semibold mb-2">
          <Sparkles className="w-3 h-3" /> 快速入门
        </div>
        <div className="text-sm font-bold text-foreground">三步创建你的第一个量化策略</div>
        <div className="text-xs text-muted-foreground mt-1">
          跟随向导完成策略配置，支持指标信号、代码脚本和AI生成三种模式
        </div>
      </div>
      <div className="flex gap-3 flex-1 min-w-[300px]">
        {steps.map((s) => (
          <div
            key={s.idx}
            className="flex-1 flex items-start gap-2.5 rounded-xl bg-quant-bg/60 border border-quant-border/40 p-3"
          >
            <div className="w-6 h-6 rounded-full bg-gradient-to-br from-quant-gold to-purple-500 flex items-center justify-center text-[10px] font-bold text-white shrink-0">
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
          onClick={onCreate}
          className="px-3 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 text-xs font-medium hover:bg-quant-gold/20 transition-colors flex items-center gap-1.5"
        >
          <Plus className="w-3.5 h-3.5" /> 创建策略
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
   Overview Tab
   ═══════════════════════════════════════════════════════════════ */
function OverviewTab() {
  const navigate = useNavigate()
  const { data: strategies, isLoading } = useQuery({ queryKey: ['strategies'], queryFn: () => strategyApi.list() })
  const list = (strategies || []) as StrategyItem[]
  const running = list.filter((s) => s.status === 'running').length
  const stopped = list.filter((s) => s.status === 'stopped').length
  const totalPnl = list.reduce((sum, s) => sum + (s.total_pnl || 0), 0)

  return (
    <div className="h-full overflow-y-auto p-6 space-y-5">
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
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
              value={totalPnl >= 0 ? `+$${totalPnl.toFixed(2)}` : `-$${Math.abs(totalPnl).toFixed(2)}`}
              color={totalPnl >= 0 ? 'text-quant-green' : 'text-quant-red'}
            />
          </>
        )}
      </div>
      <SectionCard
        title="最近活跃策略"
        headerAction={
          list.length > 5 ? (
            <button
              onClick={() => navigate('/strategies')}
              className="flex items-center gap-0.5 text-[10px] text-muted-foreground transition-colors hover:text-foreground"
            >
              查看全部 <ChevronRightIcon className="h-3 w-3" />
            </button>
          ) : undefined
        }
      >
        {isLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} variant="rect" height={44} />
            ))}
          </div>
        ) : list.length === 0 ? (
          <EmptyState title="暂无策略" description="前往 指标IDE 创建你的第一个策略" />
        ) : (
          <div className="space-y-2">
            {list.slice(0, 5).map((s) => (
              <div
                key={s.id}
                className="flex items-center justify-between px-3 py-2.5 rounded-lg bg-quant-bg-tertiary border border-quant-border hover:border-quant-gold/20 transition-colors"
              >
                <div className="flex items-center gap-3 min-w-0">
                  <span className={cn('w-2 h-2 rounded-full', getStatusDot(s.status))} />
                  <span className="text-xs font-medium truncate">{s.name}</span>
                  <span className="text-[10px] text-muted-foreground">{s.symbol}</span>
                </div>
                <div className="flex items-center gap-3 text-xs">
                  <span className={cn(s.total_pnl && s.total_pnl >= 0 ? 'text-quant-green' : 'text-quant-red')}>
                    {s.total_pnl != null ? formatCurrency(s.total_pnl) : '-'}
                  </span>
                  <StatusBadge status={s.status} />
                </div>
              </div>
            ))}
          </div>
        )}
      </SectionCard>
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

