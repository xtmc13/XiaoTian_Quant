import React, { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  CalendarClock,
  Signal,
  Target,
  TrendingUp,
  TrendingDown,
  BarChart3,
  Coins,
} from 'lucide-react'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { KPIGrid, type KPICardItem } from '@/components/ui/KPICard'
import { AsyncDataWrapper } from '@/components/ui/AsyncDataWrapper'
import { DataTable } from '@/components/DataTable'
import { EquityCurve } from '@/components/charts/EquityCurve'
import { formatCurrency } from '@/lib/utils'
import { executorApi } from '@/lib/api'
import type { ExecutorStats, ExecutorSymbolStat } from '@/types'

function buildStatsKPIItems(stats: ExecutorStats): KPICardItem[] {
  const pnlPositive = (stats.total_pnl || 0) >= 0
  return [
    {
      label: '总信号',
      value: stats.total_signals ?? 0,
      icon: <Signal className="w-4 h-4 text-[#1890ff]" />,
      variant: 'info',
    },
    {
      label: '今日信号',
      value: stats.today_signals ?? 0,
      icon: <Activity className="w-4 h-4 text-[#faad14]" />,
      variant: 'warning',
    },
    {
      label: '成功率',
      value: `${stats.success_rate ?? 0}%`,
      icon: <Target className="w-4 h-4 text-[#52c41a]" />,
      variant: 'success',
      ringProgress: stats.success_rate ?? 0,
    },
    {
      label: '阶梯达成',
      value: `T1 ${stats.tp1_rate ?? 0}%`,
      subValue: `T2 ${stats.tp2_rate ?? 0}% / T3 ${stats.tp3_rate ?? 0}%`,
      icon: <BarChart3 className="w-4 h-4 text-[#722ed1]" />,
      variant: 'default',
    },
    {
      label: '日均信号',
      value: stats.avg_signals_per_day ?? 0,
      icon: <CalendarClock className="w-4 h-4 text-[#13c2c2]" />,
      variant: 'default',
    },
    {
      label: '累计盈亏',
      value: `${pnlPositive ? '+' : ''}${formatCurrency(stats.total_pnl ?? 0)}`,
      icon: pnlPositive ? (
        <TrendingUp className="w-4 h-4 text-[#52c41a]" />
      ) : (
        <TrendingDown className="w-4 h-4 text-[#f5222d]" />
      ),
      variant: pnlPositive ? 'success' : 'error',
    },
  ]
}

const symbolColumns = [
  {
    key: 'symbol',
    title: '交易对',
    render: (item: ExecutorSymbolStat) => (
      <span className="text-[#e0e0e0] font-medium">{item.symbol}</span>
    ),
  },
  {
    key: 'signals',
    title: '信号数',
    render: (item: ExecutorSymbolStat) => <span>{item.signals}</span>,
  },
  {
    key: 'success_rate',
    title: '成功率',
    render: (item: ExecutorSymbolStat) => (
      <span className={(item.success_rate || 0) >= 50 ? 'text-[#52c41a]' : 'text-[#faad14]'}>
        {item.success_rate ?? 0}%
      </span>
    ),
  },
  {
    key: 'pnl',
    title: '盈亏',
    render: (item: ExecutorSymbolStat) => (
      <span className={(item.pnl || 0) >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]'}>
        {(item.pnl || 0) >= 0 ? '+' : ''}
        {formatCurrency(item.pnl || 0)}
      </span>
    ),
  },
]

/** 信号统计：KPI + 近 30 日盈亏曲线 + 按交易对汇总 */
export const ExecutorStatsPanel: React.FC = () => {
  const { data: stats, isLoading } = useQuery({
    queryKey: ['executor', 'stats'],
    queryFn: () => executorApi.getStats(),
    refetchInterval: 30000,
    retry: false,
  })

  const curvePoints = useMemo(() => {
    const curve = stats?.pnl_curve
    if (!curve || curve.length < 2) return []
    return curve
      .map((p, idx) => {
        const ts = typeof p.date === 'number' ? p.date : Date.parse(p.date)
        return { timestamp: Number.isNaN(ts) ? idx : ts, equity: p.pnl }
      })
      .sort((a, b) => a.timestamp - b.timestamp)
  }, [stats?.pnl_curve])

  return (
    <div className="space-y-5">
      {/* KPI 行 */}
      <KPIGrid
        items={
          stats
            ? buildStatsKPIItems(stats)
            : Array.from({ length: 6 }, () => ({ label: '-', value: '-', icon: null, variant: 'default' as const }))
        }
        isLoading={isLoading}
      />

      <AsyncDataWrapper
        isLoading={isLoading}
        data={stats}
        skeleton={
          <div className="space-y-4">
            <Skeleton className="h-44 rounded-xl" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        }
        empty={
          <EmptyState
            icon={<BarChart3 className="w-8 h-8" />}
            title="暂无信号统计数据"
            description="执行器产生信号后，这里会展示成功率、盈亏曲线与分交易对汇总"
          />
        }
      >
        {(s) => (
          <>
            {/* 近 30 日盈亏曲线 */}
            <div className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4">
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-2">
                  <Coins className="w-4 h-4 text-[#faad14]" />
                  <span className="text-sm font-medium text-[#e0e0e0]">近 30 日盈亏曲线</span>
                </div>
                <span
                  className={
                    (s.total_pnl || 0) >= 0 ? 'text-sm text-[#52c41a]' : 'text-sm text-[#f5222d]'
                  }
                >
                  累计 {(s.total_pnl || 0) >= 0 ? '+' : ''}
                  {formatCurrency(s.total_pnl || 0)}
                </span>
              </div>
              {curvePoints.length >= 2 ? (
                <EquityCurve data={curvePoints} height={180} />
              ) : (
                <EmptyState title="暂无盈亏数据" />
              )}
            </div>

            {/* 按交易对汇总 */}
            <div className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4">
              <div className="flex items-center gap-2 mb-3">
                <BarChart3 className="w-4 h-4 text-[#1890ff]" />
                <span className="text-sm font-medium text-[#e0e0e0]">分交易对统计</span>
              </div>
              <DataTable
                data={s.by_symbol || []}
                columns={symbolColumns}
                keyExtractor={(item) => item.symbol}
                emptyText="暂无分交易对数据"
              />
            </div>
          </>
        )}
      </AsyncDataWrapper>
    </div>
  )
}

export default ExecutorStatsPanel
