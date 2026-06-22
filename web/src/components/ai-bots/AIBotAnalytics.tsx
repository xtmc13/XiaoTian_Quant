import React, { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  TrendingUp, TrendingDown, Activity, Shield, BarChart3,
  DollarSign, Percent, Wallet
} from 'lucide-react'
import { aiBotApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import { KPICard, KPIGrid } from '@/components/ui/KPICard'
import { Skeleton } from '@/components/ui/Skeleton'
import { SectionCard } from '@/components/ui/SectionCard'
import { PerformanceChart } from '@/components/charts/PerformanceChart'
import type { AIBotInstance, AIBotSnapshot, AIBotTrade, AIBotAnalytics as AIBotAnalyticsType } from '@/types'

interface AIBotAnalyticsProps {
  instances: AIBotInstance[]
  selectedBotId?: string
  onSelectBot?: (id: string) => void
}

const EMPTY_ANALYTICS: AIBotAnalyticsType = {
  bot: {
    id: '',
    user_id: 0,
    name: '',
    strategy_type: '',
    symbol: '',
    market_type: 'spot',
    status: 'stopped',
    execution_mode: 'paper',
  },
  snapshots: [],
}

export const AIBotAnalytics: React.FC<AIBotAnalyticsProps> = ({
  instances,
  selectedBotId,
  onSelectBot,
}) => {
  const selectedId = selectedBotId || instances[0]?.id
  const selectedBot = instances.find((b) => b.id === selectedId)

  const { data = EMPTY_ANALYTICS, isLoading: analyticsLoading } = useQuery<AIBotAnalyticsType>({
    queryKey: ['ai-bots', 'analytics', selectedId],
    queryFn: () => (selectedId ? aiBotApi.analytics(selectedId) : Promise.resolve(EMPTY_ANALYTICS)),
    enabled: !!selectedId,
    refetchInterval: 10_000,
  })

  const { data: tradesData, isLoading: tradesLoading } = useQuery<{ bot: AIBotInstance; trades: AIBotTrade[] }>({
    queryKey: ['ai-bots', 'trades', selectedId],
    queryFn: () => (selectedId ? aiBotApi.trades(selectedId, 50) : Promise.resolve({ bot: EMPTY_ANALYTICS.bot, trades: [] })),
    enabled: !!selectedId,
    refetchInterval: 10_000,
  })

  const snapshots = useMemo(() => (data.snapshots || []) as AIBotSnapshot[], [data.snapshots])
  const sortedSnapshots = useMemo(() => {
    return [...snapshots].sort((a, b) => a.timestamp - b.timestamp)
  }, [snapshots])

  const equityData = useMemo(() => {
    return sortedSnapshots.map((s) => ({ time: s.timestamp * 1000, equity: s.total_equity || 0 }))
  }, [sortedSnapshots])

  const kpi = useMemo(() => {
    const bot = data.bot || selectedBot
    return {
      totalReturn: bot?.total_return_pct || 0,
      maxDrawdown: bot?.max_drawdown_pct || 0,
      sharpe: bot?.sharpe_ratio || 0,
      winRate: (bot?.win_rate || 0) * 100,
      totalTrades: bot?.total_trades || 0,
      unrealized: bot?.unrealized_pnl || 0,
      realized: bot?.realized_pnl || 0,
      initialBalance: bot?.initial_balance || 10000,
      equity: (bot?.initial_balance || 10000) + (bot?.unrealized_pnl || 0) + (bot?.realized_pnl || 0),
    }
  }, [data.bot, selectedBot])

  const kpiItems = useMemo(
    () => [
      {
        label: '总收益率',
        value: `${kpi.totalReturn.toFixed(2)}%`,
        icon: kpi.totalReturn >= 0 ? <TrendingUp className="w-4 h-4" /> : <TrendingDown className="w-4 h-4" />,
        variant: (kpi.totalReturn >= 0 ? 'success' : 'error') as 'success' | 'error',
      },
      {
        label: '最大回撤',
        value: `${kpi.maxDrawdown.toFixed(2)}%`,
        icon: <Shield className="w-4 h-4" />,
        variant: 'error' as const,
      },
      {
        label: '夏普比率',
        value: kpi.sharpe.toFixed(2),
        icon: <Activity className="w-4 h-4" />,
        variant: 'info' as const,
      },
      {
        label: '胜率',
        value: `${kpi.winRate.toFixed(1)}%`,
        icon: <Percent className="w-4 h-4" />,
        variant: 'default' as const,
      },
      {
        label: '总交易数',
        value: kpi.totalTrades,
        icon: <BarChart3 className="w-4 h-4" />,
        variant: 'default' as const,
      },
      {
        label: '当前权益',
        value: formatCurrency(kpi.equity),
        icon: <Wallet className="w-4 h-4" />,
        variant: 'default' as const,
      },
      {
        label: '未实现盈亏',
        value: formatCurrency(kpi.unrealized),
        icon: <TrendingUp className="w-4 h-4" />,
        variant: (kpi.unrealized >= 0 ? 'success' : 'error') as 'success' | 'error',
      },
      {
        label: '已实现盈亏',
        value: formatCurrency(kpi.realized),
        icon: <DollarSign className="w-4 h-4" />,
        variant: (kpi.realized >= 0 ? 'success' : 'error') as 'success' | 'error',
      },
    ],
    [kpi]
  )

  const trades = useMemo(() => tradesData?.trades || [], [tradesData])

  return (
    <div className="space-y-4">
      {instances.length > 0 && (
        <div className="flex items-center gap-2 overflow-x-auto pb-1">
          {instances.map((bot) => (
            <button
              key={bot.id}
              onClick={() => onSelectBot?.(bot.id)}
              className={cn(
                'px-3 py-1.5 rounded-lg text-xs font-medium border whitespace-nowrap transition-colors',
                selectedId === bot.id
                  ? 'bg-[#1890ff]/10 border-[#1890ff]/30 text-[#1890ff]'
                  : 'bg-[#111] border-[#2a2a2a] text-[#888] hover:border-[#444]'
              )}
            >
              {bot.name}
            </button>
          ))}
        </div>
      )}

      {!selectedId && (
        <div className="rounded-xl border border-[#1c1c1c] bg-[#111] p-8 text-center text-[#666]">
          暂无机器人，无法查看数据分析
        </div>
      )}

      {selectedId && (
        <>
          <KPIGrid items={kpiItems} isLoading={analyticsLoading} />

          <SectionCard title="收益曲线">
            {analyticsLoading ? (
              <Skeleton className="h-64 rounded-lg" />
            ) : equityData.length === 0 ? (
              <div className="h-64 flex items-center justify-center text-[#666]">暂无快照数据</div>
            ) : (
              <PerformanceChart data={equityData} height={320} />
            )}
          </SectionCard>

          <SectionCard title="交易记录">
            {tradesLoading ? (
              <Skeleton className="h-40 rounded-lg" />
            ) : trades.length === 0 ? (
              <div className="text-center py-8 text-[#666]">暂无交易记录</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-[#666] border-b border-[#1c1c1c]">
                      <th className="text-left py-2 px-2">时间</th>
                      <th className="text-left py-2 px-2">方向</th>
                      <th className="text-right py-2 px-2">数量</th>
                      <th className="text-right py-2 px-2">开仓价</th>
                      <th className="text-right py-2 px-2">平仓价</th>
                      <th className="text-right py-2 px-2">盈亏</th>
                      <th className="text-right py-2 px-2">原因</th>
                    </tr>
                  </thead>
                  <tbody>
                    {trades.map((t) => (
                      <tr key={t.id} className="border-b border-[#1c1c1c]/50 hover:bg-[#0a0a0a]">
                        <td className="py-2 px-2 text-[#888]">
                          {t.closed_at > 0
                            ? new Date(t.closed_at * 1000).toLocaleString()
                            : new Date(t.opened_at * 1000).toLocaleString()}
                        </td>
                        <td className="py-2 px-2">
                          <span className={cn(
                            'font-medium',
                            t.side === 'LONG' ? 'text-[#52c41a]' : 'text-[#f5222d]'
                          )}>
                            {t.side === 'LONG' ? '做多' : '做空'}
                          </span>
                        </td>
                        <td className="py-2 px-2 text-right text-[#888]">{t.quantity.toFixed(6)}</td>
                        <td className="py-2 px-2 text-right text-[#888]">{formatCurrency(t.entry_price)}</td>
                        <td className="py-2 px-2 text-right text-[#888]">{formatCurrency(t.exit_price)}</td>
                        <td className={cn('py-2 px-2 text-right font-medium', t.pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                          {t.pnl >= 0 ? '+' : ''}{formatCurrency(t.pnl)} ({t.pnl_pct.toFixed(2)}%)
                        </td>
                        <td className="py-2 px-2 text-right text-[#888]">
                          {t.close_reason === 'tp' ? '止盈' : t.close_reason === 'sl' ? '止损' : t.close_reason || '-'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </SectionCard>

          <SectionCard title="快照记录">
            {analyticsLoading ? (
              <Skeleton className="h-32 rounded-lg" />
            ) : sortedSnapshots.length === 0 ? (
              <div className="text-center py-8 text-[#666]">暂无快照记录</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-[#666] border-b border-[#1c1c1c]">
                      <th className="text-left py-2 px-2">时间</th>
                      <th className="text-right py-2 px-2">总权益</th>
                      <th className="text-right py-2 px-2">未实现盈亏</th>
                      <th className="text-right py-2 px-2">已实现盈亏</th>
                      <th className="text-right py-2 px-2">总收益率</th>
                    </tr>
                  </thead>
                  <tbody>
                    {[...sortedSnapshots].reverse().slice(0, 20).map((s, idx) => (
                      <tr key={idx} className="border-b border-[#1c1c1c]/50 hover:bg-[#0a0a0a]">
                        <td className="py-2 px-2 text-[#888]">{new Date(s.timestamp * 1000).toLocaleString()}</td>
                        <td className="py-2 px-2 text-right">{formatCurrency(s.total_equity)}</td>
                        <td className={cn('py-2 px-2 text-right', s.unrealized_pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                          {formatCurrency(s.unrealized_pnl)}
                        </td>
                        <td className={cn('py-2 px-2 text-right', s.realized_pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                          {formatCurrency(s.realized_pnl)}
                        </td>
                        <td className={cn('py-2 px-2 text-right', s.total_return_pct >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]')}>
                          {s.total_return_pct?.toFixed(2)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </SectionCard>
        </>
      )}
    </div>
  )
}

export default AIBotAnalytics
