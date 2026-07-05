import { Search, Plus, Play, Pause, Trash2, FolderOpen, DollarSign, XOctagon, Pencil } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { StrategyItem } from '@/types'
import { ContractDirectionStats } from './ContractDirectionStats'

export type MarketFilter = 'spot' | 'contract'
export type StatusFilter = 'all' | 'running' | 'stopped' | 'error' | 'history' | 'draft' | 'detecting'

interface StrategyToolbarProps {
  search: string
  onSearchChange: (value: string) => void
  marketFilter: MarketFilter
  onMarketFilterChange: (value: MarketFilter) => void
  statusFilter: StatusFilter
  onStatusFilterChange: (value: StatusFilter) => void
  typeFilter: string
  onTypeFilterChange: (value: string) => void
  strategyTypes: { value: string; label: string }[]
  selectedCount: number
  strategies?: StrategyItem[]
  onBatchStart: () => void
  onBatchStop: () => void
  onBatchClose: () => void
  onBatchDelete: () => void
  onBatchEdit: () => void
  onCreate: () => void
}

export function StrategyToolbar({
  search,
  onSearchChange,
  marketFilter,
  onMarketFilterChange,
  statusFilter,
  onStatusFilterChange,
  typeFilter,
  onTypeFilterChange,
  strategyTypes,
  selectedCount,
  strategies = [],
  onBatchStart,
  onBatchStop,
  onBatchClose,
  onBatchDelete,
  onBatchEdit,
  onCreate,
}: StrategyToolbarProps) {
  const hasSelection = selectedCount > 0

  return (
    <div className="p-3 border-b border-quant-border space-y-3">
      <div className="flex items-center justify-between gap-2">
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
          <input
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
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

      <div className="flex flex-wrap items-center gap-2">
        <div className="flex rounded-md border border-quant-border overflow-hidden">
          <button
            onClick={() => onMarketFilterChange('spot')}
            className={cn(
              'px-2.5 py-1.5 text-[11px] transition-colors flex items-center gap-1',
              marketFilter === 'spot'
                ? 'bg-quant-gold/10 text-quant-gold'
                : 'text-muted-foreground hover:text-foreground'
            )}
          >
            <FolderOpen className="w-3 h-3" />
            现货
          </button>
          <button
            onClick={() => onMarketFilterChange('contract')}
            className={cn(
              'px-2.5 py-1.5 text-[11px] transition-colors flex items-center gap-1 border-l border-quant-border',
              marketFilter === 'contract'
                ? 'bg-quant-gold/10 text-quant-gold'
                : 'text-muted-foreground hover:text-foreground'
            )}
          >
            <DollarSign className="w-3 h-3" />
            合约
          </button>
        </div>

        <select
          value={statusFilter}
          onChange={(e) => onStatusFilterChange(e.target.value as StatusFilter)}
          className="bg-quant-bg border border-quant-border rounded-md px-2 py-1.5 text-[11px] focus:outline-none focus:border-quant-gold"
        >
          <option value="all">全部状态</option>
          <option value="running">执行中</option>
          <option value="stopped">未启动</option>
          <option value="error">异常</option>
          <option value="detecting">检测中</option>
          <option value="history">执行历史</option>
        </select>

        <select
          value={typeFilter}
          onChange={(e) => onTypeFilterChange(e.target.value)}
          className="bg-quant-bg border border-quant-border rounded-md px-2 py-1.5 text-[11px] focus:outline-none focus:border-quant-gold"
        >
          <option value="">全部类型</option>
          {strategyTypes.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
        {marketFilter === 'contract' && strategies.length > 0 && <ContractDirectionStats strategies={strategies} />}
      </div>

      {hasSelection && (
        <div className="flex items-center justify-between gap-2 pt-1 border-t border-quant-border/50">
          <span className="text-[11px] text-muted-foreground">已选 {selectedCount} 项</span>
          <div className="flex items-center gap-1.5">
            <button
              onClick={onBatchStart}
              className="px-2 py-1 rounded text-[11px] bg-quant-green/10 text-quant-green border border-quant-green/20 hover:bg-quant-green/20 transition-colors flex items-center gap-1"
            >
              <Play className="w-3 h-3" /> 启动
            </button>
            <button
              onClick={onBatchStop}
              className="px-2 py-1 rounded text-[11px] bg-quant-orange/10 text-quant-orange border border-quant-orange/20 hover:bg-quant-orange/20 transition-colors flex items-center gap-1"
            >
              <Pause className="w-3 h-3" /> 停止
            </button>
            <button
              onClick={onBatchClose}
              className="px-2 py-1 rounded text-[11px] bg-quant-red/10 text-quant-red border border-quant-red/20 hover:bg-quant-red/20 transition-colors flex items-center gap-1"
            >
              <XOctagon className="w-3 h-3" /> 平仓
            </button>
            <button
              onClick={onBatchEdit}
              className="px-2 py-1 rounded text-[11px] bg-quant-bg-tertiary text-muted-foreground border border-quant-border hover:text-foreground transition-colors flex items-center gap-1"
            >
              <Pencil className="w-3 h-3" /> 修改
            </button>
            <button
              onClick={onBatchDelete}
              className="px-2 py-1 rounded text-[11px] bg-quant-red/10 text-quant-red border border-quant-red/20 hover:bg-quant-red/20 transition-colors flex items-center gap-1"
            >
              <Trash2 className="w-3 h-3" /> 删除
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
