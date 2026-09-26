import React, { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  BrainCircuit,
  Activity,
  Shield,
  Clock,
  Gauge,
  Sparkles,
  TrendingUp,
  TrendingDown,
  Zap,
  Power,
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
import type { AIStatus, AIRobotConfig, AISignal } from '@/types'
import { toast } from '@/lib/useToast'

/** 空配置 / 接口不可用时的兜底默认值 */
const DEFAULT_CONFIG: AIRobotConfig = {
  model: 'deepseek',
  confidence_threshold: 60,
  scan_interval_seconds: 300,
  market_filters: {
    min_volume_24h: 1000000,
    max_volatility: 5,
    trend_timeframe: '1h',
    require_trend_alignment: true,
    filter_whitelist_only: false,
  },
  enabled: false,
}

/** getModels 不可用时的静态兜底列表 */
const FALLBACK_MODELS = ['deepseek', 'claude', 'qwen']

const MODEL_DESCRIPTIONS: Record<string, string> = {
  deepseek: '深度思考，适合策略分析',
  claude: '擅长长文本，适合报告生成',
  qwen: '通义千问，响应快成本低',
  openai: '通用能力强，适合多维度分析',
  gpt: '通用能力强，适合多维度分析',
}

function modelLabel(id: string): string {
  const known: Record<string, string> = {
    deepseek: 'DeepSeek',
    claude: 'Claude',
    qwen: 'Qwen',
    openai: 'GPT',
    gpt: 'GPT',
  }
  for (const key of Object.keys(known)) {
    if (id.toLowerCase().includes(key)) return `${known[key]} (${id})`
  }
  return id
}

type SignalDirection = 'long' | 'short' | 'neutral'

function resolveDirection(signal: AISignal): SignalDirection {
  const raw = signal.signal ?? signal.side
  if (raw === 'long' || raw === 'buy') return 'long'
  if (raw === 'short' || raw === 'sell') return 'short'
  return 'neutral'
}

const DIRECTION_META: Record<SignalDirection, { label: string; badge: 'success' | 'error' | 'neutral' }> = {
  long: { label: '做多', badge: 'success' },
  short: { label: '做空', badge: 'error' },
  neutral: { label: '观望', badge: 'neutral' },
}

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

interface ConfigForm {
  model: string
  confidence_threshold: number
  scan_interval_seconds: number
  enabled: boolean
  /** 映射到 market_filters.require_trend_alignment */
  market_filter_enabled: boolean
}

export const AIRobotPanel: React.FC = () => {
  const queryClient = useQueryClient()
  const [form, setForm] = useState<ConfigForm>({
    model: DEFAULT_CONFIG.model,
    confidence_threshold: DEFAULT_CONFIG.confidence_threshold,
    scan_interval_seconds: DEFAULT_CONFIG.scan_interval_seconds,
    enabled: DEFAULT_CONFIG.enabled,
    market_filter_enabled: DEFAULT_CONFIG.market_filters.require_trend_alignment,
  })

  // 进入面板拉取已保存配置（空配置/404 时保留默认值）
  const { data: config, isLoading: configLoading } = useQuery({
    queryKey: ['ai-robot', 'config'],
    queryFn: () => aiRobotApi.getConfig(),
    retry: false,
  })

  useEffect(() => {
    if (!config || typeof config !== 'object' || !config.model) return
    setForm({
      model: config.model,
      confidence_threshold: config.confidence_threshold ?? DEFAULT_CONFIG.confidence_threshold,
      scan_interval_seconds: config.scan_interval_seconds ?? DEFAULT_CONFIG.scan_interval_seconds,
      enabled: config.enabled ?? DEFAULT_CONFIG.enabled,
      market_filter_enabled:
        config.market_filters?.require_trend_alignment ??
        DEFAULT_CONFIG.market_filters.require_trend_alignment,
    })
  }, [config])

  // 模型列表（接口失败/为空时回退静态列表）
  const { data: models } = useQuery({
    queryKey: ['ai-robot', 'models'],
    queryFn: () => aiRobotApi.getModels(),
    staleTime: 5 * 60 * 1000,
  })

  const modelOptions = useMemo(() => {
    const list = models && models.length > 0 ? models : FALLBACK_MODELS
    return list.map((id) => ({ value: id, label: modelLabel(id) }))
  }, [models])

  const saveMutation = useMutation({
    mutationFn: (next: ConfigForm) =>
      aiRobotApi.saveConfig({
        ...(config && config.id ? { id: config.id } : {}),
        model: next.model,
        confidence_threshold: next.confidence_threshold,
        scan_interval_seconds: next.scan_interval_seconds,
        market_filters: {
          ...(config?.market_filters ?? DEFAULT_CONFIG.market_filters),
          require_trend_alignment: next.market_filter_enabled,
        },
        enabled: next.enabled,
      }),
    onSuccess: () => {
      toast('success', 'AI 机器人配置已保存')
      queryClient.invalidateQueries({ queryKey: ['ai-robot', 'config'] })
    },
  })

  const { data: status, isLoading: statusLoading } = useQuery({
    queryKey: ['ai', 'status'],
    queryFn: () => aiRobotApi.getStatus(),
    refetchInterval: 10000,
  })

  // 信号流：10s 轮询
  const { data: signals, isLoading: signalsLoading } = useQuery({
    queryKey: ['ai', 'signals'],
    queryFn: () => aiRobotApi.getSignals({ limit: 50 }),
    refetchInterval: 10000,
  })

  return (
    <div className="space-y-5">
      {/* Model Selection */}
      <SectionCard title="AI 模型配置">
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Power className={cn('w-4 h-4', form.enabled ? 'text-[#52c41a]' : 'text-[#555]')} />
              <span className="text-sm text-[#ccc]">启用 AI 扫描机器人</span>
            </div>
            <Switch
              checked={form.enabled}
              onCheckedChange={(checked) => setForm((f) => ({ ...f, enabled: checked }))}
              aria-label="启用 AI 扫描机器人"
            />
          </div>

          <div>
            <Select
              label="选择模型"
              value={form.model}
              onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
              options={modelOptions}
              disabled={configLoading}
            />
            <p className="text-xs text-[#666] mt-1">
              {MODEL_DESCRIPTIONS[form.model] ?? '由后端 /ai-robot/models 提供'}
            </p>
          </div>

          <div>
            <label className="block text-sm text-[#aaa] mb-2">
              置信度门限: {form.confidence_threshold}%
            </label>
            <Slider
              value={form.confidence_threshold}
              onChange={(v) => setForm((f) => ({ ...f, confidence_threshold: v }))}
              min={0}
              max={100}
              step={5}
              ariaLabel="置信度门限"
            />
          </div>

          <div>
            <label className="block text-sm text-[#aaa] mb-2">
              扫描间隔: {Math.floor(form.scan_interval_seconds / 60)}分钟
            </label>
            <Slider
              value={form.scan_interval_seconds}
              onChange={(v) => setForm((f) => ({ ...f, scan_interval_seconds: v }))}
              min={60}
              max={3600}
              step={60}
              ariaLabel="扫描间隔（分钟）"
            />
          </div>

          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Shield className="w-4 h-4 text-[#52c41a]" />
              <span className="text-sm text-[#ccc]">市场条件过滤</span>
              <Badge variant="info">Beta</Badge>
            </div>
            <Switch
              checked={form.market_filter_enabled}
              onCheckedChange={(checked) => setForm((f) => ({ ...f, market_filter_enabled: checked }))}
              aria-label="市场条件过滤"
            />
          </div>

          <Button
            variant="primary"
            className="w-full"
            onClick={() => saveMutation.mutate(form)}
            disabled={saveMutation.isPending}
          >
            <Sparkles className="w-4 h-4 mr-1" />
            {saveMutation.isPending ? '保存中...' : '保存配置'}
          </Button>
        </div>
      </SectionCard>

      {/* KPI */}
      <KPIGrid
        items={status ? buildAIKPIItems(status) : Array.from({ length: 4 }, (_, i) => ({ label: '-', value: '-', icon: null, variant: 'default' as const }))}
        isLoading={statusLoading}
      />

      {/* 信号流 */}
      <SectionCard title="信号流">
        <AsyncDataWrapper
          isLoading={signalsLoading}
          data={signals}
          skeleton={<Skeleton className="h-40 rounded-xl" />}
          empty={
            <EmptyState
              icon={<BrainCircuit className="w-8 h-8" />}
              title="暂无信号——开启扫描后这里会显示 AI 实时信号"
            />
          }
        >
          {(items) => (
            <div className="space-y-2" style={{ maxHeight: '480px', overflowY: 'auto' }}>
              {items.map((signal: AISignal) => {
                const direction = resolveDirection(signal)
                const meta = DIRECTION_META[direction]
                const time = signal.created_at || signal.timestamp
                return (
                  <div
                    key={signal.id}
                    className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-3"
                  >
                    <div className="flex items-center justify-between mb-2">
                      <div className="flex items-center gap-2">
                        <Badge variant={meta.badge}>{meta.label}</Badge>
                        <span className="text-sm font-medium text-[#e0e0e0]">
                          {signal.symbol}
                        </span>
                        {signal.mode && (
                          <Badge variant="default" className="text-[10px]">
                            {signal.mode}
                          </Badge>
                        )}
                      </div>
                      <ConfidenceBadge value={signal.confidence} />
                    </div>

                    <ConfidenceBar value={signal.confidence} />

                    {signal.reason && (
                      <p className="text-xs text-[#888] mt-2 mb-2 line-clamp-2">{signal.reason}</p>
                    )}

                    {signal.filters && signal.filters.length > 0 && (
                      <div className="flex flex-wrap gap-1.5 mb-2">
                        {signal.filters.map((f: string) => (
                          <Badge key={f} variant="info" className="text-[10px]">
                            {f}
                          </Badge>
                        ))}
                      </div>
                    )}

                    <div className="flex items-center justify-between text-xs text-[#555]">
                      <span>
                        <Clock className="w-3 h-3 inline mr-1" />
                        {time ? new Date(time).toLocaleString('zh-CN') : '-'}
                      </span>
                      {signal.market_condition && (
                        <span>
                          <Zap className="w-3 h-3 inline mr-1" />
                          {signal.market_condition}
                        </span>
                      )}
                      {signal.provider && <span>{signal.provider}</span>}
                    </div>
                  </div>
                )
              })}
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

const ConfidenceBar: React.FC<{ value: number }> = ({ value }) => {
  const color =
    value >= 80 ? 'bg-[#52c41a]' : value >= 60 ? 'bg-[#faad14]' : 'bg-[#f5222d]'
  return (
    <div
      className="h-1 rounded-full bg-[#2a2a2a] overflow-hidden"
      role="progressbar"
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <div className={cn('h-full rounded-full transition-all', color)} style={{ width: `${value}%` }} />
    </div>
  )
}

export default AIRobotPanel
