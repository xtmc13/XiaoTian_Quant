import { useState, useMemo, memo } from 'react'
import { useNavigate } from 'react-router-dom'
import type { StrategyItem } from '@/types'
import { cn } from '@/lib/utils'
import { VirtualList } from '@/components/VirtualList'
import { STRAT_TYPES } from './StrategyFormFields'
import { decimalToPercent, PERCENTAGE_FIELD_THRESHOLDS } from '@/lib/craPercentUtils'
import {
  Search,
  Plus,
  ChevronRight,
  ChevronDown,
  MoreVertical,
  Play,
  Pause,
  Edit3,
  Trash2,
  DollarSign,
  Clock,
  X,
  LayoutGrid,
  TrendingUp,
  Bot,
} from 'lucide-react'

/** P2-10 空状态模板卡预设：点击"用此模板创建"预填 strategy_type 与保守默认 config。 */
export interface StrategyTemplatePreset {
  strategyType: string
  config?: Record<string, unknown>
}

const CREATE_TEMPLATES: {
  key: string
  title: string
  desc: string
  strategyType?: string
  config?: Record<string, unknown>
  href?: string
}[] = [
  {
    key: 'cra_contract',
    title: '合约网格',
    desc: 'CRA 合约网格：首单 100U · 5 档补仓 · 10x 杠杆（模拟盘）',
    strategyType: 'cra_contract',
    config: {
      first_order_amount: 100,
      first_order_multiplier: 1,
      order_count: 5,
      enable_add_position: true,
      add_positions: [1, 2, 3, 4, 5].map((order) => ({
        order,
        multiplier: [1, 1, 2, 2, 4][order - 1],
        spread: [0.03, 0.04, 0.05, 0.07, 0.09][order - 1],
        callback: 0.005,
        ema_enabled: false,
      })),
      trade_count_mode: 'cycle',
      loop_count: 100,
      take_profit_method: 'full',
      tp_mode: 'static',
      take_profit_ratio: 0.013,
      profit_callback: 0.003,
      waterfall_enabled: true,
      waterfall_protection: 0.02,
      stop_loss_enabled: true,
      stop_loss_type: 'ratio',
      stop_loss_ratio: 0.4,
      leverage: 10,
      direction: 'long',
      market_type: 'swap',
      margin_mode: 'cross',
    },
  },
  {
    key: 'spot_trend',
    title: '现货趋势',
    desc: '马丁趋势现货策略：首单 100U · 5 档补仓 · 稳健参数（模拟盘）',
    strategyType: 'martin_trend',
    config: {
      first_order_amount: 100,
      first_order_multiplier: 1,
      order_count: 5,
      enable_add_position: true,
      add_positions: [1, 2, 3, 4, 5].map((order) => ({
        order,
        multiplier: [1, 1, 2, 2, 4][order - 1],
        spread: [0.035, 0.05, 0.07, 0.09, 0.11][order - 1],
        callback: 0.005,
      })),
      trade_count_mode: 'cycle',
      loop_count: 100,
      take_profit_method: 'full',
      tp_mode: 'static',
      take_profit_ratio: 0.02,
      profit_callback: 0.003,
      waterfall_enabled: true,
      waterfall_protection: 0.02,
      direction: 'long',
      market_type: 'spot',
      margin_mode: 'cross',
    },
  },
  {
    key: 'martin_bot',
    title: '马丁格尔机器人',
    desc: '经典马丁格尔机器人已迁移至机器人页，支持更多风控选项',
    href: '/bots',
  },
]

type MarketType = 'spot' | 'contract'
type StatusFilter = 'all' | 'running' | 'stopped' | 'detecting' | 'history'

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'running', label: '执行中' },
  { value: 'stopped', label: '未启动' },
  { value: 'detecting', label: '检测中' },
  { value: 'history', label: '执行历史' },
]

/* ─── Types ─── */
export interface StrategyGroup {
  id: string
  baseName: string
  strategies: StrategyItem[]
  runningCount: number
  stoppedCount: number
}

/* ─── Helpers ─── */
export function getStatusColor(status: string) {
  switch (status) {
    case 'running':
      return 'bg-quant-green/10 text-quant-green border-quant-green/20'
    case 'error':
      return 'bg-quant-red/10 text-quant-red border-quant-red/20'
    default:
      return 'bg-quant-bg-tertiary text-muted-foreground border-quant-border'
  }
}

export function getStatusDot(status: string) {
  switch (status) {
    case 'running':
      return 'bg-quant-green'
    case 'error':
      return 'bg-quant-red'
    default:
      return 'bg-muted-foreground'
  }
}

/* ─── StatusBadge ─── */
export function StatusBadge({ status }: { status: string }) {
  return (
    <span
      className={cn('inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border', getStatusColor(status))}
    >
      <span className={cn('w-1.5 h-1.5 rounded-full', getStatusDot(status))} />
      {status === 'running' ? '运行中' : status === 'error' ? '异常' : '已停止'}
    </span>
  )
}

/* ─── GroupDropdown ─── */
function GroupDropdown({ onAction }: { onAction: (action: 'startAll' | 'stopAll' | 'deleteAll') => void }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="relative" onClick={(e) => e.stopPropagation()}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="p-1 rounded hover:bg-quant-hover text-muted-foreground hover:text-foreground"
      >
        <MoreVertical className="w-3.5 h-3.5" />
      </button>
      {open && (
        <>
          <div
            className="fixed inset-0 z-10"
            onClick={() => setOpen(false)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') setOpen(false)
            }}
            tabIndex={-1}
            role="presentation"
          />
          <div className="absolute right-0 top-full mt-1 w-32 rounded-lg border border-quant-border bg-quant-card shadow-lg z-20 py-1">
            <button
              onClick={() => {
                onAction('startAll')
                setOpen(false)
              }}
              className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-green"
            >
              <Play className="w-3 h-3" /> 全部启动
            </button>
            <button
              onClick={() => {
                onAction('stopAll')
                setOpen(false)
              }}
              className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-orange"
            >
              <Pause className="w-3 h-3" /> 全部停止
            </button>
            <div className="border-t border-quant-border my-1" />
            <button
              onClick={() => {
                onAction('deleteAll')
                setOpen(false)
              }}
              className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-red"
            >
              <Trash2 className="w-3 h-3" /> 全部删除
            </button>
          </div>
        </>
      )}
    </div>
  )
}

/* ─── StrategyListItem ─── */
const StrategyListItem = memo(function StrategyListItem({
  strategy,
  selected,
  onSelect,
  onStart,
  onStop,
  onEdit,
  onDelete,
  startError,
  onDismissStartError,
}: {
  strategy: StrategyItem
  selected: boolean
  onSelect: () => void
  onStart: () => void
  onStop: () => void
  onEdit: () => void
  onDelete: () => void
  startError?: string
  onDismissStartError?: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)

  // P1-6 卡片风险摘要：杠杆·保证金·止损·止盈模式（config_json 缺字段则跳过）。
  const riskSummary = useMemo(() => {
    let cfg: Record<string, unknown> = {}
    if (strategy.config_json) {
      try {
        cfg = JSON.parse(strategy.config_json) as Record<string, unknown>
      } catch {
        cfg = {}
      }
    }
    const parts: string[] = []
    const lev = cfg.leverage ?? strategy.leverage
    if (typeof lev === 'number' && lev > 0) parts.push(`杠杆${lev}x`)
    const margin = (cfg.margin_mode as string) || (strategy as { margin_mode?: string }).margin_mode
    if (margin === 'cross') parts.push('全仓')
    else if (margin === 'isolated') parts.push('逐仓')
    const slRaw = cfg.stop_loss_ratio
    if (typeof slRaw === 'number' && slRaw > 0) {
      parts.push(`止损${decimalToPercent(slRaw, PERCENTAGE_FIELD_THRESHOLDS.stopLossRatio).toFixed(0)}%`)
    }
    const tpMode = cfg.tp_mode as string
    if (tpMode === 'static') parts.push('静态止盈')
    else if (tpMode === 'moving') parts.push('移动止盈')
    return parts
  }, [strategy.config_json, strategy.leverage])

  return (
    <div
      onClick={onSelect}
      className={cn(
        'px-3 py-2.5 rounded-md cursor-pointer transition-all border',
        selected
          ? 'bg-quant-gold/5 border-quant-gold/30 border-l-2 border-l-quant-gold'
          : 'bg-quant-bg border-transparent hover:bg-quant-bg-tertiary hover:border-quant-border'
      )}
    >
      {/* P0-3 启动失败持久化反馈（会话级，成功启动后由父组件自动清除） */}
      {startError && (
        <div
          className="flex items-start gap-1.5 mb-2 -mt-0.5 px-2 py-1.5 rounded bg-quant-red/10 border border-quant-red/30 text-[10px] text-quant-red"
          onClick={(e) => e.stopPropagation()}
        >
          <span className="min-w-0 break-words flex-1">
            <span className="font-semibold">启动失败：</span>
            {startError}
          </span>
          {onDismissStartError && (
            <button
              onClick={onDismissStartError}
              aria-label="关闭启动失败提示"
              className="shrink-0 p-0.5 rounded hover:bg-quant-red/20 transition-colors"
            >
              <X className="w-3 h-3" />
            </button>
          )}
        </div>
      )}
      <div className="flex items-center justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium truncate">{strategy.name}</span>
            {strategy.ai_generated && (
              <span className="text-[10px] px-1 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">
                AI
              </span>
            )}
            {strategy.mode === 'script' && (
              <span className="text-[10px] px-1 rounded bg-green-500/10 text-green-400 border border-green-500/20">
                脚本
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 mt-1">
            <span className="text-[10px] text-muted-foreground flex items-center gap-1">
              <DollarSign className="w-3 h-3" />
              {strategy.symbol || '-'}
            </span>
            <span className="text-[10px] text-muted-foreground flex items-center gap-1">
              <Clock className="w-3 h-3" />
              {strategy.timeframe || '-'}
            </span>
            <StatusBadge status={strategy.status} />
          </div>
          {riskSummary.length > 0 && (
            <div className="text-[10px] text-muted-foreground/80 mt-1 truncate">{riskSummary.join(' · ')}</div>
          )}
        </div>
      <div className="relative shrink-0" onClick={(e) => e.stopPropagation()}>
        <button
          onClick={() => setMenuOpen((v) => !v)}
          className="p-1 rounded hover:bg-quant-hover text-muted-foreground hover:text-foreground"
        >
          <MoreVertical className="w-3.5 h-3.5" />
        </button>
        {menuOpen && (
          <>
            <div
              className="fixed inset-0 z-10"
              onClick={() => setMenuOpen(false)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') setMenuOpen(false)
              }}
              tabIndex={-1}
              role="presentation"
            />
            <div className="absolute right-0 top-full mt-1 w-32 rounded-lg border border-quant-border bg-quant-card shadow-lg z-20 py-1">
              {strategy.status === 'stopped' && (
                <button
                  onClick={() => {
                    onStart()
                    setMenuOpen(false)
                  }}
                  className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-green"
                >
                  <Play className="w-3 h-3" /> 启动
                </button>
              )}
              {strategy.status === 'running' && (
                <button
                  onClick={() => {
                    onStop()
                    setMenuOpen(false)
                  }}
                  className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-orange"
                >
                  <Pause className="w-3 h-3" /> 停止
                </button>
              )}
              <button
                onClick={() => {
                  onEdit()
                  setMenuOpen(false)
                }}
                className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2"
              >
                <Edit3 className="w-3 h-3" /> 编辑
              </button>
              <div className="border-t border-quant-border my-1" />
              <button
                onClick={() => {
                  onDelete()
                  setMenuOpen(false)
                }}
                className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-red"
              >
                <Trash2 className="w-3 h-3" /> 删除
              </button>
            </div>
          </>
        )}
        </div>
      </div>
    </div>
  )
})

/* ─── TemplateCard (P2-10 空状态模板引导) ─── */
function TemplateCard({
  template,
  onCreateTemplate,
}: {
  template: (typeof CREATE_TEMPLATES)[number]
  onCreateTemplate?: (preset: StrategyTemplatePreset) => void
}) {
  const navigate = useNavigate()
  return (
    <div className="rounded-xl border border-quant-border bg-quant-bg p-3 hover:border-quant-gold/30 transition-colors">
      <div className="flex items-start gap-2.5">
        <div className="w-8 h-8 rounded-lg bg-quant-gold/10 text-quant-gold flex items-center justify-center shrink-0">
          {template.href ? <Bot className="w-4 h-4" /> : template.key === 'cra_contract' ? (
            <LayoutGrid className="w-4 h-4" />
          ) : (
            <TrendingUp className="w-4 h-4" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-xs font-semibold text-foreground">{template.title}</div>
          <div className="text-[10px] text-muted-foreground leading-relaxed mt-0.5">{template.desc}</div>
        </div>
      </div>
      {template.href ? (
        <button
          onClick={() => navigate(template.href!)}
          className="mt-2.5 w-full px-3 py-2 rounded-lg border border-quant-border text-xs text-muted-foreground hover:text-foreground hover:border-quant-gold/30 transition-colors"
        >
          前往机器人页
        </button>
      ) : (
        <button
          onClick={() =>
            onCreateTemplate?.({ strategyType: template.strategyType!, config: template.config })
          }
          className="mt-2.5 w-full px-3 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 text-xs font-medium hover:bg-quant-gold/20 transition-colors"
        >
          用此模板创建
        </button>
      )}
    </div>
  )
}

/* ─── StrategyList ─── */
interface StrategyListProps {
  strategies: StrategyItem[]
  isLoading: boolean
  selectedId: string | null
  onSelect: (id: string) => void
  onStart: (id: string) => void
  onStop: (id: string) => void
  onEdit: (strategy: StrategyItem) => void
  onDelete: (id: string) => void
  onCreate: () => void
  onCreateType?: (type: string) => void
  /** P0-3：启动失败错误（strategyId → 错误信息），卡片上显示红色横条。 */
  startErrors?: Record<string, string>
  onDismissStartError?: (id: string) => void
  /** P1-8：批量启停（分组操作走批量接口并 toast 汇总）。 */
  onBatchStart?: (ids: string[]) => void
  onBatchStop?: (ids: string[]) => void
  /** P2-10：空状态模板卡"用此模板创建"。 */
  onCreateTemplate?: (preset: StrategyTemplatePreset) => void
}

export function StrategyList({
  strategies,
  isLoading,
  selectedId,
  onSelect,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onCreate,
  onCreateType,
  startErrors,
  onDismissStartError,
  onBatchStart,
  onBatchStop,
  onCreateTemplate,
}: StrategyListProps) {
  const [groupBy, setGroupBy] = useState<'strategy' | 'symbol'>('strategy')
  const [search, setSearch] = useState('')
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [marketType, setMarketType] = useState<MarketType>('spot')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    const currentMarket = marketType
    return strategies.filter((s) => {
      const market: MarketType = ['swap', 'futures', 'margin'].includes(s.market_type as string) ? 'contract' : 'spot'
      const matchesMarket = market === currentMarket
      const matchesSearch = !q || s.name.toLowerCase().includes(q) || (s.symbol || '').toLowerCase().includes(q)
      const matchesStatus =
        statusFilter === 'all' ||
        (statusFilter === 'running' && (s.status === 'running' || s.status === 'error')) ||
        (statusFilter === 'history' && s.status === 'stopped' && s.total_pnl != null) ||
        s.status === statusFilter
      return matchesMarket && matchesSearch && matchesStatus
    })
  }, [strategies, search, marketType, statusFilter])

  const grouped = useMemo(() => {
    if (groupBy === 'symbol') {
      const map = new Map<string, StrategyGroup>()
      filtered.forEach((s) => {
        const sym = s.symbol || '未指定标的'
        if (!map.has(sym))
          map.set(sym, { id: `sym_${sym}`, baseName: sym, strategies: [], runningCount: 0, stoppedCount: 0 })
        const g = map.get(sym)!
        g.strategies.push(s)
        if (s.status === 'running') {
          g.runningCount++
        } else {
          g.stoppedCount++
        }
      })
      return {
        groups: Array.from(map.values()).sort((a, b) => a.baseName.localeCompare(b.baseName)),
        ungrouped: [] as StrategyItem[],
      }
    }
    const map = new Map<string, StrategyGroup>()
    filtered.forEach((s) => {
      const gid = s.group_id || ''
      if (gid) {
        if (!map.has(gid))
          map.set(gid, {
            id: gid,
            baseName: s.group_name || s.name.split('-')[0] || '默认分组',
            strategies: [],
            runningCount: 0,
            stoppedCount: 0,
          })
        const g = map.get(gid)!
        g.strategies.push(s)
        if (s.status === 'running') {
          g.runningCount++
        } else {
          g.stoppedCount++
        }
      }
    })
    const groupedIds = new Set(Array.from(map.keys()))
    const ungrouped = filtered.filter((s) => !s.group_id || !groupedIds.has(s.group_id))
    return { groups: Array.from(map.values()), ungrouped }
  }, [filtered, groupBy])

  const toggleGroup = (id: string) => setCollapsed((p) => ({ ...p, [id]: !p[id] }))

  const handleGroupAction = (action: 'startAll' | 'stopAll' | 'deleteAll', group: StrategyGroup) => {
    const ids = group.strategies.map((s) => s.id)
    if (action === 'startAll') {
      if (onBatchStart) onBatchStart(ids)
      else ids.forEach((id) => onStart(id))
    }
    if (action === 'stopAll') {
      if (onBatchStop) onBatchStop(ids)
      else ids.forEach((id) => onStop(id))
    }
    if (action === 'deleteAll') {
      if (confirm(`确定删除分组 "${group.baseName}" 下的 ${ids.length} 个策略？`)) ids.forEach((id) => onDelete(id))
    }
  }

  return (
    <div className="hidden md:flex w-80 shrink-0 border-r border-quant-border bg-quant-bg-secondary flex-col">
      <div className="p-3 border-b border-quant-border flex items-center justify-between gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="搜索策略..."
            className="w-full bg-quant-bg border border-quant-border rounded-lg pl-8 pr-3 py-2 text-xs focus:outline-none focus:border-quant-gold"
          />
        </div>
        <button
          onClick={onCreate}
          aria-label="创建策略"
          className="shrink-0 px-2.5 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 hover:bg-quant-gold/20 transition-colors"
        >
          <Plus className="w-4 h-4" />
        </button>
      </div>

      <div className="px-3 py-2 border-b border-quant-border grid grid-cols-2 gap-2">
        <div>
          <label className="text-[10px] text-muted-foreground mb-1 block">市场</label>
          <select
            value={marketType}
            onChange={(e) => setMarketType(e.target.value as MarketType)}
            className="w-full bg-quant-bg border border-quant-border rounded-lg px-2 py-1.5 text-[11px] focus:outline-none focus:border-quant-gold"
          >
            <option value="spot">现货策略</option>
            <option value="contract">合约策略</option>
          </select>
        </div>
        <div>
          <label className="text-[10px] text-muted-foreground mb-1 block">状态</label>
          <select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
            className="w-full bg-quant-bg border border-quant-border rounded-lg px-2 py-1.5 text-[11px] focus:outline-none focus:border-quant-gold"
          >
            {STATUS_OPTIONS.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div
        className={cn(
          'px-3 py-2 border-b border-quant-border bg-quant-bg-secondary',
          filtered.length === 0 && 'flex-1 flex flex-col'
        )}
      >
        <label className="text-[10px] text-muted-foreground mb-1 block">策略类型</label>
        <div className={cn('space-y-1 overflow-y-auto', filtered.length === 0 ? 'flex-1' : 'max-h-48')}>
          {STRAT_TYPES[marketType].map((t) => (
            <button
              key={t.value}
              onClick={() => (onCreateType ? onCreateType(t.value) : onCreate())}
              className="w-full text-left px-2.5 py-2 rounded-md bg-quant-bg border border-quant-border text-xs font-medium text-foreground hover:border-quant-gold/30 hover:bg-quant-bg-tertiary transition-colors"
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      <div className={cn('overflow-y-auto p-2 space-y-2', filtered.length > 0 ? 'flex-1' : 'h-0')}>
        {isLoading && <div className="text-center text-xs text-muted-foreground py-8">加载中...</div>}

        {/* P2-10 空状态模板引导 */}
        {!isLoading && strategies.length === 0 && (
          <div className="py-4 px-1 space-y-3">
            <div className="text-[11px] text-muted-foreground text-center">还没有策略，从模板快速开始</div>
            {CREATE_TEMPLATES.map((t) => (
              <TemplateCard key={t.key} template={t} onCreateTemplate={onCreateTemplate} />
            ))}
          </div>
        )}
        {!isLoading && strategies.length > 0 && filtered.length === 0 && (
          <div className="text-center text-xs text-muted-foreground py-8">无匹配的策略</div>
        )}

        {grouped.groups.map((g) => (
          <div key={g.id} className="rounded-lg border border-quant-border overflow-hidden">
            <div
              onClick={() => toggleGroup(g.id)}
              className="flex items-center justify-between px-3 py-2 bg-quant-bg-tertiary cursor-pointer hover:bg-quant-hover transition-colors"
            >
              <div className="flex items-center gap-2 min-w-0">
                {collapsed[g.id] ? (
                  <ChevronRight className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                ) : (
                  <ChevronDown className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                )}
                <span className="text-xs font-semibold truncate">{g.baseName}</span>
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg border border-quant-border text-muted-foreground">
                  {g.strategies.length}
                </span>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {g.runningCount > 0 && <span className="text-[10px] text-quant-green">{g.runningCount} 运行</span>}
                {g.stoppedCount > 0 && <span className="text-[10px] text-quant-red">{g.stoppedCount} 停止</span>}
                <GroupDropdown onAction={(a) => handleGroupAction(a, g)} />
              </div>
            </div>
            {!collapsed[g.id] && (
              <div className="p-1.5">
                {g.strategies.length > 10 ? (
                  <VirtualList
                    items={g.strategies}
                    itemHeight={44}
                    containerHeight={220}
                    overscan={2}
                    renderItem={(s) => (
                      <StrategyListItem
                        strategy={s}
                        startError={startErrors?.[s.id]}
                        onDismissStartError={onDismissStartError ? () => onDismissStartError(s.id) : undefined}
                        selected={selectedId === s.id}
                        onSelect={() => onSelect(s.id)}
                        onStart={() => onStart(s.id)}
                        onStop={() => onStop(s.id)}
                        onEdit={() => onEdit(s)}
                        onDelete={() => {
                          if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id)
                        }}
                      />
                    )}
                  />
                ) : (
                  <div className="space-y-1">
                    {g.strategies.map((s) => (
                      <StrategyListItem
                        key={s.id}
                        strategy={s}
                        startError={startErrors?.[s.id]}
                        onDismissStartError={onDismissStartError ? () => onDismissStartError(s.id) : undefined}
                        selected={selectedId === s.id}
                        onSelect={() => onSelect(s.id)}
                        onStart={() => onStart(s.id)}
                        onStop={() => onStop(s.id)}
                        onEdit={() => onEdit(s)}
                        onDelete={() => {
                          if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id)
                        }}
                      />
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        ))}

        {grouped.ungrouped.length > 0 && (
          <div>
            {grouped.ungrouped.length > 10 ? (
              <VirtualList
                items={grouped.ungrouped}
                itemHeight={44}
                containerHeight={220}
                overscan={2}
                renderItem={(s) => (
                  <StrategyListItem
                    strategy={s}
                    startError={startErrors?.[s.id]}
                    onDismissStartError={onDismissStartError ? () => onDismissStartError(s.id) : undefined}
                    selected={selectedId === s.id}
                    onSelect={() => onSelect(s.id)}
                    onStart={() => onStart(s.id)}
                    onStop={() => onStop(s.id)}
                    onEdit={() => onEdit(s)}
                    onDelete={() => {
                      if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id)
                    }}
                  />
                )}
              />
            ) : (
              <div className="space-y-1">
                {grouped.ungrouped.map((s) => (
                  <StrategyListItem
                    key={s.id}
                    strategy={s}
                    startError={startErrors?.[s.id]}
                    onDismissStartError={onDismissStartError ? () => onDismissStartError(s.id) : undefined}
                    selected={selectedId === s.id}
                    onSelect={() => onSelect(s.id)}
                    onStart={() => onStart(s.id)}
                    onStop={() => onStop(s.id)}
                    onEdit={() => onEdit(s)}
                    onDelete={() => {
                      if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id)
                    }}
                  />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
