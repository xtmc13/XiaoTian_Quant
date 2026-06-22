import React, { memo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Bot,
  Play,
  Pause,
  Square,
  Eye,
  Pencil,
  Trash2,
  RefreshCw,
  ArrowLeft,
  Copy,
  Terminal,
  LineChart,
  Wallet,
  Activity,
  TrendingUp,
  TrendingDown,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { KPICard } from '@/components/ui/KPICard'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import { PerformanceChart } from '@/components/charts/PerformanceChart'
import { aiBotApi, strategyApi } from '@/lib/api'
import type { BotItem } from '@/hooks/useBotData'
import { STATUS_META } from '@/hooks/useBotData'

export function StatusBadge({ status }: { status: string }) {
  const meta = STATUS_META[status] || STATUS_META.stopped
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11px] font-medium',
        meta.border,
        meta.text,
        meta.bg
      )}
    >
      <span className={cn('h-1.5 w-1.5 rounded-full', meta.dot)} />
      {meta.label}
    </span>
  )
}

function ActionBtn({
  icon,
  label,
  variant = 'default',
  onClick,
  loading,
}: {
  icon: React.ReactNode
  label: string
  variant?: 'default' | 'primary' | 'danger'
  onClick?: (e: React.MouseEvent) => void
  loading?: boolean
}) {
  const variants = {
    default:
      'border-[#1c1c1c] bg-[#141414] text-[#888888] hover:bg-[#1c1c1c] hover:text-white',
    primary: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-400 hover:bg-emerald-500/20',
    danger: 'border-red-500/20 bg-red-500/10 text-red-400 hover:bg-red-500/20',
  }
  return (
    <button
      onClick={onClick}
      disabled={loading}
      title={label}
      className={cn(
        'flex h-8 w-8 items-center justify-center rounded-lg border transition-colors disabled:opacity-50',
        variants[variant]
      )}
    >
      {loading ? <RefreshCw className="h-3.5 w-3.5 animate-spin" /> : icon}
    </button>
  )
}

export interface BotCardProps {
  bot: BotItem
  isSelected: boolean
  isActionLoading: boolean
  onSelect: (bot: BotItem) => void
  onStart: (bot: BotItem) => void
  onStop: (bot: BotItem) => void
  onEdit: (bot: BotItem) => void
  onDelete: (bot: BotItem) => void
  onViewDetail: (bot: BotItem) => void
}

export const BotCard = memo(function BotCard({
  bot,
  isSelected,
  isActionLoading,
  onSelect,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onViewDetail,
}: BotCardProps) {
  const isRunning = bot.status === 'running'
  const pnl = bot.unrealized_pnl ?? 0
  const pnlColor = pnl >= 0 ? 'text-emerald-400' : 'text-red-400'

  return (
    <div
      onClick={() => onSelect(bot)}
      className={cn(
        'group flex cursor-pointer flex-col gap-3 rounded-xl border bg-[#111111] p-4 transition-all sm:flex-row sm:items-center sm:justify-between',
        'border-[#1c1c1c] hover:border-[#2a2a2a] hover:shadow-[0_2px_8px_rgba(0,0,0,0.3)]',
        isSelected && 'border-[#4f6ed1]/30 bg-[#4f6ed1]/[0.06]'
      )}
    >
      {/* Left: icon + name + meta */}
      <div className="flex items-center gap-3">
        <div
          className={cn(
            'flex h-10 w-10 shrink-0 items-center justify-center rounded-lg',
            isRunning ? 'bg-emerald-500/10 text-emerald-400' : 'bg-[#1c1c1c] text-[#999999]'
          )}
        >
          <Bot className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-semibold text-white">
              {bot.name || bot.strategy_name || '未命名机器人'}
            </span>
            <StatusBadge status={bot.status} />
          </div>
          <div className="mt-0.5 flex items-center gap-2 text-[11px] text-[#8a8a8a]">
            <span>{bot.symbol || bot.coin || '-'}</span>
            <span className="text-[#333333]">·</span>
            <span className="capitalize">{bot.bot_type || 'custom'}</span>
            {bot.leverage ? (
              <>
                <span className="text-[#333333]">·</span>
                <span>{bot.leverage}x</span>
              </>
            ) : null}
          </div>
        </div>
      </div>

      {/* Middle: PnL + capital */}
      <div className="flex items-center gap-6 sm:justify-end">
        <div className="text-right">
          <div className={cn('font-mono text-sm font-semibold', pnlColor)}>
            {pnl >= 0 ? '+' : ''}
            {formatCurrency(pnl)}
          </div>
          <div className="text-[11px] text-[#8a8a8a]">未实现盈亏</div>
        </div>
        <div className="text-right">
          <div className="font-mono text-sm font-semibold text-white">
            ${formatCurrency(bot.initial_capital || bot.trading_config?.initial_capital || 0)}
          </div>
          <div className="text-[11px] text-[#8a8a8a]">初始资金</div>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1">
          {isRunning ? (
            <>
              <ActionBtn
                icon={<Pause className="h-3.5 w-3.5" />}
                label="暂停"
                onClick={(e) => {
                  e.stopPropagation()
                  onStop(bot)
                }}
                loading={isActionLoading}
              />
              <ActionBtn
                icon={<Square className="h-3.5 w-3.5" />}
                label="停止"
                variant="danger"
                onClick={(e) => {
                  e.stopPropagation()
                  onStop(bot)
                }}
                loading={isActionLoading}
              />
            </>
          ) : (
            <ActionBtn
              icon={<Play className="h-3.5 w-3.5" />}
              label="启动"
              variant="primary"
              onClick={(e) => {
                e.stopPropagation()
                onStart(bot)
              }}
              loading={isActionLoading}
            />
          )}
          <ActionBtn
            icon={<Eye className="h-3.5 w-3.5" />}
            label="详情"
            onClick={(e) => {
              e.stopPropagation()
              onViewDetail(bot)
            }}
          />
          <ActionBtn
            icon={<Pencil className="h-3.5 w-3.5" />}
            label="编辑"
            onClick={(e) => {
              e.stopPropagation()
              onEdit(bot)
            }}
          />
          <ActionBtn
            icon={<Trash2 className="h-3.5 w-3.5" />}
            label="删除"
            variant="danger"
            onClick={(e) => {
              e.stopPropagation()
              onDelete(bot)
            }}
          />
        </div>
      </div>
    </div>
  )
})

export function ConfigItem({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2">
      <div className="text-[11px] text-[#8a8a8a]">{label}</div>
      <div className="mt-0.5 text-sm font-medium text-white">{value}</div>
    </div>
  )
}

export function BotDetailView({
  bot,
  onBack,
  onStart,
  onStop,
  onEdit,
  onDelete,
  onClone,
}: {
  bot: BotItem
  onBack: () => void
  onStart: (bot: BotItem) => void
  onStop: (bot: BotItem) => void
  onEdit: (bot: BotItem) => void
  onDelete: (bot: BotItem) => void
  onClone: (bot: BotItem) => void
}) {
  const isRunning = bot.status === 'running'
  const totalPnl = (bot.unrealized_pnl || 0) + (bot.realized_pnl || 0)

  const { data: analytics, isLoading: analyticsLoading } = useQuery({
    queryKey: ['ai-bots', 'analytics', bot.id],
    queryFn: () => aiBotApi.analytics(bot.id),
    retry: false,
  })

  const { data: logs, isLoading: logsLoading } = useQuery({
    queryKey: ['strategies', 'logs', bot.id],
    queryFn: () => strategyApi.logs(bot.id),
    retry: false,
  })

  const equityData = React.useMemo(() => {
    if (!analytics?.snapshots) return []
    return analytics.snapshots
      .sort((a, b) => a.timestamp - b.timestamp)
      .map((s) => ({ time: s.timestamp * 1000, equity: s.total_equity || 0 }))
  }, [analytics])

  return (
    <div className="space-y-6">
      {/* Back */}
      <button
        onClick={onBack}
        className="flex items-center gap-1.5 text-sm text-[#999999] transition-colors hover:text-white"
      >
        <ArrowLeft className="h-4 w-4" />
        返回列表
      </button>

      {/* Header card */}
      <div className="rounded-xl border border-[#1c1c1c] bg-[#111111] p-5">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3">
            <div
              className={cn(
                'flex h-12 w-12 items-center justify-center rounded-xl',
                isRunning ? 'bg-emerald-500/10 text-emerald-400' : 'bg-[#1c1c1c] text-[#999999]'
              )}
            >
              <Bot className="h-6 w-6" />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">
                {bot.name || bot.strategy_name || '未命名机器人'}
              </h2>
              <div className="mt-1 flex items-center gap-2">
                <StatusBadge status={bot.status} />
                <span className="text-xs text-[#8a8a8a]">
                  {bot.symbol || bot.coin || '-'} · {bot.bot_type}
                </span>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {isRunning ? (
              <button
                onClick={() => onStop(bot)}
                className="flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/20"
              >
                <Square className="h-3.5 w-3.5" />
                停止
              </button>
            ) : (
              <button
                onClick={() => onStart(bot)}
                className="flex items-center gap-1.5 rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-400 transition-colors hover:bg-emerald-500/20"
              >
                <Play className="h-3.5 w-3.5" />
                启动
              </button>
            )}
            <button
              onClick={() => onEdit(bot)}
              className="flex items-center gap-1.5 rounded-lg border border-[#1c1c1c] bg-[#141414] px-3 py-2 text-xs font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
            >
              <Pencil className="h-3.5 w-3.5" />
              编辑
            </button>
            <button
              onClick={() => onClone(bot)}
              className="flex items-center gap-1.5 rounded-lg border border-[#1c1c1c] bg-[#141414] px-3 py-2 text-xs font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
            >
              <Copy className="h-3.5 w-3.5" />
              克隆
            </button>
            <button
              onClick={() => onDelete(bot)}
              className="flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs font-medium text-red-400 transition-colors hover:bg-red-500/20"
            >
              <Trash2 className="h-3.5 w-3.5" />
              删除
            </button>
          </div>
        </div>
      </div>

      {/* Stats grid */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <KPICard
          icon={<Wallet className="h-4 w-4" />}
          label="初始资金"
          value={`$${formatCurrency(bot.initial_capital || bot.trading_config?.initial_capital || 0)}`}
        />
        <KPICard
          icon={totalPnl >= 0 ? <TrendingUp className="h-4 w-4" /> : <TrendingDown className="h-4 w-4" />}
          label="总盈亏"
          value={`${totalPnl >= 0 ? '+' : ''}${formatCurrency(totalPnl)}`}
          trend={totalPnl >= 0 ? 'up' : 'down'}
        />
        <KPICard
          icon={<Activity className="h-4 w-4" />}
          label="未实现盈亏"
          value={`${(bot.unrealized_pnl || 0) >= 0 ? '+' : ''}${formatCurrency(bot.unrealized_pnl || 0)}`}
          trend={(bot.unrealized_pnl || 0) >= 0 ? 'up' : 'down'}
        />
        <KPICard
          icon={<LineChart className="h-4 w-4" />}
          label="已实现盈亏"
          value={`${(bot.realized_pnl || 0) >= 0 ? '+' : ''}${formatCurrency(bot.realized_pnl || 0)}`}
          trend={(bot.realized_pnl || 0) >= 0 ? 'up' : 'down'}
        />
      </div>

      {/* Performance chart */}
      <SectionCard title="收益曲线">
        {analyticsLoading ? (
          <Skeleton className="h-48 rounded-lg" />
        ) : equityData.length > 0 ? (
          <PerformanceChart data={equityData} height={260} />
        ) : (
          <div className="flex h-48 items-center justify-center rounded-lg border border-dashed border-[#1c1c1c] bg-[#0a0a0a]">
            <div className="text-center">
              <LineChart className="mx-auto h-8 w-8 text-[#333333]" />
              <p className="mt-2 text-xs text-[#8a8a8a]">暂无收益曲线数据</p>
            </div>
          </div>
        )}
      </SectionCard>

      {/* Logs */}
      <SectionCard title="运行日志">
        {logsLoading ? (
          <Skeleton className="h-40 rounded-lg" />
        ) : logs && logs.length > 0 ? (
          <div className="h-40 overflow-y-auto rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-3 space-y-1.5 font-mono text-[11px]">
            {logs.slice(0, 50).map((log: any, idx: number) => (
              <div key={idx} className="flex gap-2">
                <span className={cn(
                  'shrink-0',
                  log.level === 'error' ? 'text-red-400' : log.level === 'warning' ? 'text-yellow-400' : 'text-emerald-400'
                )}>[{log.level || 'info'}]</span>
                <span className="text-[#666]">{log.created_at ? new Date(log.created_at).toLocaleTimeString() : '-'}</span>
                <span className="text-[#aaa]">{log.message}</span>
              </div>
            ))}
          </div>
        ) : (
          <div className="flex h-40 items-center justify-center rounded-lg border border-dashed border-[#1c1c1c] bg-[#0a0a0a]">
            <div className="text-center">
              <Terminal className="mx-auto h-8 w-8 text-[#333333]" />
              <p className="mt-2 text-xs text-[#8a8a8a]">暂无运行日志</p>
            </div>
          </div>
        )}
      </SectionCard>

      {/* Config summary */}
      <SectionCard title="配置详情">
        <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
          <ConfigItem label="策略类型" value={bot.bot_type || '-'} />
          <ConfigItem label="标的" value={bot.symbol || bot.coin || '-'} />
          <ConfigItem label="杠杆" value={bot.leverage ? `${bot.leverage}x` : '-'} />
          <ConfigItem label="市场" value={bot.market_category || 'crypto'} />
          <ConfigItem label="执行模式" value={bot.execution_mode || 'paper'} />
          <ConfigItem
            label="创建时间"
            value={bot.created_at ? new Date(bot.created_at).toLocaleString() : '-'}
          />
        </div>
      </SectionCard>
    </div>
  )
}
