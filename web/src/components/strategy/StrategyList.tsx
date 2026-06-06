import { useState, useMemo, memo } from 'react'
import type { StrategyItem } from '@/types'
import { cn } from '@/lib/utils'
import { VirtualList } from '@/components/VirtualList'
import { EmptyState } from '@/components/ui/EmptyState'
import {
  Search, Plus, ChevronRight, ChevronDown, MoreVertical,
  Play, Pause, Edit3, Trash2, DollarSign, Clock, FolderOpen
} from 'lucide-react'

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
    case 'running': return 'bg-quant-green/10 text-quant-green border-quant-green/20'
    case 'error': return 'bg-quant-red/10 text-quant-red border-quant-red/20'
    default: return 'bg-quant-bg-tertiary text-muted-foreground border-quant-border'
  }
}

export function getStatusDot(status: string) {
  switch (status) {
    case 'running': return 'bg-quant-green'
    case 'error': return 'bg-quant-red'
    default: return 'bg-muted-foreground'
  }
}

/* ─── StatusBadge ─── */
export function StatusBadge({ status }: { status: string }) {
  return (
    <span className={cn('inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded border', getStatusColor(status))}>
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
      <button onClick={() => setOpen((v) => !v)} className="p-1 rounded hover:bg-quant-hover text-muted-foreground hover:text-foreground">
        <MoreVertical className="w-3.5 h-3.5" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-10" onClick={() => setOpen(false)} onKeyDown={(e) => { if (e.key === 'Escape') setOpen(false) }} tabIndex={-1} role="presentation" />
          <div className="absolute right-0 top-full mt-1 w-32 rounded-lg border border-quant-border bg-quant-card shadow-lg z-20 py-1">
            <button onClick={() => { onAction('startAll'); setOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-green">
              <Play className="w-3 h-3" /> 全部启动
            </button>
            <button onClick={() => { onAction('stopAll'); setOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-orange">
              <Pause className="w-3 h-3" /> 全部停止
            </button>
            <div className="border-t border-quant-border my-1" />
            <button onClick={() => { onAction('deleteAll'); setOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-red">
              <Trash2 className="w-3 h-3" /> 全部删除
            </button>
          </div>
        </>
      )}
    </div>
  )
}

/* ─── StrategyListItem ─── */
const StrategyListItem = memo(function StrategyListItem({ strategy, selected, onSelect, onStart, onStop, onEdit, onDelete }: {
  strategy: StrategyItem; selected: boolean; onSelect: () => void; onStart: () => void; onStop: () => void; onEdit: () => void; onDelete: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  return (
    <div onClick={onSelect} className={cn(
      'flex items-center justify-between gap-2 px-3 py-2.5 rounded-md cursor-pointer transition-all border',
      selected
        ? 'bg-quant-gold/5 border-quant-gold/30 border-l-2 border-l-quant-gold'
        : 'bg-quant-bg border-transparent hover:bg-quant-bg-tertiary hover:border-quant-border'
    )}>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium truncate">{strategy.name}</span>
          {strategy.ai_generated && <span className="text-[10px] px-1 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">AI</span>}
          {strategy.mode === 'script' && <span className="text-[10px] px-1 rounded bg-green-500/10 text-green-400 border border-green-500/20">脚本</span>}
        </div>
        <div className="flex items-center gap-2 mt-1">
          <span className="text-[10px] text-muted-foreground flex items-center gap-1"><DollarSign className="w-3 h-3" />{strategy.symbol || '-'}</span>
          <span className="text-[10px] text-muted-foreground flex items-center gap-1"><Clock className="w-3 h-3" />{strategy.timeframe || '-'}</span>
          <StatusBadge status={strategy.status} />
        </div>
      </div>
      <div className="relative shrink-0" onClick={(e) => e.stopPropagation()}>
        <button onClick={() => setMenuOpen((v) => !v)} className="p-1 rounded hover:bg-quant-hover text-muted-foreground hover:text-foreground">
          <MoreVertical className="w-3.5 h-3.5" />
        </button>
        {menuOpen && (
          <>
            <div className="fixed inset-0 z-10" onClick={() => setMenuOpen(false)} onKeyDown={(e) => { if (e.key === 'Escape') setMenuOpen(false) }} tabIndex={-1} role="presentation" />
            <div className="absolute right-0 top-full mt-1 w-32 rounded-lg border border-quant-border bg-quant-card shadow-lg z-20 py-1">
              {strategy.status === 'stopped' && (
                <button onClick={() => { onStart(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-green">
                  <Play className="w-3 h-3" /> 启动
                </button>
              )}
              {strategy.status === 'running' && (
                <button onClick={() => { onStop(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-orange">
                  <Pause className="w-3 h-3" /> 停止
                </button>
              )}
              <button onClick={() => { onEdit(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2">
                <Edit3 className="w-3 h-3" /> 编辑
              </button>
              <div className="border-t border-quant-border my-1" />
              <button onClick={() => { onDelete(); setMenuOpen(false) }} className="w-full px-3 py-1.5 text-xs text-left hover:bg-quant-hover flex items-center gap-2 text-quant-red">
                <Trash2 className="w-3 h-3" /> 删除
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
})

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
}

export function StrategyList({ strategies, isLoading, selectedId, onSelect, onStart, onStop, onEdit, onDelete, onCreate }: StrategyListProps) {
  const [groupBy, setGroupBy] = useState<'strategy' | 'symbol'>('strategy')
  const [search, setSearch] = useState('')
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})

  const filtered = useMemo(() => {
    if (!search.trim()) return strategies
    const q = search.toLowerCase()
    return strategies.filter((s) => s.name.toLowerCase().includes(q) || (s.symbol || '').toLowerCase().includes(q))
  }, [strategies, search])

  const grouped = useMemo(() => {
    if (groupBy === 'symbol') {
      const map = new Map<string, StrategyGroup>()
      filtered.forEach((s) => {
        const sym = s.symbol || '未指定标的'
        if (!map.has(sym)) map.set(sym, { id: `sym_${sym}`, baseName: sym, strategies: [], runningCount: 0, stoppedCount: 0 })
        const g = map.get(sym)!
        g.strategies.push(s)
        if (s.status === 'running') { g.runningCount++ } else { g.stoppedCount++ }
      })
      return { groups: Array.from(map.values()).sort((a, b) => a.baseName.localeCompare(b.baseName)), ungrouped: [] as StrategyItem[] }
    }
    const map = new Map<string, StrategyGroup>()
    filtered.forEach((s) => {
      const gid = s.group_id || ''
      if (gid) {
        if (!map.has(gid)) map.set(gid, { id: gid, baseName: s.group_name || s.name.split('-')[0] || '默认分组', strategies: [], runningCount: 0, stoppedCount: 0 })
        const g = map.get(gid)!
        g.strategies.push(s)
        if (s.status === 'running') { g.runningCount++ } else { g.stoppedCount++ }
      }
    })
    const groupedIds = new Set(Array.from(map.keys()))
    const ungrouped = filtered.filter((s) => !s.group_id || !groupedIds.has(s.group_id))
    return { groups: Array.from(map.values()), ungrouped }
  }, [filtered, groupBy])

  const toggleGroup = (id: string) => setCollapsed((p) => ({ ...p, [id]: !p[id] }))

  const handleGroupAction = (action: 'startAll' | 'stopAll' | 'deleteAll', group: StrategyGroup) => {
    const ids = group.strategies.map((s) => s.id)
    if (action === 'startAll') ids.forEach((id) => onStart(id))
    if (action === 'stopAll') ids.forEach((id) => onStop(id))
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
        <button onClick={onCreate} aria-label="创建策略" className="shrink-0 px-2.5 py-2 rounded-lg bg-quant-gold/10 text-quant-gold border border-quant-gold/20 hover:bg-quant-gold/20 transition-colors">
          <Plus className="w-4 h-4" />
        </button>
      </div>

      <div className="px-3 py-2 border-b border-quant-border flex items-center gap-2">
        <span className="text-[11px] text-muted-foreground">分组:</span>
        <div className="flex rounded-md border border-quant-border overflow-hidden">
          <button onClick={() => setGroupBy('strategy')} className={cn('px-2.5 py-1 text-[11px] transition-colors', groupBy === 'strategy' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
            <FolderOpen className="w-3 h-3 inline mr-1" />策略
          </button>
          <button onClick={() => setGroupBy('symbol')} className={cn('px-2.5 py-1 text-[11px] transition-colors border-l border-quant-border', groupBy === 'symbol' ? 'bg-quant-gold/10 text-quant-gold' : 'text-muted-foreground hover:text-foreground')}>
            <DollarSign className="w-3 h-3 inline mr-1" />标的
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-2">
        {isLoading && <div className="text-center text-xs text-muted-foreground py-8">加载中...</div>}
        {!isLoading && strategies.length === 0 && (
          <div className="px-2 py-6 text-center">
            <EmptyState title="暂无策略" description="点击右上角 + 创建策略" actionLabel="创建策略" onAction={onCreate} />
          </div>
        )}

        {grouped.groups.map((g) => (
          <div key={g.id} className="rounded-lg border border-quant-border overflow-hidden">
            <div onClick={() => toggleGroup(g.id)} className="flex items-center justify-between px-3 py-2 bg-quant-bg-tertiary cursor-pointer hover:bg-quant-hover transition-colors">
              <div className="flex items-center gap-2 min-w-0">
                {collapsed[g.id] ? <ChevronRight className="w-3.5 h-3.5 text-muted-foreground shrink-0" /> : <ChevronDown className="w-3.5 h-3.5 text-muted-foreground shrink-0" />}
                <span className="text-xs font-semibold truncate">{g.baseName}</span>
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-quant-bg border border-quant-border text-muted-foreground">{g.strategies.length}</span>
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
                        selected={selectedId === s.id}
                        onSelect={() => onSelect(s.id)}
                        onStart={() => onStart(s.id)}
                        onStop={() => onStop(s.id)}
                        onEdit={() => onEdit(s)}
                        onDelete={() => { if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id) }}
                      />
                    )}
                  />
                ) : (
                  <div className="space-y-1">
                    {g.strategies.map((s) => (
                      <StrategyListItem key={s.id} strategy={s} selected={selectedId === s.id} onSelect={() => onSelect(s.id)} onStart={() => onStart(s.id)} onStop={() => onStop(s.id)} onEdit={() => onEdit(s)} onDelete={() => { if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id) }} />
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
                    selected={selectedId === s.id}
                    onSelect={() => onSelect(s.id)}
                    onStart={() => onStart(s.id)}
                    onStop={() => onStop(s.id)}
                    onEdit={() => onEdit(s)}
                    onDelete={() => { if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id) }}
                  />
                )}
              />
            ) : (
              <div className="space-y-1">
                {grouped.ungrouped.map((s) => (
                  <StrategyListItem key={s.id} strategy={s} selected={selectedId === s.id} onSelect={() => onSelect(s.id)} onStart={() => onStart(s.id)} onStop={() => onStop(s.id)} onEdit={() => onEdit(s)} onDelete={() => { if (confirm(`删除策略 "${s.name}"？`)) onDelete(s.id) }} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
