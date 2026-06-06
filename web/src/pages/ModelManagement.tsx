import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { mlApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import {
  BrainCircuit,
  Plus,
  Trash2,
  Play,
  Loader2,
  AlertCircle,
  CheckCircle2,
  XCircle,
  BarChart3,
  Activity,
  Clock,
  Database,
  Sparkles,
  ChevronDown,
  ChevronUp,
  Eye,
  RefreshCw,
  Zap,
  Layers,
  Gauge,
} from 'lucide-react'

/* ── Types ── */
interface ModelInfo {
  model_id: string
  model_type: string
  task_type: string
  trained_at: string
  metrics: Record<string, number>
  feature_count: number
}

interface TrainResult {
  success: boolean
  model_id: string
  symbol: string
  bars_loaded: number
  features_generated: number
  train_samples: number
  test_samples: number
  metrics: Record<string, number>
  feature_names: string[]
  duration_ms: number
  error?: string
}

/* ── Constants ── */
const INTERVALS = [
  { value: '1m', label: '1分钟' },
  { value: '5m', label: '5分钟' },
  { value: '15m', label: '15分钟' },
  { value: '30m', label: '30分钟' },
  { value: '1h', label: '1小时' },
  { value: '4h', label: '4小时' },
  { value: '1d', label: '日线' },
]

const MODEL_TYPES = [
  { value: 'lightgbm', label: 'LightGBM' },
  { value: 'xgboost', label: 'XGBoost' },
]

const TASK_TYPES = [
  { value: 'regression', label: '回归 (预测收益率)' },
  { value: 'classification', label: '分类 (预测方向)' },
]

/* ── Page ── */
export function ModelManagement() {
  const queryClient = useQueryClient()
  const [expandedModel, setExpandedModel] = useState<string | null>(null)
  const [showTrainForm, setShowTrainForm] = useState(false)

  // Train form state
  const [trainConfig, setTrainConfig] = useState({
    symbol: 'BTCUSDT',
    interval: '1h',
    model_id: '',
    model_type: 'lightgbm',
    task_type: 'regression',
    lookback_days: 90,
    feature_periods: [5, 10, 20, 50],
    label_horizon: 5,
  })

  // Queries
  const { data: models, isLoading: modelsLoading } = useQuery({
    queryKey: ['ml-models'],
    queryFn: () => mlApi.list(),
  })

  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['ml-health'],
    queryFn: async () => {
      try {
        return await mlApi.health()
      } catch {
        return { status: 'unhealthy' }
      }
    },
    refetchInterval: 30000,
  })

  // Mutations
  const trainMutation = useMutation({
    mutationFn: mlApi.train,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ml-models'] })
      setShowTrainForm(false)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => mlApi.deleteModel(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['ml-models'] }),
  })

  const handleTrain = useCallback(() => {
    trainMutation.mutate({
      ...trainConfig,
      model_id: trainConfig.model_id || undefined,
    })
  }, [trainConfig, trainMutation])

  const isHealthy = health?.status === 'healthy'

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="ML 模型管理"
          subtitle="训练、评估和管理机器学习交易模型"
          actions={<BrainCircuit className="w-6 h-6 text-quant-gold" />}
        />

        {/* Status Bar */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="ML 服务器"
            value={healthLoading ? '...' : isHealthy ? '在线' : '离线'}
            icon={isHealthy ? <CheckCircle2 className="w-4 h-4 text-green-400" /> : <XCircle className="w-4 h-4 text-red-400" />}
            subValue={isHealthy ? '正常' : '请检查 ML 服务'}
            trend={isHealthy ? 'up' : 'down'}
          />
          <KPICard
            label="模型数量"
            value={models?.length ?? 0}
            icon={<Layers className="w-4 h-4 text-quant-gold" />}
            subValue="已训练"
            trend="up"
          />
          <KPICard
            label="最新训练"
            value={models && models.length > 0 ? new Date(models[0].trained_at).toLocaleDateString() : '-'}
            icon={<Clock className="w-4 h-4 text-quant-gold" />}
            subValue="最近"
            trend="neutral"
          />
          <KPICard
            label="特征维度"
            value={models && models.length > 0 ? models[0].feature_count : '-'}
            icon={<Database className="w-4 h-4 text-quant-gold" />}
            subValue="每模型"
            trend="up"
          />
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-foreground">模型列表</h2>
          <button
            onClick={() => setShowTrainForm(!showTrainForm)}
            className={cn(
              'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
              showTrainForm
                ? 'bg-quant-gold/10 text-quant-gold'
                : 'bg-quant-gold text-white hover:bg-quant-gold/90'
            )}
          >
            {showTrainForm ? <XCircle className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            {showTrainForm ? '取消' : '训练新模型'}
          </button>
        </div>

        {/* Train Form */}
        {showTrainForm && (
          <SectionCard title={<>训练配置 <Sparkles className="w-4 h-4 inline ml-1 text-quant-gold" /></>}>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">交易对</label>
                <input
                  type="text"
                  value={trainConfig.symbol}
                  onChange={(e) => setTrainConfig({ ...trainConfig, symbol: e.target.value.toUpperCase() })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  placeholder="BTCUSDT"
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">时间周期</label>
                <select
                  value={trainConfig.interval}
                  onChange={(e) => setTrainConfig({ ...trainConfig, interval: e.target.value })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                >
                  {INTERVALS.map((i) => (
                    <option key={i.value} value={i.value}>{i.label}</option>
                  ))}
                </select>
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">模型类型</label>
                <select
                  value={trainConfig.model_type}
                  onChange={(e) => setTrainConfig({ ...trainConfig, model_type: e.target.value })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                >
                  {MODEL_TYPES.map((t) => (
                    <option key={t.value} value={t.value}>{t.label}</option>
                  ))}
                </select>
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">任务类型</label>
                <select
                  value={trainConfig.task_type}
                  onChange={(e) => setTrainConfig({ ...trainConfig, task_type: e.target.value })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                >
                  {TASK_TYPES.map((t) => (
                    <option key={t.value} value={t.value}>{t.label}</option>
                  ))}
                </select>
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">回测天数</label>
                <input
                  type="number"
                  value={trainConfig.lookback_days}
                  onChange={(e) => setTrainConfig({ ...trainConfig, lookback_days: parseInt(e.target.value) || 90 })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  min={7}
                  max={365}
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-muted-foreground">预测周期 (K线数)</label>
                <input
                  type="number"
                  value={trainConfig.label_horizon}
                  onChange={(e) => setTrainConfig({ ...trainConfig, label_horizon: parseInt(e.target.value) || 5 })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  min={1}
                  max={50}
                />
              </div>
              <div className="space-y-1.5 md:col-span-2 lg:col-span-3">
                <label className="text-xs font-medium text-muted-foreground">模型 ID (可选，留空自动生成)</label>
                <input
                  type="text"
                  value={trainConfig.model_id}
                  onChange={(e) => setTrainConfig({ ...trainConfig, model_id: e.target.value })}
                  className="w-full px-3 py-2 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                  placeholder="btcusdt_1h_1234567890"
                />
              </div>
            </div>
            <div className="mt-4 flex items-center gap-3">
              <button
                onClick={handleTrain}
                disabled={trainMutation.isPending || !isHealthy}
                className={cn(
                  'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                  trainMutation.isPending || !isHealthy
                    ? 'bg-muted text-muted-foreground cursor-not-allowed'
                    : 'bg-quant-gold text-white hover:bg-quant-gold/90'
                )}
              >
                {trainMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
                {trainMutation.isPending ? '训练中...' : '开始训练'}
              </button>
              {!isHealthy && (
                <span className="text-xs text-red-400 flex items-center gap-1">
                  <AlertCircle className="w-3 h-3" />
                  ML 服务器离线，无法训练
                </span>
              )}
            </div>
            {trainMutation.isError && (
              <div className="mt-3 p-3 rounded-md bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
                训练失败: {trainMutation.error?.message || '未知错误'}
              </div>
            )}
            {trainMutation.isSuccess && (
              <div className="mt-3 p-3 rounded-md bg-green-500/10 border border-green-500/20 text-green-400 text-sm">
                <CheckCircle2 className="w-4 h-4 inline mr-1" />
                模型 {trainMutation.data?.model_id} 训练成功！
                耗时 {trainMutation.data?.duration_ms}ms，
                加载 {trainMutation.data?.bars_loaded} 条 K线，
                生成 {trainMutation.data?.features_generated} 个特征样本
              </div>
            )}
          </SectionCard>
        )}

        {/* Models List */}
        {modelsLoading ? (
          <div className="space-y-3">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-16 rounded-md bg-quant-bg-secondary animate-pulse" />
            ))}
          </div>
        ) : !models || models.length === 0 ? (
          <EmptyState
            icon={<BrainCircuit className="w-10 h-10 text-muted-foreground" />}
            title="暂无模型"
            description="点击上方「训练新模型」按钮创建您的第一个 ML 交易模型"
          />
        ) : (
          <div className="space-y-3">
            {models.map((model) => {
              const isExpanded = expandedModel === model.model_id
              return (
                <SectionCard
                  key={model.model_id}
                  className={cn('transition-all', isExpanded && 'ring-1 ring-quant-gold/30')}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="w-9 h-9 rounded-md bg-quant-gold/10 flex items-center justify-center shrink-0">
                        <BrainCircuit className="w-4 h-4 text-quant-gold" />
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-sm truncate">{model.model_id}</span>
                          <span className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-quant-gold/10 text-quant-gold">
                            {model.model_type}
                          </span>
                          <span className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-blue-500/10 text-blue-400">
                            {model.task_type}
                          </span>
                        </div>
                        <div className="flex items-center gap-3 text-xs text-muted-foreground mt-0.5">
                          <span className="flex items-center gap-1">
                            <Clock className="w-3 h-3" />
                            {new Date(model.trained_at).toLocaleString()}
                          </span>
                          <span className="flex items-center gap-1">
                            <Database className="w-3 h-3" />
                            {model.feature_count} 特征
                          </span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <button
                        onClick={() => setExpandedModel(isExpanded ? null : model.model_id)}
                        className="p-1.5 rounded-md hover:bg-white/5 text-muted-foreground transition-colors"
                        title="查看详情"
                      >
                        {isExpanded ? <ChevronUp className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                      </button>
                      <button
                        onClick={() => {
                          if (confirm(`确定删除模型 ${model.model_id} 吗？`)) {
                            deleteMutation.mutate(model.model_id)
                          }
                        }}
                        disabled={deleteMutation.isPending}
                        className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                        title="删除模型"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>

                  {/* Expanded Details */}
                  {isExpanded && (
                    <div className="mt-4 pt-4 border-t border-quant-border space-y-4">
                      {/* Metrics */}
                      {model.metrics && Object.keys(model.metrics).length > 0 && (
                        <div>
                          <h4 className="text-xs font-medium text-muted-foreground mb-2 flex items-center gap-1.5">
                            <Gauge className="w-3 h-3" />
                            训练指标
                          </h4>
                          <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                            {Object.entries(model.metrics).map(([key, value]) => (
                              <div key={key} className="p-2.5 rounded-md bg-quant-bg-secondary">
                                <div className="text-[10px] text-muted-foreground uppercase">{key}</div>
                                <div className="text-sm font-semibold mt-0.5">
                                  {typeof value === 'number' ? value.toFixed(4) : value}
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Actions */}
                      <div className="flex items-center gap-2">
                        <button
                          onClick={async () => {
                            try {
                              const res = await mlApi.importance(model.model_id)
                              alert(`特征重要性:\n${JSON.stringify(res.importance?.slice(0, 10), null, 2)}`)
                            } catch (e: unknown) {
                              const err = e instanceof Error ? e : new Error(String(e))
                              alert('获取特征重要性失败: ' + err.message)
                            }
                          }}
                          className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-quant-bg-secondary text-xs font-medium hover:bg-white/5 transition-colors"
                        >
                          <BarChart3 className="w-3 h-3" />
                          特征重要性
                        </button>
                        <button
                          onClick={async () => {
                            try {
                              const res = await mlApi.deploy({
                                model_id: model.model_id,
                                strategy_id: 'ml_strategy_' + Date.now(),
                                symbol: 'BTCUSDT',
                              })
                              alert(`部署成功: ${JSON.stringify(res, null, 2)}`)
                            } catch (e: unknown) {
                              const err = e instanceof Error ? e : new Error(String(e))
                              alert('部署失败: ' + err.message)
                            }
                          }}
                          className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-quant-gold/10 text-quant-gold text-xs font-medium hover:bg-quant-gold/20 transition-colors"
                        >
                          <Zap className="w-3 h-3" />
                          部署到策略
                        </button>
                      </div>
                    </div>
                  )}
                </SectionCard>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
