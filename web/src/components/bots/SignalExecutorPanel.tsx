import React, { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Signal,
  TrendingUp,
  TrendingDown,
  Target,
  ShieldAlert,
  Copy,
  Check,
  ChevronDown,
  ChevronUp,
  Radio,
  Webhook,
  Bot,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { executorApi } from '@/lib/api'
import type { ExecutorStatus, ExecutionRecord, ExecutorPosition, SignalSource } from '@/types'

/* ── KPI Row ── */
interface ExecutorKPIProps {
  status: ExecutorStatus | undefined
  isLoading: boolean
}

const ExecutorKPI: React.FC<ExecutorKPIProps> = ({ status, isLoading }) => {
  if (isLoading || !status) {
    return (
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-20 rounded-xl" />
        ))}
      </div>
    )
  }

  const items = [
    {
      label: '活跃持仓',
      value: status.active_positions,
      icon: <Activity className="w-4 h-4 text-[#1890ff]" />,
      variant: 'info' as const,
    },
    {
      label: '待执行信号',
      value: status.pending_signals,
      icon: <Signal className="w-4 h-4 text-[#faad14]" />,
      variant: 'warning' as const,
    },
    {
      label: '今日执行',
      value: status.today_executed,
      icon: <Target className="w-4 h-4 text-[#52c41a]" />,
      variant: 'success' as const,
    },
    {
      label: '今日盈亏',
      value: `${status.today_pnl >= 0 ? '+' : ''}${formatCurrency(status.today_pnl)}`,
      icon: status.today_pnl >= 0
        ? <TrendingUp className="w-4 h-4 text-[#52c41a]" />
        : <TrendingDown className="w-4 h-4 text-[#f5222d]" />,
      variant: status.today_pnl >= 0 ? 'success' : 'error' as const,
    },
  ]

  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      {items.map((item) => (
        <div
          key={item.label}
          className="rounded-xl border border-[#1c1c1c] bg-[#111] p-4"
        >
          <div className="flex items-center gap-2 mb-2">
            {item.icon}
            <span className="text-xs text-[#888]">{item.label}</span>
          </div>
          <div className="text-lg font-semibold text-[#e0e0e0]">{item.value}</div>
        </div>
      ))}
    </div>
  )
}

/* ── TP/SL Stats ── */
interface TPStatsProps {
  status: ExecutorStatus | undefined
  isLoading: boolean
}

const TPStats: React.FC<TPStatsProps> = ({ status, isLoading }) => {
  if (isLoading || !status) {
    return <Skeleton className="h-24 rounded-xl" />
  }

  const stats = [
    { label: 'TP1 触发', value: status.tp1_executed, color: 'text-[#52c41a]', bg: 'bg-[#52c41a]/10', border: 'border-[#52c41a]/20' },
    { label: 'TP2 触发', value: status.tp2_executed, color: 'text-[#1890ff]', bg: 'bg-[#1890ff]/10', border: 'border-[#1890ff]/20' },
    { label: 'TP3 触发', value: status.tp3_executed, color: 'text-[#722ed1]', bg: 'bg-[#722ed1]/10', border: 'border-[#722ed1]/20' },
    { label: 'SL 触发', value: status.sl_triggered, color: 'text-[#f5222d]', bg: 'bg-[#f5222d]/10', border: 'border-[#f5222d]/20' },
  ]

  return (
    <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
      {stats.map((s) => (
        <div
          key={s.label}
          className={cn('rounded-xl border p-3 text-center', s.bg, s.border)}
        >
          <div className={cn('text-2xl font-bold', s.color)}>{s.value}</div>
          <div className="text-[10px] text-[#888] mt-1">{s.label}</div>
        </div>
      ))}
    </div>
  )
}

/* ── Signal Sources ── */
interface SignalSourcesProps {
  sources: SignalSource[] | undefined
  isLoading: boolean
}

const SignalSourcesView: React.FC<SignalSourcesProps> = ({ sources, isLoading }) => {
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const handleCopy = async (url: string, id: string) => {
    try {
      await navigator.clipboard.writeText(url)
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    } catch {
      // fallback
      const textArea = document.createElement('textarea')
      textArea.value = url
      document.body.appendChild(textArea)
      textArea.select()
      document.execCommand('copy')
      document.body.removeChild(textArea)
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    }
  }

  if (isLoading) {
    return <Skeleton className="h-40 rounded-xl" />
  }

  if (!sources || sources.length === 0) {
    return (
      <div className="text-center py-8 text-[#555]">
        <Radio className="w-8 h-8 mx-auto mb-2 opacity-50" />
        <p className="text-sm mb-4">暂无信号来源配置</p>
        <Button variant="outline" size="sm" onClick={() => toast({ title: '请前往设置页配置信号源' })}>
          配置信号源
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {sources.map((source) => (
        <div
          key={source.id}
          className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4"
        >
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              {source.type === 'webhook' && <Webhook className="w-4 h-4 text-[#1890ff]" />}
              {source.type === 'api' && <Bot className="w-4 h-4 text-[#52c41a]" />}
              {source.type === 'internal' && <Radio className="w-4 h-4 text-[#faad14]" />}
              <span className="text-sm font-medium text-[#e0e0e0]">{source.name}</span>
              <Badge variant={source.enabled ? 'success' : 'neutral'} dot>
                {source.enabled ? '启用' : '停用'}
              </Badge>
            </div>
            <span className="text-xs text-[#555]">
              今日: {source.signal_count_today} / 总计: {source.signal_count_total}
            </span>
          </div>

          {source.webhook_url && (
            <div className="flex items-center gap-2 mt-2">
              <code className="flex-1 text-xs bg-[#111] border border-[#1c1c1c] rounded-lg px-3 py-2 text-[#888] truncate">
                {source.webhook_url}
              </code>
              <button
                type="button"
                onClick={() => handleCopy(source.webhook_url!, source.id)}
                className="flex items-center gap-1 px-3 py-2 rounded-lg bg-[#2a2a2a] hover:bg-[#333] text-xs text-[#aaa] transition-colors"
              >
                {copiedId === source.id ? (
                  <>
                    <Check className="w-3 h-3 text-[#52c41a]" />
                    已复制
                  </>
                ) : (
                  <>
                    <Copy className="w-3 h-3" />
                    复制
                  </>
                )}
              </button>
            </div>
          )}

          {source.tp_sl_config && (
            <div className="flex flex-wrap gap-2 mt-3">
              <Badge variant="success">TP1: {source.tp_sl_config.tp1_pct}%</Badge>
              <Badge variant="info">TP2: {source.tp_sl_config.tp2_pct}%</Badge>
              <Badge variant="default">TP3: {source.tp_sl_config.tp3_pct}%</Badge>
              <Badge variant="error">SL: {source.tp_sl_config.sl_pct}%</Badge>
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

/* ── Active Positions ── */
interface ActivePositionsProps {
  positions: ExecutorPosition[] | undefined
  isLoading: boolean
}

const ActivePositionsView: React.FC<ActivePositionsProps> = ({ positions, isLoading }) => {
  const [expandedId, setExpandedId] = useState<string | null>(null)

  if (isLoading) {
    return <Skeleton className="h-32 rounded-xl" />
  }

  if (!positions || positions.length === 0) {
    return (
      <div className="text-center py-8 text-[#555]">
        <ShieldAlert className="w-8 h-8 mx-auto mb-2 opacity-50" />
        <p className="text-sm">当前无活跃持仓</p>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {positions.map((pos) => {
        const isExpanded = expandedId === pos.id
        return (
          <div
            key={pos.id}
            className={cn(
              'rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] transition-all',
              isExpanded && 'border-[#333]'
            )}
          >
            <button
              type="button"
              onClick={() => setExpandedId(isExpanded ? null : pos.id)}
              className="w-full flex items-center justify-between p-3 text-left"
            >
              <div className="flex items-center gap-3">
                <Badge variant={pos.side === 'LONG' ? 'success' : 'error'}>
                  {pos.side}
                </Badge>
                <span className="text-sm font-medium text-[#e0e0e0]">{pos.symbol}</span>
                <span className={cn(
                  'text-xs',
                  (pos.unrealized_pnl || 0) >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'
                )}>
                  {(pos.unrealized_pnl || 0) >= 0 ? '+' : ''}{formatCurrency(pos.unrealized_pnl || 0)}
                </span>
              </div>
              {isExpanded ? (
                <ChevronUp className="w-4 h-4 text-[#555]" />
              ) : (
                <ChevronDown className="w-4 h-4 text-[#555]" />
              )}
            </button>

            {isExpanded && (
              <div className="px-3 pb-3 border-t border-[#1c1c1c] pt-3">
                <div className="grid grid-cols-2 gap-2 text-xs">
                  <div className="text-[#888]">入场价: <span className="text-[#ccc]">{pos.entry_price}</span></div>
                  <div className="text-[#888]">当前价: <span className="text-[#ccc]">{pos.current_price}</span></div>
                  <div className="text-[#888]">数量: <span className="text-[#ccc]">{pos.quantity}</span></div>
                  <div className="text-[#888]">已实现盈亏: <span className="text-[#ccc]">{formatCurrency(pos.realized_pnl || 0)}</span></div>
                  {pos.tp1_price && (
                    <div className="text-[#52c41a]">TP1: {pos.tp1_price} {pos.tp1_hit && '✓'}</div>
                  )}
                  {pos.tp2_price && (
                    <div className="text-[#1890ff]">TP2: {pos.tp2_price} {pos.tp2_hit && '✓'}</div>
                  )}
                  {pos.tp3_price && (
                    <div className="text-[#722ed1]">TP3: {pos.tp3_price} {pos.tp3_hit && '✓'}</div>
                  )}
                  {pos.sl_price && (
                    <div className="text-[#f5222d]">SL: {pos.sl_price}</div>
                  )}
                </div>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

/* ── Execution Records ── */
interface ExecutionRecordsProps {
  records: ExecutionRecord[] | undefined
  isLoading: boolean
}

const ExecutionRecordsView: React.FC<ExecutionRecordsProps> = ({ records, isLoading }) => {
  if (isLoading) {
    return <Skeleton className="h-32 rounded-xl" />
  }

  if (!records || records.length === 0) {
    return (
      <div className="text-center py-6 text-[#555]">
        <p className="text-sm">暂无执行记录</p>
      </div>
    )
  }

  const typeLabels: Record<string, string> = {
    entry: '开仓',
    tp1: 'TP1',
    tp2: 'TP2',
    tp3: 'TP3',
    sl: '止损',
  }

  const typeVariants: Record<string, BadgeVariant> = {
    entry: 'info',
    tp1: 'success',
    tp2: 'success',
    tp3: 'success',
    sl: 'error',
  }

  type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info' | 'neutral'

  return (
    <div className="space-y-1.5 max-h-80 overflow-y-auto">
      {records.map((rec) => (
        <div
          key={rec.id}
          className="flex items-center justify-between rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2"
        >
          <div className="flex items-center gap-2">
            <Badge variant={typeVariants[rec.type] || 'default'}>
              {typeLabels[rec.type] || rec.type}
            </Badge>
            <span className="text-xs text-[#ccc]">{rec.symbol}</span>
            <span className="text-xs text-[#888]">{rec.side}</span>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-xs text-[#888]">@{rec.price}</span>
            {rec.pnl !== undefined && (
              <span className={cn(
                'text-xs font-medium',
                rec.pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'
              )}>
                {rec.pnl >= 0 ? '+' : ''}{formatCurrency(rec.pnl)}
              </span>
            )}
            <span className="text-[10px] text-[#555]">
              {new Date(rec.executed_at).toLocaleTimeString()}
            </span>
          </div>
        </div>
      ))}
    </div>
  )
}

/* ── Main Panel ── */
export const SignalExecutorPanel: React.FC = () => {
  const { data: status, isLoading: statusLoading } = useQuery({
    queryKey: ['executor', 'status'],
    queryFn: () => executorApi.getStatus().then((r) => r.data),
    refetchInterval: 5000,
  })

  const { data: positionsData, isLoading: positionsLoading } = useQuery({
    queryKey: ['executor', 'positions'],
    queryFn: () => executorApi.getActivePositions().then((r) => r.data?.positions || []),
    refetchInterval: 5000,
  })

  const { data: recordsData, isLoading: recordsLoading } = useQuery({
    queryKey: ['executor', 'records'],
    queryFn: () => executorApi.getExecutionRecords({ limit: 50 }).then((r) => r.data?.records || []),
    refetchInterval: 10000,
  })

  const { data: sourcesData, isLoading: sourcesLoading } = useQuery({
    queryKey: ['executor', 'sources'],
    queryFn: () => executorApi.getSignalSources().then((r) => r.data?.sources || []),
    refetchInterval: 30000,
  })

  return (
    <div className="space-y-5">
      {/* Status Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className={cn(
            'w-2.5 h-2.5 rounded-full',
            status?.status === 'running' ? 'bg-[#52c41a] animate-pulse' :
            status?.status === 'error' ? 'bg-[#f5222d]' : 'bg-[#555]'
          )} />
          <span className="text-sm font-medium text-[#e0e0e0]">
            SignalExecutor {status?.status === 'running' ? '运行中' : status?.status === 'error' ? '异常' : '已停止'}
          </span>
        </div>
        {status?.updated_at && (
          <span className="text-xs text-[#555]">
            更新: {new Date(status.updated_at).toLocaleTimeString()}
          </span>
        )}
      </div>

      {/* KPI */}
      <ExecutorKPI status={status} isLoading={statusLoading} />

      {/* TP/SL Stats */}
      <SectionCard title="止盈/止损统计">
        <TPStats status={status} isLoading={statusLoading} />
      </SectionCard>

      {/* Signal Sources */}
      <SectionCard title="信号来源">
        <SignalSourcesView sources={sourcesData} isLoading={sourcesLoading} />
      </SectionCard>

      {/* Active Positions */}
      <SectionCard title="活跃持仓">
        <ActivePositionsView positions={positionsData} isLoading={positionsLoading} />
      </SectionCard>

      {/* Execution Records */}
      <SectionCard title="执行记录">
        <ExecutionRecordsView records={recordsData} isLoading={recordsLoading} />
      </SectionCard>
    </div>
  )
}

export default SignalExecutorPanel
