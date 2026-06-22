import React, { useMemo, useState } from 'react'
import {
  Play, Square, Copy, Trash2, BarChart3, Bot, TrendingUp,
  Activity, Shield, Pause, RotateCcw, MoreHorizontal, CheckSquare, Square as SquareIcon
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { KPICard, KPIGrid } from '@/components/ui/KPICard'
import { Skeleton } from '@/components/ui/Skeleton'
import type { AIBotInstance } from '@/types'

interface AIBotManagementProps {
  instances: AIBotInstance[]
  isLoading: boolean
  actionLoadingId: string | null
  kpi: {
    running: number
    stopped: number
    total: number
    totalPnl: number
    best: number
  }
  onStart: (bot: AIBotInstance) => void
  onStop: (bot: AIBotInstance) => void
  onPause: (bot: AIBotInstance) => void
  onResume: (bot: AIBotInstance) => void
  onClone: (bot: AIBotInstance) => void
  onDelete: (bot: AIBotInstance) => void
  onEdit: (bot: AIBotInstance) => void
  onViewAnalytics: (bot: AIBotInstance) => void
  onCreate: () => void
  onBatchStart?: (ids: string[]) => void
  onBatchStop?: (ids: string[]) => void
  onBatchDelete?: (ids: string[]) => void
  batchLoading?: boolean
}

const STATUS_META: Record<string, { label: string; variant: 'success' | 'warning' | 'error' | 'neutral'; dot: string }> = {
  running: { label: '运行中', variant: 'success', dot: 'bg-[#52c41a]' },
  stopped: { label: '已停止', variant: 'neutral', dot: 'bg-[#888]' },
  paused: { label: '暂停', variant: 'warning', dot: 'bg-[#faad14]' },
  error: { label: '错误', variant: 'error', dot: 'bg-[#f5222d]' },
}

const MARKET_LABEL: Record<string, string> = {
  spot: '现货',
  futures: '合约',
}

export const AIBotManagement: React.FC<AIBotManagementProps> = ({
  instances,
  isLoading,
  actionLoadingId,
  kpi,
  onStart,
  onStop,
  onPause,
  onResume,
  onClone,
  onDelete,
  onEdit,
  onViewAnalytics,
  onCreate,
  onBatchStart,
  onBatchStop,
  onBatchDelete,
  batchLoading,
}) => {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  const kpiItems = useMemo(
    () => [
      {
        label: '我的机器人',
        value: kpi.total,
        icon: <Bot className="w-4 h-4" />,
        variant: 'default' as const,
      },
      {
        label: '运行中',
        value: kpi.running,
        icon: <Activity className="w-4 h-4" />,
        variant: 'success' as const,
      },
      {
        label: '总未实现盈亏',
        value: formatCurrency(kpi.totalPnl),
        icon: <TrendingUp className="w-4 h-4" />,
        variant: (kpi.totalPnl >= 0 ? 'success' : 'error') as 'success' | 'error',
      },
      {
        label: '最佳收益',
        value: `${kpi.best.toFixed(2)}%`,
        icon: <Shield className="w-4 h-4" />,
        variant: 'info' as const,
      },
    ],
    [kpi]
  )

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleSelectAll = () => {
    if (selectedIds.size === instances.length && instances.length > 0) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(instances.map((b) => b.id)))
    }
  }

  const selectedArray = Array.from(selectedIds)
  const selectedStopped = instances.filter((b) => selectedIds.has(b.id) && b.status === 'stopped')
  const selectedRunning = instances.filter((b) => selectedIds.has(b.id) && (b.status === 'running' || b.status === 'paused'))

  return (
    <div className="space-y-4">
      <KPIGrid items={kpiItems} isLoading={isLoading} />

      <div className="flex flex-col sm:flex-row gap-3 sm:items-center justify-between">
        <h2 className="text-sm font-semibold text-white">机器人列表</h2>
        <div className="flex items-center gap-2">
          {selectedIds.size > 0 && (
            <>
              <span className="text-xs text-[#888]">已选 {selectedIds.size}</span>
              {selectedStopped.length > 0 && (
                <Button size="sm" variant="outline" isLoading={batchLoading} onClick={() => onBatchStart?.(selectedStopped.map((b) => b.id))}>
                  <Play className="w-3 h-3 mr-1" /> 批量启动
                </Button>
              )}
              {selectedRunning.length > 0 && (
                <Button size="sm" variant="outline" isLoading={batchLoading} onClick={() => onBatchStop?.(selectedRunning.map((b) => b.id))}>
                  <Square className="w-3 h-3 mr-1" /> 批量停止
                </Button>
              )}
              {selectedStopped.length > 0 && (
                <Button size="sm" variant="outline" className="text-[#f5222d] hover:bg-[#f5222d]/10" isLoading={batchLoading} onClick={() => onBatchDelete?.(selectedStopped.map((b) => b.id))}>
                  <Trash2 className="w-3 h-3 mr-1" /> 批量删除
                </Button>
              )}
            </>
          )}
          <Button size="sm" leftIcon={<Bot className="w-4 h-4" />} onClick={onCreate}>
            创建机器人
          </Button>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <button
          onClick={toggleSelectAll}
          className="flex items-center gap-1.5 text-xs text-[#888] hover:text-[#e0e0e0] transition-colors"
        >
          {selectedIds.size === instances.length && instances.length > 0 ? (
            <CheckSquare className="w-4 h-4 text-[#1890ff]" />
          ) : (
            <SquareIcon className="w-4 h-4" />
          )}
          全选
        </button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
        {isLoading &&
          Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-48 rounded-xl" />)}

        {!isLoading && instances.length === 0 && (
          <div className="col-span-full rounded-xl border border-[#1c1c1c] bg-[#111] p-8 text-center">
            <Bot className="w-10 h-10 text-[#444] mx-auto mb-3" />
            <div className="text-sm text-[#888]">暂无机器人</div>
            <div className="text-xs text-[#666] mt-1">从机器人市场部署一个，或创建自定义机器人</div>
            <Button className="mt-4" size="sm" onClick={onCreate}>创建第一个机器人</Button>
          </div>
        )}

        {!isLoading &&
          instances.map((bot) => {
            const status = STATUS_META[bot.status] || STATUS_META.stopped
            const isRunning = bot.status === 'running'
            const isPaused = bot.status === 'paused'
            const isStopped = bot.status === 'stopped'
            const isLoadingAction = actionLoadingId === bot.id
            const pnl = bot.unrealized_pnl || 0
            const pnlPct = bot.total_return_pct || 0
            const selected = selectedIds.has(bot.id)
            return (
              <div
                key={bot.id}
                className={cn(
                  'rounded-xl border bg-[#111] p-4 hover:border-[#333] transition-colors relative',
                  selected ? 'border-[#1890ff]/50' : 'border-[#1c1c1c]'
                )}
              >
                <div className="absolute top-3 left-3">
                  <button
                    onClick={() => toggleSelect(bot.id)}
                    className="text-[#888] hover:text-[#1890ff]"
                  >
                    {selected ? <CheckSquare className="w-4 h-4 text-[#1890ff]" /> : <SquareIcon className="w-4 h-4" />}
                  </button>
                </div>

                <div className="flex items-start justify-between mb-3 pl-6">
                  <div className="flex items-center gap-2.5">
                    <span className={cn('w-2 h-2 rounded-full', status.dot)} />
                    <div>
                      <div className="text-sm font-medium text-white">{bot.name}</div>
                      <div className="text-[10px] text-[#666]">{bot.symbol} · {MARKET_LABEL[bot.market_type] || bot.market_type}</div>
                    </div>
                  </div>
                  <Badge variant={status.variant} className="text-[10px]">{status.label}</Badge>
                </div>

                <div className="grid grid-cols-2 gap-2 mb-3">
                  <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5">
                    <div className="text-[10px] text-[#666] mb-0.5">未实现盈亏</div>
                    <div className={cn('text-sm font-semibold', pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                      {pnl >= 0 ? '+' : ''}
                      {formatCurrency(pnl)}
                    </div>
                  </div>
                  <div className="rounded-lg bg-[#0a0a0a] border border-[#1c1c1c] p-2.5">
                    <div className="text-[10px] text-[#666] mb-0.5">总收益率</div>
                    <div className={cn('text-sm font-semibold', pnlPct >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                      {pnlPct >= 0 ? '+' : ''}
                      {pnlPct.toFixed(2)}%
                    </div>
                  </div>
                </div>

                {bot.status === 'error' && bot.error_message && (
                  <div className="mb-3 text-[10px] text-[#f5222d] bg-[#f5222d]/10 border border-[#f5222d]/20 rounded-lg p-2">
                    {bot.error_message}
                  </div>
                )}

                <div className="flex flex-wrap gap-1.5 mb-3">
                  <Badge variant="neutral" className="text-[10px]">{bot.execution_mode === 'paper' ? '模拟' : bot.execution_mode === 'live' ? '实盘' : '信号'}</Badge>
                  <Badge variant="neutral" className="text-[10px]">{bot.strategy_type}</Badge>
                  {bot.sharpe_ratio !== undefined && bot.sharpe_ratio > 0 && (
                    <Badge variant="neutral" className="text-[10px]">Sharpe {bot.sharpe_ratio.toFixed(2)}</Badge>
                  )}
                </div>

                <div className="flex items-center gap-1.5 pt-3 border-t border-[#1c1c1c] flex-wrap">
                  {isRunning ? (
                    <>
                      <Button
                        size="sm"
                        variant="ghost"
                        isLoading={isLoadingAction}
                        onClick={() => onPause(bot)}
                        leftIcon={<Pause className="w-3 h-3 text-[#faad14]" />}
                        className="text-[#faad14] hover:bg-[#faad14]/10"
                      >
                        暂停
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        isLoading={isLoadingAction}
                        onClick={() => onStop(bot)}
                        leftIcon={<Square className="w-3 h-3 text-[#f5222d]" />}
                        className="text-[#f5222d] hover:bg-[#f5222d]/10"
                      >
                        停止
                      </Button>
                    </>
                  ) : isPaused ? (
                    <>
                      <Button
                        size="sm"
                        variant="ghost"
                        isLoading={isLoadingAction}
                        onClick={() => onResume(bot)}
                        leftIcon={<RotateCcw className="w-3 h-3 text-[#52c41a]" />}
                        className="text-[#52c41a] hover:bg-[#52c41a]/10"
                      >
                        恢复
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        isLoading={isLoadingAction}
                        onClick={() => onStop(bot)}
                        leftIcon={<Square className="w-3 h-3 text-[#f5222d]" />}
                        className="text-[#f5222d] hover:bg-[#f5222d]/10"
                      >
                        停止
                      </Button>
                    </>
                  ) : (
                    <Button
                      size="sm"
                      variant="ghost"
                      isLoading={isLoadingAction}
                      onClick={() => onStart(bot)}
                      leftIcon={<Play className="w-3 h-3 text-[#52c41a]" />}
                      className="text-[#52c41a] hover:bg-[#52c41a]/10"
                    >
                      启动
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    leftIcon={<BarChart3 className="w-3 h-3" />}
                    onClick={() => onViewAnalytics(bot)}
                  >
                    分析
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    leftIcon={<MoreHorizontal className="w-3 h-3" />}
                    onClick={() => onEdit(bot)}
                    disabled={isLoadingAction}
                  >
                    编辑
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    leftIcon={<Copy className="w-3 h-3" />}
                    onClick={() => onClone(bot)}
                    disabled={isLoadingAction}
                  >
                    克隆
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    leftIcon={<Trash2 className="w-3 h-3 text-[#f5222d]" />}
                    onClick={() => onDelete(bot)}
                    disabled={isLoadingAction}
                    className="text-[#f5222d] hover:bg-[#f5222d]/10"
                  >
                    删除
                  </Button>
                </div>
              </div>
            )
          })}
      </div>
    </div>
  )
}

export default AIBotManagement
