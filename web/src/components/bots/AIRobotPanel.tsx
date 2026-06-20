import React, { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  BrainCircuit,
  Save,
  Signal,
  BarChart3,
  Volume2,
  TrendingUp,
  Filter,
  Zap,
  ChevronRight,
  AlertCircle,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Switch } from '@/components/ui/Switch'
import { Slider } from '@/components/ui/Slider'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { aiRobotApi } from '@/lib/api'
import type { AIRobotConfig, AISignal } from '@/types'

const TIMEFRAME_OPTIONS = [
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '30m', label: '30 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '4h', label: '4 小时' },
  { value: '1d', label: '1 天' },
]

interface AIConfigFormValues {
  model: string
  confidence_threshold: number
  scan_interval_seconds: number
  min_volume_24h: number
  max_volatility: number
  trend_timeframe: string
  require_trend_alignment: boolean
  filter_whitelist_only: boolean
}

const DEFAULT_VALUES: AIConfigFormValues = {
  model: 'gpt-4',
  confidence_threshold: 0.7,
  scan_interval_seconds: 60,
  min_volume_24h: 1000000,
  max_volatility: 5,
  trend_timeframe: '1h',
  require_trend_alignment: true,
  filter_whitelist_only: false,
}

/* ── Recent Signals ── */
interface RecentSignalsProps {
  signals: AISignal[] | undefined
  isLoading: boolean
}

const RecentSignalsView: React.FC<RecentSignalsProps> = ({ signals, isLoading }) => {
  if (isLoading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="h-12 rounded-lg" />
        ))}
      </div>
    )
  }

  if (!signals || signals.length === 0) {
    return (
      <div className="text-center py-8 text-[#555]">
        <Signal className="w-8 h-8 mx-auto mb-2 opacity-50" />
        <p className="text-sm">暂无 AI 信号</p>
      </div>
    )
  }

  const getConfidenceColor = (c: number) => {
    if (c >= 0.8) return 'text-[#52c41a]'
    if (c >= 0.6) return 'text-[#faad14]'
    return 'text-[#f5222d]'
  }

  const getConfidenceBg = (c: number) => {
    if (c >= 0.8) return 'bg-[#52c41a]/10 border-[#52c41a]/20'
    if (c >= 0.6) return 'bg-[#faad14]/10 border-[#faad14]/20'
    return 'bg-[#f5222d]/10 border-[#f5222d]/20'
  }

  return (
    <div className="space-y-2 max-h-96 overflow-y-auto">
      {signals.map((sig) => (
        <div
          key={sig.id}
          className={cn(
            'flex items-center justify-between rounded-xl border p-3 transition-all',
            getConfidenceBg(sig.confidence)
          )}
        >
          <div className="flex items-center gap-3">
            <Badge variant={sig.side === 'buy' ? 'success' : 'error'}>
              {sig.side === 'buy' ? '买入' : '卖出'}
            </Badge>
            <span className="text-sm font-medium text-[#e0e0e0]">{sig.symbol}</span>
            <span className="text-xs text-[#888]">{sig.model}</span>
          </div>
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1">
              <Zap className={cn('w-3 h-3', getConfidenceColor(sig.confidence))} />
              <span className={cn('text-sm font-semibold', getConfidenceColor(sig.confidence))}>
                {(sig.confidence * 100).toFixed(0)}%
              </span>
            </div>
            <span className="text-[10px] text-[#555]">
              {new Date(sig.created_at).toLocaleTimeString()}
            </span>
            {sig.executed && (
              <Badge variant="success" className="text-[10px]">已执行</Badge>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

/* ── Main Panel ── */
export const AIRobotPanel: React.FC = () => {
  const queryClient = useQueryClient()
  const [savedOk, setSavedOk] = useState(false)

  const { data: config, isLoading: configLoading } = useQuery({
    queryKey: ['ai-robot', 'config'],
    queryFn: () => aiRobotApi.getConfig().then((r) => r.data),
  })

  const { data: models } = useQuery({
    queryKey: ['ai-robot', 'models'],
    queryFn: () => aiRobotApi.getModels().then((r) => r.data?.models || []),
  })

  const { data: signals, isLoading: signalsLoading } = useQuery({
    queryKey: ['ai-robot', 'signals'],
    queryFn: () => aiRobotApi.getSignals({ limit: 50 }).then((r) => r.data?.signals || []),
    refetchInterval: 30000,
  })

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<AIConfigFormValues>({
    defaultValues: DEFAULT_VALUES,
  })

  const confidenceThreshold = watch('confidence_threshold')
  const scanInterval = watch('scan_interval_seconds')
  const minVolume = watch('min_volume_24h')
  const maxVolatility = watch('max_volatility')
  const trendAlignment = watch('require_trend_alignment')
  const whitelistOnly = watch('filter_whitelist_only')

  React.useEffect(() => {
    if (config) {
      reset({
        model: config.model || 'gpt-4',
        confidence_threshold: config.confidence_threshold ?? 0.7,
        scan_interval_seconds: config.scan_interval_seconds ?? 60,
        min_volume_24h: config.market_filters?.min_volume_24h ?? 1000000,
        max_volatility: config.market_filters?.max_volatility ?? 5,
        trend_timeframe: config.market_filters?.trend_timeframe || '1h',
        require_trend_alignment: config.market_filters?.require_trend_alignment ?? true,
        filter_whitelist_only: config.market_filters?.filter_whitelist_only ?? false,
      })
    }
  }, [config, reset])

  const saveMutation = useMutation({
    mutationFn: (data: AIRobotConfig) => aiRobotApi.saveConfig(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ai-robot', 'config'] })
      setSavedOk(true)
      setTimeout(() => setSavedOk(false), 3000)
    },
  })

  const handleFormSubmit = (values: AIConfigFormValues) => {
    const payload: AIRobotConfig = {
      model: values.model,
      confidence_threshold: values.confidence_threshold,
      scan_interval_seconds: values.scan_interval_seconds,
      enabled: true,
      market_filters: {
        min_volume_24h: values.min_volume_24h,
        max_volatility: values.max_volatility,
        trend_timeframe: values.trend_timeframe,
        require_trend_alignment: values.require_trend_alignment,
        filter_whitelist_only: values.filter_whitelist_only,
      },
    }
    saveMutation.mutate(payload)
  }

  const modelOptions = (models || ['gpt-4', 'gpt-4o', 'gpt-3.5-turbo', 'claude-3']).map((m) => ({
    value: m,
    label: m,
  }))

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-5">
      {/* Model Selection */}
      <SectionCard title="AI 模型配置" headerAction={<BrainCircuit className="w-4 h-4 text-[#722ed1]" />}>
        <div className="space-y-4">
          <Select
            label="AI 模型"
            options={modelOptions}
            {...register('model')}
          />
          <div>
            <Slider
              label="置信度门限"
              min={0.1}
              max={1.0}
              step={0.05}
              value={confidenceThreshold}
              onChange={(v) => setValue('confidence_threshold', v)}
              valueFormatter={(v) => `${(v * 100).toFixed(0)}%`}
            />
            <p className="text-[10px] text-[#555] mt-1">
              低于此门限的信号将被过滤掉
            </p>
          </div>
          <Input
            label="扫描间隔 (秒)"
            type="number"
            min={10}
            max={3600}
            helperText="AI 扫描市场的间隔时间"
            {...register('scan_interval_seconds', {
              required: true,
              valueAsNumber: true,
              min: { value: 10, message: '最小10秒' },
              max: { value: 3600, message: '最大3600秒' },
            })}
            error={errors.scan_interval_seconds?.message}
          />
        </div>
      </SectionCard>

      {/* Market Filters */}
      <SectionCard title="市场条件过滤" headerAction={<Filter className="w-4 h-4 text-[#1890ff]" />}>
        <div className="space-y-4">
          <Input
            label="最小24H成交量 (USDT)"
            type="number"
            min={0}
            step={100000}
            leftIcon={<Volume2 className="w-4 h-4" />}
            {...register('min_volume_24h', {
              required: true,
              valueAsNumber: true,
            })}
            error={errors.min_volume_24h?.message}
          />
          <Input
            label="最大波动率限制 (%)"
            type="number"
            min={0.1}
            max={20}
            step={0.1}
            leftIcon={<AlertCircle className="w-4 h-4" />}
            helperText="超过此波动率的市场将被忽略"
            {...register('max_volatility', {
              required: true,
              valueAsNumber: true,
              min: { value: 0.1, message: '最小0.1%' },
              max: { value: 20, message: '最大20%' },
            })}
            error={errors.max_volatility?.message}
          />
          <Select
            label="趋势周期"
            options={TIMEFRAME_OPTIONS}
            {...register('trend_timeframe')}
          />
          <div className="flex items-center gap-4 pt-1">
            <Switch
              label="要求趋势对齐"
              checked={trendAlignment}
              onChange={(e) => setValue('require_trend_alignment', e.target.checked)}
            />
            <Switch
              label="仅白名单币种"
              checked={whitelistOnly}
              onChange={(e) => setValue('filter_whitelist_only', e.target.checked)}
            />
          </div>
        </div>
      </SectionCard>

      {/* Config Summary */}
      <div className="flex flex-wrap gap-2">
        <Badge variant="info">
          <BrainCircuit className="w-3 h-3 inline mr-1" />
          {watch('model') || 'gpt-4'}
        </Badge>
        <Badge variant={confidenceThreshold >= 0.7 ? 'success' : 'warning'}>
          置信度 {(confidenceThreshold * 100).toFixed(0)}%
        </Badge>
        <Badge variant="neutral">
          扫描 {scanInterval}s
        </Badge>
        <Badge variant="neutral">
          <Volume2 className="w-3 h-3 inline mr-1" />
          最低 ${(minVolume / 1e6).toFixed(1)}M
        </Badge>
        {trendAlignment && (
          <Badge variant="success">
            <TrendingUp className="w-3 h-3 inline mr-1" />
            趋势对齐
          </Badge>
        )}
        {whitelistOnly && (
          <Badge variant="warning">白名单</Badge>
        )}
      </div>

      {/* Save Button */}
      <div className="flex items-center gap-3 pt-2">
        <Button
          type="submit"
          variant="primary"
          isLoading={saveMutation.isPending}
          leftIcon={<Save className="w-4 h-4" />}
        >
          保存 AI 配置
        </Button>
        {savedOk && (
          <Badge variant="success">保存成功</Badge>
        )}
      </div>

      {/* Recent Signals */}
      <SectionCard title="最近信号" headerAction={<BarChart3 className="w-4 h-4 text-[#52c41a]" />}>
        <RecentSignalsView signals={signals} isLoading={signalsLoading} />
      </SectionCard>
    </form>
  )
}

export default AIRobotPanel
