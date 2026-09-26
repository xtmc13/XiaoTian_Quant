import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  analysisApi,
  type AnalysisJob,
  type AnalysisVariantGroup,
  type LookaheadResult,
  type RecursiveResult,
} from '@/lib/api'
import { MODEL_INTERVALS } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { useConfirmDialog } from '@/components/ui/ConfirmDialog'
import {
  ShieldCheck,
  ShieldAlert,
  Play,
  Clock,
  CheckCircle2,
  AlertCircle,
  Layers,
  ScanSearch,
  ChevronDown,
  ChevronUp,
  Ban,
  Trash2,
} from 'lucide-react'

/* ── Constants ── */
const STRATEGIES = [
  { value: 'sma_cross', label: '均线交叉' },
  { value: 'breakout', label: '突破策略' },
  { value: 'martin_trend', label: '马丁趋势' },
  { value: 'wallstreet', label: '华尔街策略' },
  { value: 'macd_golden_long', label: 'MACD金叉开多' },
  { value: 'macd_death_short', label: 'MACD死叉开空' },
  { value: 'ema_follow_trend', label: 'EMA顺势策略' },
  { value: 'ema_counter_trend', label: 'EMA逆势策略' },
  { value: 'dual_burn', label: '双向燃烧' },
  { value: 'global_burn', label: '超级全局燃烧' },
  { value: 'trend_long', label: '顺势做多' },
  { value: 'trend_short', label: '顺势做空' },
  { value: 'counter_stable', label: '逆势稳健' },
  { value: 'head_tail_arb', label: '首尾套利' },
]

function fmtTime(ms?: number) {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN', { hour12: false })
}

function isLookahead(r?: LookaheadResult | RecursiveResult): r is LookaheadResult {
  return !!r && 'variants' in r
}

function conclusionBadge(result?: LookaheadResult | RecursiveResult) {
  if (!result) return null
  const c = result.conclusion
  if (c === 'biased' || c === 'recursive') {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-red-500/15 text-red-400">
        <ShieldAlert className="w-3.5 h-3.5" />
        {c === 'biased' ? '检出前视偏差' : '检出递归偏差'}
      </span>
    )
  }
  if (c === 'inconclusive') {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-yellow-500/15 text-yellow-400">
        <AlertCircle className="w-3.5 h-3.5" />
        无法判定
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-green-500/15 text-green-400">
      <ShieldCheck className="w-3.5 h-3.5" />
      {c === 'unbiased' ? '未发现偏差' : '信号稳定'}
    </span>
  )
}

function statusBadge(status: AnalysisJob['status']) {
  switch (status) {
    case 'running':
      return (
        <span className="inline-flex items-center gap-1 text-xs text-quant-gold">
          <Clock className="w-3.5 h-3.5 animate-spin" /> 运行中
        </span>
      )
    case 'completed':
      return (
        <span className="inline-flex items-center gap-1 text-xs text-green-400">
          <CheckCircle2 className="w-3.5 h-3.5" /> 已完成
        </span>
      )
    case 'cancelled':
      return (
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          <Ban className="w-3.5 h-3.5" /> 已取消
        </span>
      )
    default:
      return (
        <span className="inline-flex items-center gap-1 text-xs text-red-400">
          <AlertCircle className="w-3.5 h-3.5" /> 失败
        </span>
      )
  }
}

const GROUP_LABELS: Record<string, string> = {
  prefix: '前缀递增（同起点、长度递增）',
  start_offset: '起点偏移（固定终点、起点右移，捕捉 warmup 不收敛）',
}

/* Recursive 单个变体组（prefix / start_offset）的档位表 + 不稳定时点表 */
function RecursiveGroupView({ group }: { group: AnalysisVariantGroup }) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-xs">
        <span className="px-2 py-0.5 rounded bg-purple-500/15 text-purple-400 font-mono">{group.kind}</span>
        <span className="text-muted-foreground">{GROUP_LABELS[group.kind] ?? group.kind}</span>
        <span className="text-muted-foreground">
          对比时点 <span className="text-foreground">{group.compared_points}</span>，不稳定{' '}
          <span className={group.unstable_count > 0 ? 'text-red-400 font-medium' : 'text-foreground'}>
            {group.unstable_count}
          </span>
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-quant-border text-xs text-muted-foreground">
              <th className="text-left px-3 py-2 font-medium">档位</th>
              <th className="text-right px-3 py-2 font-medium">K线数</th>
              <th className="text-right px-3 py-2 font-medium">入场</th>
              <th className="text-right px-3 py-2 font-medium">离场</th>
            </tr>
          </thead>
          <tbody>
            {group.levels.map((l) => (
              <tr key={l.name} className="border-b border-quant-border/50">
                <td className="px-3 py-2 font-mono text-xs text-foreground">{l.name}</td>
                <td className="px-3 py-2 text-right text-foreground">{l.bars}</td>
                <td className="px-3 py-2 text-right text-foreground">{l.entries}</td>
                <td className="px-3 py-2 text-right text-foreground">{l.exits}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {group.unstable_points.length > 0 && (
        <div>
          <h3 className="text-sm font-medium text-red-400 mb-2">
            不稳定时点（同一历史时点信号在该维度下不一致）
          </h3>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-quant-border text-xs text-muted-foreground">
                  <th className="text-left px-3 py-2 font-medium">时间</th>
                  {group.levels.map((l) => (
                    <th key={l.name} className="text-left px-3 py-2 font-medium">
                      {l.bars}根
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {group.unstable_points.slice(0, 50).map((p) => {
                  const first = group.levels[0] ? p.by_lens[group.levels[0].name] : ''
                  return (
                    <tr key={p.index} className="border-b border-quant-border/50 bg-red-500/5">
                      <td className="px-3 py-2 text-xs text-foreground">{fmtTime(p.time)}</td>
                      {group.levels.map((l) => {
                        const dir = p.by_lens[l.name] || ''
                        return (
                          <td
                            key={l.name}
                            className={cn(
                              'px-3 py-2 text-xs',
                              dir !== first ? 'text-red-400 font-medium' : 'text-foreground'
                            )}
                          >
                            {dir || '—'}
                          </td>
                        )
                      })}
                    </tr>
                  )
                })}
              </tbody>
            </table>
            {group.unstable_points.length > 50 && (
              <p className="text-xs text-muted-foreground mt-2">
                仅展示前 50 个，共 {group.unstable_points.length} 个不稳定时点
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

/* ── Page ── */
export function AnalysisPage() {
  const queryClient = useQueryClient()
  const { confirm, Dialog } = useConfirmDialog()
  const [showForm, setShowForm] = useState(false)
  const [selectedJob, setSelectedJob] = useState<string | null>(null)
  const [form, setForm] = useState({
    kind: 'lookahead' as 'lookahead' | 'recursive',
    strategy_type: 'sma_cross',
    symbol: 'BTCUSDT',
    interval: '1h',
    from: '',
    to: '',
  })

  const { data: jobs } = useQuery({
    queryKey: ['analysis-jobs'],
    queryFn: () => analysisApi.jobs(),
    refetchInterval: 5000,
  })

  const { data: jobDetail } = useQuery({
    queryKey: ['analysis-job', selectedJob],
    queryFn: () => (selectedJob ? analysisApi.job(selectedJob) : null),
    enabled: !!selectedJob,
    refetchInterval: (q) => (q.state.data?.status === 'running' ? 3000 : false),
  })

  const startMutation = useMutation({
    mutationFn: () => {
      const payload: Record<string, unknown> = {
        strategy_type: form.strategy_type,
        symbol: form.symbol,
        interval: form.interval,
      }
      if (form.from) payload.from = form.from
      if (form.to) payload.to = form.to
      return form.kind === 'lookahead'
        ? analysisApi.startLookahead(payload)
        : analysisApi.startRecursive(payload)
    },
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['analysis-jobs'] })
      setShowForm(false)
      if (res?.job_id) setSelectedJob(res.job_id)
    },
  })

  const cancelMutation = useMutation({
    mutationFn: (id: string) => analysisApi.cancel(id),
    onSuccess: () => {
      toast('success', '任务已取消')
      queryClient.invalidateQueries({ queryKey: ['analysis-jobs'] })
      if (selectedJob) queryClient.invalidateQueries({ queryKey: ['analysis-job', selectedJob] })
    },
    onError: (e) => toast('error', '取消失败：' + ((e as Error)?.message || '未知错误')),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => analysisApi.remove(id),
    onSuccess: (_res, id) => {
      toast('success', '任务已删除')
      if (selectedJob === id) setSelectedJob(null)
      queryClient.invalidateQueries({ queryKey: ['analysis-jobs'] })
    },
    onError: (e) => toast('error', '删除失败：' + ((e as Error)?.message || '未知错误')),
  })

  const handleCancelJob = async (j: AnalysisJob) => {
    if (await confirm({ title: '取消检测任务', message: `确定取消任务 ${j.id} 吗？后台变体回测将中止。` })) {
      cancelMutation.mutate(j.id)
    }
  }

  const handleDeleteJob = async (j: AnalysisJob) => {
    if (
      await confirm({
        title: '删除检测任务',
        message: `确定删除任务 ${j.id} 吗？结果记录将一并删除，不可恢复。`,
        confirmText: '删除',
        variant: 'danger',
      })
    ) {
      deleteMutation.mutate(j.id)
    }
  }

  const runningCount = jobs?.filter((j) => j.status === 'running').length ?? 0
  const completedCount = jobs?.filter((j) => j.status === 'completed').length ?? 0
  const biasedCount =
    jobs?.filter((j) => {
      if (!j.result) return false
      try {
        const r = JSON.parse(j.result) as LookaheadResult | RecursiveResult
        return r.conclusion === 'biased' || r.conclusion === 'recursive'
      } catch {
        return false
      }
    }).length ?? 0

  const result = jobDetail?.result

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="偏差检测"
          subtitle="对标 freqtrade lookahead-analysis / recursive-analysis，质检回测可信度"
          actions={<ScanSearch className="w-6 h-6 text-quant-gold" />}
        />

        {/* KPI Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="运行中"
            value={runningCount}
            icon={<Clock className="w-4 h-4 text-quant-gold" />}
            subValue="任务"
            trend="neutral"
          />
          <KPICard
            label="已完成"
            value={completedCount}
            icon={<CheckCircle2 className="w-4 h-4 text-green-400" />}
            subValue="任务"
            trend="up"
          />
          <KPICard
            label="检出偏差"
            value={biasedCount}
            icon={<ShieldAlert className="w-4 h-4 text-red-400" />}
            subValue="任务"
            trend={biasedCount > 0 ? 'down' : 'neutral'}
          />
          <KPICard
            label="总任务"
            value={jobs?.length ?? 0}
            icon={<Layers className="w-4 h-4 text-quant-gold" />}
            subValue="全部"
            trend="neutral"
          />
        </div>

        {/* Actions */}
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-foreground">检测任务</h2>
          <button
            onClick={() => setShowForm(!showForm)}
            className={cn(
              'flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium transition-colors',
              showForm
                ? 'bg-quant-gold/10 text-quant-gold'
                : 'bg-quant-gold text-white hover:bg-quant-gold/90'
            )}
          >
            <Play className="w-3.5 h-3.5" />
            新建检测
            {showForm ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
          </button>
        </div>

        {/* New analysis form */}
        {showForm && (
          <SectionCard title="新建偏差检测">
            <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
              <div>
                <label className="block text-xs text-muted-foreground mb-1.5">分析类型</label>
                <select
                  value={form.kind}
                  onChange={(e) => setForm({ ...form, kind: e.target.value as 'lookahead' | 'recursive' })}
                  className="w-full bg-quant-bg-secondary border border-quant-border rounded-md px-3 py-2 text-sm text-foreground"
                >
                  <option value="lookahead">Lookahead 前视偏差（变体回测）</option>
                  <option value="recursive">Recursive 递归偏差（前缀稳定性）</option>
                </select>
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1.5">策略</label>
                <select
                  value={form.strategy_type}
                  onChange={(e) => setForm({ ...form, strategy_type: e.target.value })}
                  className="w-full bg-quant-bg-secondary border border-quant-border rounded-md px-3 py-2 text-sm text-foreground"
                >
                  {STRATEGIES.map((s) => (
                    <option key={s.value} value={s.value}>
                      {s.label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1.5">交易对</label>
                <input
                  value={form.symbol}
                  onChange={(e) => setForm({ ...form, symbol: e.target.value.toUpperCase() })}
                  className="w-full bg-quant-bg-secondary border border-quant-border rounded-md px-3 py-2 text-sm text-foreground"
                  placeholder="BTCUSDT"
                />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1.5">K线周期</label>
                <select
                  value={form.interval}
                  onChange={(e) => setForm({ ...form, interval: e.target.value })}
                  className="w-full bg-quant-bg-secondary border border-quant-border rounded-md px-3 py-2 text-sm text-foreground"
                >
                  {MODEL_INTERVALS.map((i) => (
                    <option key={i.value} value={i.value}>
                      {i.label}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1.5">开始日期（可选）</label>
                <input
                  type="date"
                  value={form.from}
                  onChange={(e) => setForm({ ...form, from: e.target.value })}
                  className="w-full bg-quant-bg-secondary border border-quant-border rounded-md px-3 py-2 text-sm text-foreground"
                />
              </div>
              <div>
                <label className="block text-xs text-muted-foreground mb-1.5">结束日期（可选）</label>
                <input
                  type="date"
                  value={form.to}
                  onChange={(e) => setForm({ ...form, to: e.target.value })}
                  className="w-full bg-quant-bg-secondary border border-quant-border rounded-md px-3 py-2 text-sm text-foreground"
                />
              </div>
            </div>
            <div className="mt-4 flex items-center gap-3">
              <button
                onClick={() => startMutation.mutate()}
                disabled={startMutation.isPending}
                className="flex items-center gap-1.5 px-4 py-2 rounded-md text-xs font-medium bg-quant-gold text-white hover:bg-quant-gold/90 disabled:opacity-50"
              >
                <Play className="w-3.5 h-3.5" />
                {startMutation.isPending ? '启动中…' : '开始检测'}
              </button>
              {startMutation.isError && (
                <span className="text-xs text-red-400">
                  启动失败：{(startMutation.error as Error)?.message || '请检查参数与本地数据'}
                </span>
              )}
              <span className="text-xs text-muted-foreground">
                需本地已导入该交易对至少 100 根K线（数据管理页可导入）
              </span>
            </div>
          </SectionCard>
        )}

        {/* Jobs table */}
        <SectionCard title="任务列表" noPadding>
          {!jobs || jobs.length === 0 ? (
            <EmptyState
              icon={<ScanSearch className="w-10 h-10" />}
              title="暂无检测任务"
              description="点击右上角「新建检测」发起一次回测偏差质检"
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-quant-border text-xs text-muted-foreground">
                    <th className="text-left px-4 py-3 font-medium">任务</th>
                    <th className="text-left px-4 py-3 font-medium">类型</th>
                    <th className="text-left px-4 py-3 font-medium">策略</th>
                    <th className="text-left px-4 py-3 font-medium">交易对</th>
                    <th className="text-left px-4 py-3 font-medium">状态</th>
                    <th className="text-left px-4 py-3 font-medium">创建时间</th>
                    <th className="text-right px-4 py-3 font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.map((j) => (
                    <tr
                      key={j.id}
                      onClick={() => setSelectedJob(j.id)}
                      className={cn(
                        'border-b border-quant-border/50 cursor-pointer transition-colors',
                        selectedJob === j.id ? 'bg-quant-gold/5' : 'hover:bg-white/5'
                      )}
                    >
                      <td className="px-4 py-3 font-mono text-xs text-foreground">{j.id}</td>
                      <td className="px-4 py-3">
                        <span
                          className={cn(
                            'px-2 py-0.5 rounded text-xs',
                            j.kind === 'lookahead'
                              ? 'bg-blue-500/15 text-blue-400'
                              : 'bg-purple-500/15 text-purple-400'
                          )}
                        >
                          {j.kind === 'lookahead' ? '前视' : '递归'}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-foreground">{j.strategy_type}</td>
                      <td className="px-4 py-3 text-foreground">
                        {j.symbol} <span className="text-muted-foreground">{j.interval}</span>
                      </td>
                      <td className="px-4 py-3">{statusBadge(j.status)}</td>
                      <td className="px-4 py-3 text-xs text-muted-foreground">{fmtTime(j.created_at)}</td>
                      <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="inline-flex items-center gap-1.5">
                          {j.status === 'running' && (
                            <button
                              onClick={() => handleCancelJob(j)}
                              disabled={cancelMutation.isPending}
                              className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs text-quant-gold hover:bg-quant-gold/10 disabled:opacity-50"
                              title="取消任务（中止后台变体回测）"
                            >
                              <Ban className="w-3.5 h-3.5" /> 取消
                            </button>
                          )}
                          {j.status !== 'running' && (
                            <button
                              onClick={() => handleDeleteJob(j)}
                              disabled={deleteMutation.isPending}
                              className="inline-flex items-center gap-1 px-2 py-1 rounded text-xs text-red-400 hover:bg-red-500/10 disabled:opacity-50"
                              title="删除任务记录（仅终态可删）"
                            >
                              <Trash2 className="w-3.5 h-3.5" /> 删除
                            </button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </SectionCard>

        {/* Job detail */}
        {selectedJob && jobDetail && (
          <SectionCard
            title={`检测结果 ${jobDetail.id}`}
            headerAction={conclusionBadge(result)}
          >
            {jobDetail.status === 'running' && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Clock className="w-4 h-4 animate-spin text-quant-gold" />
                正在运行变体回测，请稍候…
              </div>
            )}
            {jobDetail.status === 'failed' && (
              <div className="text-sm text-red-400">任务失败：{jobDetail.error || '未知错误'}</div>
            )}
            {result && (
              <div className="space-y-5">
                <p className="text-sm text-foreground">{result.summary}</p>
                <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
                  <span>
                    策略 <span className="text-foreground">{jobDetail.strategy_type}</span>
                  </span>
                  <span>
                    交易对{' '}
                    <span className="text-foreground">
                      {jobDetail.symbol} {jobDetail.interval}
                    </span>
                  </span>
                  <span>
                    置信度 <span className="text-foreground">{result.confidence}</span>
                  </span>
                  {isLookahead(result) && (
                    <>
                      <span>
                        基线入场 <span className="text-foreground">{result.total_entries}</span>
                      </span>
                      <span>
                        变体回测 <span className="text-foreground">{result.variant_count}</span>
                      </span>
                      <span>
                        异常入场{' '}
                        <span className={result.false_entry_count > 0 ? 'text-red-400' : 'text-foreground'}>
                          {result.false_entry_count}
                        </span>
                      </span>
                    </>
                  )}
                  {!isLookahead(result) && (
                    <>
                      <span>
                        对比时点 <span className="text-foreground">{result.compared_points}</span>
                      </span>
                      <span>
                        不稳定时点{' '}
                        <span className={result.unstable_count > 0 ? 'text-red-400' : 'text-foreground'}>
                          {result.unstable_count}
                        </span>
                      </span>
                    </>
                  )}
                </div>

                {/* Lookahead: 变体明细 */}
                {isLookahead(result) && (
                  <div className="space-y-4">
                    <div className="overflow-x-auto">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="border-b border-quant-border text-xs text-muted-foreground">
                            <th className="text-left px-3 py-2 font-medium">变体</th>
                            <th className="text-left px-3 py-2 font-medium">类型</th>
                            <th className="text-right px-3 py-2 font-medium">K线数</th>
                            <th className="text-right px-3 py-2 font-medium">入场数</th>
                            <th className="text-right px-3 py-2 font-medium">消失</th>
                            <th className="text-right px-3 py-2 font-medium">异常位置</th>
                          </tr>
                        </thead>
                        <tbody>
                          {result.variants.map((v) => (
                            <tr
                              key={v.name}
                              className={cn(
                                'border-b border-quant-border/50',
                                v.missing_count + v.displaced_count > 0 && 'bg-red-500/5'
                              )}
                            >
                              <td className="px-3 py-2 font-mono text-xs text-foreground">{v.name}</td>
                              <td className="px-3 py-2 text-xs text-muted-foreground">{v.kind}</td>
                              <td className="px-3 py-2 text-right text-foreground">{v.variant_bars}</td>
                              <td className="px-3 py-2 text-right text-foreground">{v.total_entries}</td>
                              <td
                                className={cn(
                                  'px-3 py-2 text-right',
                                  v.missing_count > 0 ? 'text-red-400 font-medium' : 'text-foreground'
                                )}
                              >
                                {v.missing_count}
                              </td>
                              <td
                                className={cn(
                                  'px-3 py-2 text-right',
                                  v.displaced_count > 0 ? 'text-red-400 font-medium' : 'text-foreground'
                                )}
                              >
                                {v.displaced_count}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>

                    {result.variants.some((v) => v.false_entries.length > 0) && (
                      <div>
                        <h3 className="text-sm font-medium text-red-400 mb-2">偏差嫌疑信号明细</h3>
                        <div className="overflow-x-auto">
                          <table className="w-full text-sm">
                            <thead>
                              <tr className="border-b border-quant-border text-xs text-muted-foreground">
                                <th className="text-left px-3 py-2 font-medium">时间</th>
                                <th className="text-left px-3 py-2 font-medium">方向</th>
                                <th className="text-left px-3 py-2 font-medium">变体</th>
                                <th className="text-left px-3 py-2 font-medium">问题</th>
                                <th className="text-left px-3 py-2 font-medium">说明</th>
                              </tr>
                            </thead>
                            <tbody>
                              {result.variants.flatMap((v) =>
                                v.false_entries.map((f, i) => (
                                  <tr
                                    key={`${v.name}-${i}`}
                                    className="border-b border-quant-border/50 bg-red-500/5"
                                  >
                                    <td className="px-3 py-2 text-xs text-foreground">
                                      {fmtTime(f.signal.time)}
                                    </td>
                                    <td className="px-3 py-2 text-xs text-foreground">{f.signal.direction}</td>
                                    <td className="px-3 py-2 font-mono text-xs text-muted-foreground">
                                      {f.variant}
                                    </td>
                                    <td className="px-3 py-2 text-xs text-red-400">
                                      {f.kind === 'missing' ? '信号消失' : '异常位置'}
                                    </td>
                                    <td className="px-3 py-2 text-xs text-muted-foreground">{f.detail}</td>
                                  </tr>
                                ))
                              )}
                            </tbody>
                          </table>
                        </div>
                      </div>
                    )}
                  </div>
                )}

                {/* Recursive: 按变体组渲染（prefix 前缀递增 + start_offset 起点偏移） */}
                {!isLookahead(result) && (
                  <div className="space-y-6">
                    {(result.groups && result.groups.length > 0
                      ? result.groups
                      : [
                          {
                            kind: 'prefix' as const,
                            levels: result.levels,
                            compared_points: result.compared_points,
                            unstable_points: result.unstable_points,
                            unstable_count: result.unstable_count,
                          },
                        ]
                    ).map((g) => (
                      <RecursiveGroupView key={g.kind} group={g} />
                    ))}
                  </div>
                )}
              </div>
            )}
          </SectionCard>
        )}
      </div>
      <Dialog />
    </div>
  )
}

export default AnalysisPage
