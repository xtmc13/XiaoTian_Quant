import React from 'react'
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
import { Button } from '@/components/ui/Button'
import { EmptyState } from '@/components/ui/EmptyState'
import { KPIGrid, type KPICardItem } from '@/components/ui/KPICard'
import { AsyncDataWrapper } from '@/components/ui/AsyncDataWrapper'
import { TradeRow, TradeRowList, type TradeRowItem } from '@/components/ui/TradeRow'
import { executorApi } from '@/lib/api'
import type { ExecutorStatus, ExecutionRecord, ExecutorPosition } from '@/types'
import { toast } from '@/lib/useToast'

/* ── KPI Data Builder ── */
const buildKPIItems = (status: ExecutorStatus): KPICardItem[] => [
  {
    label: '活跃持仓',
    value: status.active_positions,
    icon: <Activity className="w-4 h-4 text-[#1890ff]" />,
    variant: 'info',
  },
  {
    label: '待执行信号',
    value: status.pending_signals,
    icon: <Signal className="w-4 h-4 text-[#faad14]" />,
    variant: 'warning',
  },
  {
    label: '今日执行',
    value: status.today_executed,
    icon: <Target className="w-4 h-4 text-[#52c41a]" />,
    variant: 'success',
  },
  {
    label: '今日盈亏',
    value: `${status.today_pnl >= 0 ? '+' : ''}${formatCurrency(status.today_pnl)}`,
    icon:
      status.today_pnl >= 0 ? (
        <TrendingUp className="w-4 h-4 text-[#52c41a]" />
      ) : (
        <TrendingDown className="w-4 h-4 text-[#f5222d]" />
      ),
    variant: status.today_pnl >= 0 ? 'success' : 'error',
  },
]

/* ── TP Stats Builder ── */
const TP_STATS = [
  { label: 'TP1', key: 'tp1_executed' as const, color: 'text-[#52c41a]', bg: 'bg-[#52c41a]/10', border: 'border-[#52c41a]/20' },
  { label: 'TP2', key: 'tp2_executed' as const, color: 'text-[#1890ff]', bg: 'bg-[#1890ff]/10', border: 'border-[#1890ff]/20' },
  { label: 'TP3', key: 'tp3_executed' as const, color: 'text-[#722ed1]', bg: 'bg-[#722ed1]/10', border: 'border-[#722ed1]/20' },
  { label: 'SL', key: 'sl_triggered' as const, color: 'text-[#f5222d]', bg: 'bg-[#f5222d]/10', border: 'border-[#f5222d]/20' },
]

/* ── Execution Records to TradeRow items ── */
const typeLabels: Record<string, string> = {
  entry: '开仓',
  tp1: 'TP1',
  tp2: 'TP2',
  tp3: 'TP3',
  sl: '止损',
}

const typeVariants: Record<string, 'default' | 'success' | 'warning' | 'error' | 'info'> = {
  entry: 'info',
  tp1: 'success',
  tp2: 'success',
  tp3: 'success',
  sl: 'error',
}

function recordsToRows(records: ExecutionRecord[]): TradeRowItem[] {
  return records.map((rec) => ({
    id: rec.id,
    badge: {
      label: typeLabels[rec.type] || rec.type,
      variant: typeVariants[rec.type] || 'default',
    },
    symbol: rec.symbol,
    side: rec.side,
    price: rec.price,
    pnl: rec.pnl,
    time: new Date(rec.executed_at).toLocaleTimeString('zh-CN', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }),
  }))
}

/* ── Signal Sources ── */
const SignalSourcesView: React.FC<{
  sources: { id: string; name: string; type: string; enabled: boolean; webhook_url?: string; signal_count_today: number; signal_count_total: number; tp_sl_config?: { tp1_pct: number; tp2_pct: number; tp3_pct: number; sl_pct: number } }[]
}> = ({ sources }) => {
  const [copiedId, setCopiedId] = React.useState<string | null>(null)

  const handleCopy = async (url: string, id: string) => {
    try {
      await navigator.clipboard.writeText(url)
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    } catch {
      const el = document.createElement('textarea')
      el.value = url
      document.body.appendChild(el)
      el.select()
      document.execCommand('copy')
      document.body.removeChild(el)
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 2000)
    }
  }

  const typeIcon = (type: string) => {
    if (type === 'webhook') return <Webhook className="w-4 h-4 text-[#1890ff]" />
    if (type === 'api') return <Bot className="w-4 h-4 text-[#52c41a]" />
    return <Radio className="w-4 h-4 text-[#faad14]" />
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
              {typeIcon(source.type)}
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
const ActivePositionsView: React.FC<{
  positions: ExecutorPosition[]
}> = ({ positions }) => {
  const [expandedId, setExpandedId] = React.useState<string | null>(null)

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
                <span
                  className={cn(
                    'text-xs',
                    (pos.unrealized_pnl || 0) >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'
                  )}
                >
                  {(pos.unrealized_pnl || 0) >= 0 ? '+' : ''}
                  {formatCurrency(pos.unrealized_pnl || 0)}
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
                  <div className="text-[#888]">
                    入场价: <span className="text-[#ccc]">{pos.entry_price}</span>
                  </div>
                  <div className="text-[#888]">
                    当前价: <span className="text-[#ccc]">{pos.current_price}</span>
                  </div>
                  <div className="text-[#888]">
                    数量: <span className="text-[#ccc]">{pos.quantity}</span>
                  </div>
                  <div className="text-[#888]">
                    已实现盈亏:{' '}
                    <span className="text-[#ccc]">{formatCurrency(pos.realized_pnl || 0)}</span>
                  </div>
                  {pos.tp1_price && (
                    <div className="text-[#52c41a]">
                      TP1: {pos.tp1_price} {pos.tp1_hit && '✓'}
                    </div>
                  )}
                  {pos.tp2_price && (
                    <div className="text-[#1890ff]">
                      TP2: {pos.tp2_price} {pos.tp2_hit && '✓'}
                    </div>
                  )}
                  {pos.tp3_price && (
                    <div className="text-[#722ed1]">
                      TP3: {pos.tp3_price} {pos.tp3_hit && '✓'}
                    </div>
                  )}
                  {pos.sl_price && <div className="text-[#f5222d]">SL: {pos.sl_price}</div>}
                </div>
              </div>
            )}
          </div>
        )
      })}
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
          <div
            className={cn(
              'w-2.5 h-2.5 rounded-full',
              status?.status === 'running'
                ? 'bg-[#52c41a] animate-pulse'
                : status?.status === 'error'
                  ? 'bg-[#f5222d]'
                  : 'bg-[#555]'
            )}
          />
          <span className="text-sm font-medium text-[#e0e0e0]">
            SignalExecutor{' '}
            {status?.status === 'running'
              ? '运行中'
              : status?.status === 'error'
                ? '异常'
                : '已停止'}
          </span>
        </div>
        {status?.updated_at && (
          <span className="text-xs text-[#555]">
            更新:{' '}
            {new Date(status.updated_at).toLocaleTimeString('zh-CN', {
              hour: '2-digit',
              minute: '2-digit',
            })}
          </span>
        )}
      </div>

      {/* KPI — 使用通用组件 */}
      <KPIGrid
        items={status ? buildKPIItems(status) : Array.from({ length: 4 }, (_, i) => ({ label: '-', value: '-', icon: null, variant: 'default' as const }))}
        isLoading={statusLoading}
      />

      {/* TP/SL Stats */}
      <SectionCard title="止盈/止损统计">
        <AsyncDataWrapper
          isLoading={statusLoading}
          data={status}
          skeleton={<Skeleton className="h-24 rounded-xl" />}
          empty={<EmptyState icon={<ShieldAlert className="w-8 h-8" />} title="暂无统计数据" />}
        >
          {(s) => (
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              {TP_STATS.map((stat) => (
                <div
                  key={stat.label}
                  className={cn('rounded-xl border p-3 text-center', stat.bg, stat.border)}
                >
                  <div className={cn('text-2xl font-bold', stat.color)}>
                    {(s as ExecutorStatus)[stat.key]}
                  </div>
                  <div className="text-[10px] text-[#888] mt-1">{stat.label} 触发</div>
                </div>
              ))}
            </div>
          )}
        </AsyncDataWrapper>
      </SectionCard>

      {/* Signal Sources — 使用通用组件 */}
      <SectionCard title="信号来源">
        <AsyncDataWrapper
          isLoading={sourcesLoading}
          data={sourcesData}
          skeleton={<Skeleton className="h-40 rounded-xl" />}
          empty={
            <EmptyState
              icon={<Radio className="w-8 h-8" />}
              title="暂无信号来源配置"
              action={
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => toast({ title: '请前往设置页配置信号源' })}
                >
                  配置信号源
                </Button>
              }
            />
          }
        >
          {(sources) => <SignalSourcesView sources={sources} />}
        </AsyncDataWrapper>
      </SectionCard>

      {/* Active Positions — 使用通用组件 */}
      <SectionCard title="活跃持仓">
        <AsyncDataWrapper
          isLoading={positionsLoading}
          data={positionsData}
          skeleton={<Skeleton className="h-32 rounded-xl" />}
          empty={<EmptyState icon={<ShieldAlert className="w-8 h-8" />} title="当前无活跃持仓" />}
        >
          {(positions) => <ActivePositionsView positions={positions} />}
        </AsyncDataWrapper>
      </SectionCard>

      {/* Execution Records — 使用通用 TradeRow 组件 */}
      <SectionCard title="执行记录">
        <AsyncDataWrapper
          isLoading={recordsLoading}
          data={recordsData}
          skeleton={<Skeleton className="h-32 rounded-xl" />}
          empty={<EmptyState title="暂无执行记录" />}
        >
          {(records) => (
            <TradeRowList
              items={recordsToRows(records)}
              maxHeight="320px"
            />
          )}
        </AsyncDataWrapper>
      </SectionCard>
    </div>
  )
}

export default SignalExecutorPanel
