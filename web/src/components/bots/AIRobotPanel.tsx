import React, { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  BrainCircuit,
  Activity,
  BarChart3,
  Shield,
  Clock,
  Gauge,
  Sparkles,
  TrendingUp,
  TrendingDown,
  Zap,
} from 'lucide-react'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { Slider } from '@/components/ui/Slider'
import { Switch } from '@/components/ui/Switch'
import Select from '@/components/ui/Select'
import { Button } from '@/components/ui/Button'
import { EmptyState } from '@/components/ui/EmptyState'
import { KPIGrid, type KPICardItem } from '@/components/ui/KPICard'
import { AsyncDataWrapper } from '@/components/ui/AsyncDataWrapper'
import { cn } from '@/lib/utils'
import { aiRobotApi } from '@/lib/api'
import type { AIStatus, AISignal } from '@/types'

const modelOptions = [
  { value: 'deepseek', label: 'DeepSeek V3', description: '深度思考，适合策略分析' },
  { value: 'openai', label: 'GPT-4o', description: '通用能力强，适合多维度分析' },
  { value: 'claude', label: 'Claude 3.5', description: '擅长长文本，适合报告生成' },
]

function buildAIKPIItems(status: AIStatus): KPICardItem[] {
  return [
    {
      label: '今日信号',
      value: status.signals_today,
      icon: <Activity className="w-4 h-4 text-[#1890ff]" />,
      variant: 'info',
    },
    {
      label: '平均置信度',
      value: `${status.avg_confidence}%`,
      icon: <Gauge className="w-4 h-4 text-[#faad14]" />,
      variant: 'warning',
    },
    {
      label: '过滤率',
      value: `${status.filter_rate}%`,
      icon: <Shield className="w-4 h-4 text-[#52c41a]" />,
      variant: 'success',
    },
    {
      label: '胜率',
      value: `${status.win_rate}%`,
      icon:
        (status.win_rate || 0) > 50 ? (
          <TrendingUp className="w-4 h-4 text-[#52c41a]" />
        ) : (
          <TrendingDown className="w-4 h-4 text-[#f5222d]" />
        ),
      variant: (status.win_rate || 0) > 50 ? 'success' : 'error',
    },
  ]
}

export const AIRobotPanel: React.FC = () => {
  const [selectedModel, setSelectedModel] = useState('deepseek')
  const [confidenceThreshold, setConfidenceThreshold] = useState(60)
  const [scanInterval, setScanInterval] = useState(300)
  const [marketFilterEnabled, setMarketFilterEnabled] = useState(true)

  const { data: status, isLoading: statusLoading } = useQuery({
    queryKey: ['ai', 'status'],
    queryFn: () => aiRobotApi.getStatus(),
    refetchInterval: 10000,
  })

  const { data: signals, isLoading: signalsLoading } = useQuery({
    queryKey: ['ai', 'signals'],
    queryFn: () => aiRobotApi.getSignals({ limit: 20 }).then((signals) => signals || []),
    refetchInterval: 30000,
  })

  return (
    <div className="space-y-5">
      {/* Model Selection */}
      <SectionCard title="AI 模型配置">
        <div className="space-y-4">
          <div>
            <Select
              label="选择模型"
              value={selectedModel}
              onChange={(e) => setSelectedModel(e.target.value)}
              options={modelOptions.map((opt) => ({ value: opt.value, label: opt.label }))}
            />
            <p className="text-xs text-[#666] mt-1">
              {modelOptions.find((o) => o.value === selectedModel)?.description}
            </p>
          </div>

          <div>
            <label className="block text-sm text-[#aaa] mb-2">
              置信度门限: {confidenceThreshold}%
            </label>
            <Slider
              value={confidenceThreshold}
              onChange={setConfidenceThreshold}
              min={0}
              max={100}
              step={5}
            />
          </div>

          <div>
            <label className="block text-sm text-[#aaa] mb-2">
              扫描间隔: {Math.floor(scanInterval / 60)}分钟
            </label>
            <Slider
              value={scanInterval}
              onChange={setScanInterval}
              min={60}
              max={3600}
              step={60}
            />
          </div>

          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Shield className="w-4 h-4 text-[#52c41a]" />
              <span className="text-sm text-[#ccc]">市场条件过滤</span>
              <Badge variant="info">Beta</Badge>
            </div>
            <Switch checked={marketFilterEnabled} onCheckedChange={setMarketFilterEnabled} />
          </div>

          <Button variant="primary" className="w-full">
            <Sparkles className="w-4 h-4 mr-1" />
            保存配置
          </Button>
        </div>
      </SectionCard>

      {/* KPI */}
      <KPIGrid
        items={status ? buildAIKPIItems(status) : Array.from({ length: 4 }, (_, i) => ({ label: '-', value: '-', icon: null, variant: 'default' as const }))}
        isLoading={statusLoading}
      />

      {/* Recent Signals */}
      <SectionCard title="最近信号">
        <AsyncDataWrapper
          isLoading={signalsLoading}
          data={signals}
          skeleton={<Skeleton className="h-40 rounded-xl" />}
          empty={<EmptyState icon={<BrainCircuit className="w-8 h-8" />} title="暂无AI信号" />}
        >
          {(items) => (
            <div className="space-y-2" style={{ maxHeight: '400px', overflowY: 'auto' }}>
              {items.map((signal: AISignal) => (
                <div
                  key={signal.id}
                  className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-3"
                >
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <Badge
                        variant={
                          signal.side === 'buy'
                            ? 'success'
                            : signal.side === 'sell'
                              ? 'error'
                              : 'neutral'
                        }
                      >
                        {signal.side === 'buy' ? '买入' : signal.side === 'sell' ? '卖出' : '观望'}
                      </Badge>
                      <span className="text-sm font-medium text-[#e0e0e0]">
                        {signal.symbol}
                      </span>
                    </div>
                    <ConfidenceBadge value={signal.confidence} />
                  </div>

                  {signal.reason && (
                    <p className="text-xs text-[#888] mb-2 line-clamp-2">{signal.reason}</p>
                  )}

                  <div className="flex flex-wrap gap-1.5 mb-2">
                    {signal.filters?.map((f: string) => (
                      <Badge key={f} variant="info" className="text-[10px]">
                        {f}
                      </Badge>
                    ))}
                  </div>

                  <div className="flex items-center justify-between text-xs text-[#555]">
                    <span>
                      <Clock className="w-3 h-3 inline mr-1" />
                      {new Date(signal.timestamp).toLocaleTimeString('zh-CN')}
                    </span>
                    {signal.market_condition && (
                      <span>
                        <Zap className="w-3 h-3 inline mr-1" />
                        {signal.market_condition}
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </AsyncDataWrapper>
      </SectionCard>
    </div>
  )
}

const ConfidenceBadge: React.FC<{ value: number }> = ({ value }) => {
  const variant = value >= 80 ? 'success' : value >= 60 ? 'warning' : 'error'
  const label = value >= 80 ? '高' : value >= 60 ? '中' : '低'
  return (
    <Badge variant={variant}>
      {value}% {label}
    </Badge>
  )
}

export default AIRobotPanel
