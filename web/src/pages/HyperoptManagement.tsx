import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { hyperoptApi, backtestApi } from '@/lib/api'
import { MODEL_INTERVALS } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import {
  FlaskConical,
  Play,
  Square,
  RefreshCw,
  TrendingUp,
  Target,
  Clock,
  CheckCircle2,
  AlertCircle,
  Trash2,
  ChevronDown,
  ChevronUp,
  BarChart3,
  Layers,
  Zap,
  Search,
} from 'lucide-react'

/* ── Types ── */
interface HyperoptJob {
  id: string
  strategy_type: string
  symbol: string
  interval: string
  status: 'running' | 'completed' | 'failed' | 'cancelled'
  best_score: number
  best_params: Record<string, unknown>
  trials_completed: number
  total_trials: number
  created_at: number
  updated_at: number
}

interface HyperoptSpace {
  name: string
  type: string
  low?: number
  high?: number
  choices?: string[]
}

const STRATEGIES = [
  { value: 'breakout', label: '突破策略' },
  { value: 'ema_cross', label: 'EMA交叉' },
  { value: 'macd', label: 'MACD' },
  { value: 'rsi', label: 'RSI' },
  { value: 'bollinger_bands', label: '布林带' },
  { value: 'grid', label: '网格交易' },
  { value: 'arbitrage', label: '套利' },
  { value: 'market_making', label: '做市' },
]

/* ── Page ── */
export function HyperoptManagement() {
  const queryClient = useQueryClient()
  const [showNewJob, setShowNewJob] = useState(false)
  const [showSpaces, setShowSpaces] = useState(false)
  const [selectedJob, setSelectedJob] = useState<string | null>(null)

  const [jobConfig, setJobConfig] = useState({
    strategy_type: 'breakout',
    symbol: 'BTCUSDT',
    interval: '1h',
    max_trials: 50,
    sampler: 'tpe',
  })

  // Queries
  const { data: jobs, isLoading: jobsLoading } = useQuery({
    queryKey: ['hyperopt-jobs'],
    queryFn: async () => {
      const res = await hyperoptApi.jobs()
      return res
    },
    refetchInterval: 10000,
  })

  const { data: spaces } = useQuery({
    queryKey: ['hyperopt-spaces', jobConfig.strategy_type],
    queryFn: async () => {
      const res = await hyperoptApi.spaces(jobConfig.strategy_type)
      return res
    },
    enabled: showSpaces,
  })

  const { data: jobDetail } = useQuery({
    queryKey: ['hyperopt-job', selectedJob],
    queryFn: async () => {
      if (!selectedJob) return null
      return await hyperoptApi.job(selectedJob)
    },
    enabled: !!selectedJob,
    refetchInterval: selectedJob ? 5000 : false,
  })

  // Mutations
  const startMutation = useMutation({
    mutationFn: () => hyperoptApi.start({
      strategy_type: jobConfig.strategy_type,
      symbol: jobConfig.symbol,
      interval: jobConfig.interval,
      max_trials: jobConfig.max_trials,
      sampler: jobConfig.sampler,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['hyperopt-jobs'] })
      setShowNewJob(false)
    },
  })

  const cancelMutation = useMutation({
    mutationFn: (id: string) => hyperoptApi.cancel(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['hyperopt-jobs'] }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => hyperoptApi.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['hyperopt-jobs'] }),
  })

  const runningCount = jobs?.filter((j) => j.status === 'running').length ?? 0
  const completedCount = jobs?.filter((j) => j.status === 'completed').length ?? 0

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="参数优化"
          subtitle="Hyperopt 自动寻找最优策略参数"
          actions={<FlaskConical className="w-6 h-6 text-quant-gold" />}
        />

        {/* KPI Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="运行中"
            value={runningCount}
            icon={<Zap className="w-4 h-4 text-quant-gold" />}
            subValue="任务"
            trend={runningCount > 0 ? 'up' : 'neutral'}
          />
          <KPICard
            label="已完成"
            value={completedCount}
            icon={<CheckCircle2 className="w-4 h-4 text-green-400" />}
            subValue="任务"
            trend="up"
          />
          <KPICard
            label="总任务"
            value={jobs?.length ?? 0}
            icon={<Layers className="w-4 h-4 text-quant-gold" />}
            subValue="全部"
            trend="neutral"
          />
          <KPICard
            label="最佳得分"
            value={jobs && jobs.length > 0
              ? Math.min(...jobs.filter((j) => j.best_score > 0).map((j) => j.best_score)).toFixed(4)
              : '-'}
            icon={<Target className="w-4 h-4 text-quant-gold" />}
            subValue="越低越好"
            trend="up"
          />
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-foreground">优化任务</h2>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowSpaces(!showSpaces)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium transition-colors',
                showSpaces ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg-secondary text-muted-foreground hover:text-foreground'
              )}
            >
              <Search className="w-3.5 h-3.5" />
              搜索空间
            </button>
            <button
              onClick={() => setShowNewJob(!showNewJob)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium transition-colors',
                showNewJob ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-gold text-white hover:bg-quant-gold/90'
              )}
            >
              <Play className="w-3.5 h-3.5" />
              {showNewJob ? '取消' : '新建任务'}
            </button>
          </div>
        </div>

        {/* New Job Form */}
        {showNewJob && (
          <SectionCard title="新建优化任务">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">策略类型</label>
                <select
                  value={jobConfig.strategy_type}
                  onChange={(e) => setJobConfig({ ...jobConfig, strategy_type: e.target.value })}
                  className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                >
                  {STRATEGIES.map((s) => (
                    <option key={s.value} value={s.value}>{s.label}</option>
                  ))}
                </select>
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">交易对</label>
                <input
                  type="text"
                  value={jobConfig.symbol}
                  onChange={(e) => setJobConfig({ ...jobConfig, symbol: e.target.value.toUpperCase() })}
                  className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">时间周期</label>
                <select
                  value={jobConfig.interval}
                  onChange={(e) => setJobConfig({ ...jobConfig, interval: e.target.value })}
                  className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                >
                  {MODEL_INTERVALS.map((i) => (
                    <option key={i.value} value={i.value}>{i.label}</option>
                  ))}
                </select>
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">最大迭代次数</label>
                <input
                  type="number"
                  value={jobConfig.max_trials}
                  onChange={(e) => setJobConfig({ ...jobConfig, max_trials: parseInt(e.target.value) || 50 })}
                  min={10}
                  max={500}
                  className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                />
              </div>
              <div className="space-y-1">
                <label className="text-xs text-muted-foreground">采样器</label>
                <select
                  value={jobConfig.sampler}
                  onChange={(e) => setJobConfig({ ...jobConfig, sampler: e.target.value })}
                  className="w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                >
                  <option value="tpe">TPE (贝叶斯优化)</option>
                  <option value="random">Random (随机搜索)</option>
                  <option value="grid">Grid (网格搜索)</option>
                </select>
              </div>
            </div>
            <div className="mt-3 flex items-center gap-3">
              <button
                onClick={() => startMutation.mutate()}
                disabled={startMutation.isPending}
                className={cn(
                  'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                  startMutation.isPending
                    ? 'bg-muted text-muted-foreground cursor-not-allowed'
                    : 'bg-quant-gold text-white hover:bg-quant-gold/90'
                )}
              >
                {startMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
                {startMutation.isPending ? '启动中...' : '开始优化'}
              </button>
              {startMutation.isError && (
                <span className="text-xs text-red-400">{startMutation.error?.message}</span>
              )}
            </div>
          </SectionCard>
        )}

        {/* Search Spaces */}
        {showSpaces && spaces && (
          <SectionCard title={`${STRATEGIES.find((s) => s.value === jobConfig.strategy_type)?.label} 搜索空间`}>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
              {spaces.map((space, i) => (
                <div key={i} className="p-2.5 rounded-md bg-quant-bg-secondary">
                  <div className="text-[10px] text-muted-foreground uppercase">{space.name}</div>
                  <div className="text-xs font-medium mt-0.5">
                    {space.type === 'categorical' && space.choices
                      ? space.choices.join(', ')
                      : space.low !== undefined && space.high !== undefined
                        ? `${space.low} ~ ${space.high}`
                        : space.type}
                  </div>
                </div>
              ))}
            </div>
          </SectionCard>
        )}

        {/* Jobs List */}
        {jobsLoading ? (
          <div className="space-y-2">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-16 rounded-md bg-quant-bg-secondary animate-pulse" />
            ))}
          </div>
        ) : !jobs || jobs.length === 0 ? (
          <EmptyState
            icon={<FlaskConical className="w-10 h-10 text-muted-foreground" />}
            title="暂无优化任务"
            description="点击「新建任务」开始策略参数优化"
          />
        ) : (
          <div className="space-y-2">
            {jobs.map((job) => {
              const isSelected = selectedJob === job.id
              return (
                <SectionCard
                  key={job.id}
                  className={cn('transition-all', isSelected && 'ring-1 ring-quant-gold/30')}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3 min-w-0">
                      <div className={cn(
                        'w-2 h-2 rounded-full shrink-0',
                        job.status === 'running' ? 'bg-green-400 animate-pulse' :
                        job.status === 'completed' ? 'bg-quant-gold' :
                        job.status === 'failed' ? 'bg-red-400' : 'bg-muted-foreground'
                      )} />
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium truncate">{job.id.slice(0, 16)}...</span>
                          <span className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-quant-gold/10 text-quant-gold">
                            {STRATEGIES.find((s) => s.value === job.strategy_type)?.label || job.strategy_type}
                          </span>
                          <span className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-blue-500/10 text-blue-400">
                            {job.symbol}
                          </span>
                        </div>
                        <div className="flex items-center gap-3 text-xs text-muted-foreground mt-0.5">
                          <span>{job.trials_completed}/{job.total_trials} 迭代</span>
                          <span>得分: {job.best_score > 0 ? job.best_score.toFixed(4) : '-'}</span>
                          <span>{new Date(job.created_at).toLocaleString()}</span>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <button
                        onClick={() => setSelectedJob(isSelected ? null : job.id)}
                        className="p-1.5 rounded-md hover:bg-white/5 text-muted-foreground transition-colors"
                      >
                        {isSelected ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
                      </button>
                      {job.status === 'running' && (
                        <button
                          onClick={() => cancelMutation.mutate(job.id)}
                          disabled={cancelMutation.isPending}
                          className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                        >
                          <Square className="w-4 h-4" />
                        </button>
                      )}
                      <button
                        onClick={() => {
                          if (confirm('确定删除此任务吗？')) deleteMutation.mutate(job.id)
                        }}
                        disabled={deleteMutation.isPending}
                        className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>

                  {/* Detail */}
                  {isSelected && jobDetail && (
                    <div className="mt-3 pt-3 border-t border-quant-border">
                      <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                        <div className="p-2 rounded-md bg-quant-bg-secondary">
                          <div className="text-[10px] text-muted-foreground">状态</div>
                          <div className="text-sm font-medium">{jobDetail.status}</div>
                        </div>
                        <div className="p-2 rounded-md bg-quant-bg-secondary">
                          <div className="text-[10px] text-muted-foreground">最佳得分</div>
                          <div className="text-sm font-medium">{jobDetail.best_score?.toFixed(4) ?? '-'}</div>
                        </div>
                        <div className="p-2 rounded-md bg-quant-bg-secondary">
                          <div className="text-[10px] text-muted-foreground">迭代进度</div>
                          <div className="text-sm font-medium">{jobDetail.trials_completed}/{jobDetail.total_trials}</div>
                        </div>
                        <div className="p-2 rounded-md bg-quant-bg-secondary">
                          <div className="text-[10px] text-muted-foreground">耗时</div>
                          <div className="text-sm font-medium">
                            {jobDetail.duration_ms ? `${(Number(jobDetail.duration_ms) / 1000).toFixed(0)}s` : '-'}
                          </div>
                        </div>
                      </div>
                      {jobDetail.best_params && (
                        <div className="mt-2">
                          <div className="text-xs text-muted-foreground mb-1">最佳参数</div>
                          <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
                            {Object.entries(jobDetail.best_params).map(([key, value]) => (
                              <div key={key} className="p-2 rounded-md bg-quant-bg-secondary">
                                <div className="text-[10px] text-muted-foreground">{key}</div>
                                <div className="text-sm font-medium">{String(value)}</div>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
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
