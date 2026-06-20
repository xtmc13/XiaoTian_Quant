import React, { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  ShieldAlert,
  Activity,
  TrendingUp,
  TrendingDown,
  Gauge,
  Wallet,
  BarChart3,
  AlertTriangle,
} from 'lucide-react'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Slider } from '@/components/ui/Slider'
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from '@/components/ui/Select'
import { Switch } from '@/components/ui/Switch'
import { Button } from '@/components/ui/Button'
import { EmptyState } from '@/components/ui/EmptyState'
import { KPIGrid, type KPICardItem } from '@/components/ui/KPICard'
import { AsyncDataWrapper } from '@/components/ui/AsyncDataWrapper'
import { cn, formatCurrency } from '@/lib/utils'
import { contractApi } from '@/lib/api'
import type { ContractStatus } from '@/types'

function buildContractKPIs(status: ContractStatus): KPICardItem[] {
  return [
    {
      label: '杠杆倍数',
      value: `${status.leverage}x`,
      icon: <Gauge className="w-4 h-4 text-[#1890ff]" />,
      variant: 'info',
    },
    {
      label: '可用保证金',
      value: formatCurrency(status.available_margin),
      icon: <Wallet className="w-4 h-4 text-[#52c41a]" />,
      variant: 'success',
    },
    {
      label: '保证金率',
      value: `${(status.margin_ratio * 100).toFixed(1)}%`,
      icon:
        status.margin_ratio > 0.5 ? (
          <TrendingUp className="w-4 h-4 text-[#52c41a]" />
        ) : status.margin_ratio > 0.25 ? (
          <TrendingDown className="w-4 h-4 text-[#faad14]" />
        ) : (
          <AlertTriangle className="w-4 h-4 text-[#f5222d]" />
        ),
      variant:
        status.margin_ratio > 0.5
          ? 'success'
          : status.margin_ratio > 0.25
            ? 'warning'
            : 'error',
    },
    {
      label: '强平价',
      value: status.liquidation_price?.toFixed(2) || '—',
      icon: <ShieldAlert className="w-4 h-4 text-[#f5222d]" />,
      variant: 'error',
    },
  ]
}

export const ContractPanel: React.FC = () => {
  const [leverage, setLeverage] = useState(20)
  const [direction, setDirection] = useState('both')
  const [marginMode, setMarginMode] = useState('cross')
  const [indicator, setIndicator] = useState('macd_golden')
  const [timeframe, setTimeframe] = useState('1h')
  const [trendFollowing, setTrendFollowing] = useState(false)
  const [maxPositions, setMaxPositions] = useState(10)

  const { data: status, isLoading } = useQuery({
    queryKey: ['contract', 'status'],
    queryFn: () => contractApi.getMarginInfo().then((r) => r.data),
    refetchInterval: 10000,
  })

  const alertLevel = status?.margin_ratio
    ? status.margin_ratio > 0.5
      ? 'safe'
      : status.margin_ratio > 0.25
        ? 'warning'
        : 'danger'
    : 'safe'

  return (
    <div className="space-y-5">
      {/* Alert Banner */}
      <div
        className={cn(
          'rounded-xl border p-3 flex items-center gap-2',
          alertLevel === 'safe' && 'border-[#52c41a]/20 bg-[#52c41a]/5',
          alertLevel === 'warning' && 'border-[#faad14]/20 bg-[#faad14]/5',
          alertLevel === 'danger' && 'border-[#f5222d]/20 bg-[#f5222d]/5'
        )}
      >
        {alertLevel === 'safe' && <Activity className="w-4 h-4 text-[#52c41a]" />}
        {alertLevel === 'warning' && <AlertTriangle className="w-4 h-4 text-[#faad14]" />}
        {alertLevel === 'danger' && <ShieldAlert className="w-4 h-4 text-[#f5222d]" />}
        <span
          className={cn(
            'text-sm font-medium',
            alertLevel === 'safe' && 'text-[#52c41a]',
            alertLevel === 'warning' && 'text-[#faad14]',
            alertLevel === 'danger' && 'text-[#f5222d]'
          )}
        >
          {alertLevel === 'safe' && '账户状态正常'}
          {alertLevel === 'warning' && '保证金不足预警'}
          {alertLevel === 'danger' && '强平风险！请立即补充保证金'}
        </span>
      </div>

      {/* KPI */}
      <KPIGrid
        items={status ? buildContractKPIs(status) : Array.from({ length: 4 }, (_, i) => ({ label: '-', value: '-', icon: null, variant: 'default' as const }))}
        isLoading={isLoading}
      />

      {/* Configuration */}
      <SectionCard title="合约参数">
        <div className="space-y-4">
          {/* Leverage */}
          <div>
            <label className="block text-sm text-[#aaa] mb-2">杠杆倍数: {leverage}x</label>
            <Slider value={leverage} onChange={setLeverage} min={1} max={125} step={1} />
            <div className="flex justify-between text-[10px] text-[#555] mt-1">
              <span>1x</span>
              <span>25x</span>
              <span>50x</span>
              <span>75x</span>
              <span>100x</span>
              <span>125x</span>
            </div>
          </div>

          {/* Direction */}
          <div>
            <label className="block text-sm text-[#aaa] mb-2">开仓方向</label>
            <Select value={direction} onValueChange={setDirection}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="long">仅做多</SelectItem>
                <SelectItem value="short">仅做空</SelectItem>
                <SelectItem value="both">双向</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Margin Mode */}
          <div>
            <label className="block text-sm text-[#aaa] mb-2">保证金模式</label>
            <Select value={marginMode} onValueChange={setMarginMode}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="isolated">逐仓</SelectItem>
                <SelectItem value="cross">全仓</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Open Indicator */}
          <div>
            <label className="block text-sm text-[#aaa] mb-2">开仓指标</label>
            <Select value={indicator} onValueChange={setIndicator}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="macd_golden">MACD 金叉死叉</SelectItem>
                <SelectItem value="ema_counter">EMA60 逆势</SelectItem>
                <SelectItem value="ema_follow">EMA60+10 顺势</SelectItem>
                <SelectItem value="none">无指标</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Timeframe */}
          <div>
            <label className="block text-sm text-[#aaa] mb-2">指标时间周期</label>
            <Select value={timeframe} onValueChange={setTimeframe}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="5m">5分钟</SelectItem>
                <SelectItem value="15m">15分钟</SelectItem>
                <SelectItem value="30m">30分钟</SelectItem>
                <SelectItem value="1h">1小时</SelectItem>
                <SelectItem value="4h">4小时</SelectItem>
                <SelectItem value="8h">8小时</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Trend Following */}
          <div className="flex items-center justify-between">
            <span className="text-sm text-[#ccc]">顺势而为 (对冲亏损)</span>
            <Switch checked={trendFollowing} onCheckedChange={setTrendFollowing} />
          </div>

          {/* Max Positions */}
          <div>
            <label className="block text-sm text-[#aaa] mb-2">限制在线单量: {maxPositions}</label>
            <Slider value={maxPositions} onChange={setMaxPositions} min={1} max={50} step={1} />
          </div>

          <Button variant="primary" className="w-full">
            保存配置
          </Button>
        </div>
      </SectionCard>

      {/* Positions Summary */}
      <SectionCard title="持仓概要">
        <EmptyState icon={<BarChart3 className="w-8 h-8" />} title="暂无合约持仓" />
      </SectionCard>
    </div>
  )
}

export default ContractPanel
