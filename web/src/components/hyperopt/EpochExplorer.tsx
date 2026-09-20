import { useMemo, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { hyperoptApi } from '@/lib/api'
import type { HyperoptEpoch, HyperoptEpochApplyResult } from '@/types/strategies'
import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { EmptyState } from '@/components/ui/EmptyState'
import {
  CheckCircle2,
  ArrowUpDown,
  ArrowUp,
  ArrowDown,
  UploadCloud,
  X,
  History,
} from 'lucide-react'

/* ── Types ── */

type EpochSortKey = 'loss' | 'total_return_pct'

interface EpochFilters {
  loss_max: string
  sharpe_min: string
  trade_count_min: string
  max_drawdown_max: string
  limit: string
}

const DEFAULT_FILTERS: EpochFilters = {
  loss_max: '',
  sharpe_min: '',
  trade_count_min: '',
  max_drawdown_max: '',
  limit: '200',
}

/* ── Component ── */

/**
 * EpochExplorer：hyperopt 每轮 trial 的持久化结果浏览器。
 * 任务选择 + 组合过滤 + 可排序表格 + 选中轮次“回写到策略”（确认弹窗）。
 * 需要 hyperoptApi.epochs / epoch / applyEpoch（见 api.ts 片段）。
 */
export function EpochExplorer() {
  const queryClient = useQueryClient()
  const [jobId, setJobId] = useState<string>('')
  const [draftFilters, setDraftFilters] = useState<EpochFilters>(DEFAULT_FILTERS)
  const [filters, setFilters] = useState<EpochFilters>(DEFAULT_FILTERS)
  const [sortKey, setSortKey] = useState<EpochSortKey>('loss')
  const [sortAsc, setSortAsc] = useState(true)
  const [confirmEpoch, setConfirmEpoch] = useState<HyperoptEpoch | null>(null)
  const [applyResult, setApplyResult] = useState<HyperoptEpochApplyResult | null>(null)

  // 任务列表（复用现有 jobs 接口；含他人的会被后端过滤）
  const { data: jobs, isLoading: jobsLoading } = useQuery({
    queryKey: ['hyperopt-jobs'],
    queryFn: () => hyperoptApi.jobs(),
    refetchInterval: 15000,
  })

  const epochQueryParams = useMemo(() => {
    const params: Record<string, string> = {}
    if (jobId) params.job_id = jobId
    for (const key of ['loss_max', 'sharpe_min', 'trade_count_min', 'max_drawdown_max', 'limit'] as const) {
      const v = filters[key].trim()
      if (v !== '') params[key] = v
    }
    return params
  }, [jobId, filters])

  const { data: epochs, isLoading: epochsLoading } = useQuery({
    queryKey: ['hyperopt-epochs', epochQueryParams],
    queryFn: () => hyperoptApi.epochs(epochQueryParams),
    refetchInterval: jobId ? 10000 : false,
  })

  const sortedEpochs = useMemo(() => {
    const list = [...(epochs ?? [])]
    const pick = (e: HyperoptEpoch) =>
      sortKey === 'loss' ? e.loss : (e.metrics?.[sortKey] ?? Number.POSITIVE_INFINITY)
    list.sort((a, b) => {
      const av = pick(a)
      const bv = pick(b)
      return sortAsc ? av - bv : bv - av
    })
    return list
  }, [epochs, sortKey, sortAsc])

  const applyMutation = useMutation({
    mutationFn: (id: string) => hyperoptApi.applyEpoch(id),
    onSuccess: (result) => {
      setApplyResult(result)
      setConfirmEpoch(null)
      queryClient.invalidateQueries({ queryKey: ['hyperopt-epochs'] })
    },
  })

  const toggleSort = (key: EpochSortKey) => {
    if (sortKey === key) {
      setSortAsc(!sortAsc)
    } else {
      setSortKey(key)
      setSortAsc(true)
    }
  }

  const sortIcon = (key: EpochSortKey) =>
    sortKey !== key ? (
      <ArrowUpDown className="w-3 h-3 text-muted-foreground" />
    ) : sortAsc ? (
      <ArrowUp className="w-3 h-3 text-quant-gold" />
    ) : (
      <ArrowDown className="w-3 h-3 text-quant-gold" />
    )

  const inputCls =
    'w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold'

  return (
    <SectionCard
      title={
        <span className="flex items-center gap-2">
          <History className="w-4 h-4 text-quant-gold" />
          Epoch 浏览器
        </span>
      }
    >
      <div className="space-y-4 p-4">
        {/* 任务选择 + 过滤器 */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <div className="space-y-1 md:col-span-1">
            <label className="text-xs text-muted-foreground">优化任务</label>
            <select
              value={jobId}
              onChange={(e) => setJobId(e.target.value)}
              className={inputCls}
              disabled={jobsLoading}
            >
              <option value="">全部任务</option>
              {(jobs ?? []).map((j) => (
                <option key={j.id} value={j.id}>
                  {j.id} · {j.strategy_type} · {j.symbol}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">loss ≤</label>
            <input
              type="number"
              value={draftFilters.loss_max}
              onChange={(e) => setDraftFilters({ ...draftFilters, loss_max: e.target.value })}
              className={inputCls}
              placeholder="-2.0"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">Sharpe ≥</label>
            <input
              type="number"
              value={draftFilters.sharpe_min}
              onChange={(e) => setDraftFilters({ ...draftFilters, sharpe_min: e.target.value })}
              className={inputCls}
              placeholder="1.5"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">交易次数 ≥</label>
            <input
              type="number"
              value={draftFilters.trade_count_min}
              onChange={(e) => setDraftFilters({ ...draftFilters, trade_count_min: e.target.value })}
              className={inputCls}
              placeholder="20"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">最大回撤% ≤</label>
            <input
              type="number"
              value={draftFilters.max_drawdown_max}
              onChange={(e) => setDraftFilters({ ...draftFilters, max_drawdown_max: e.target.value })}
              className={inputCls}
              placeholder="10"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">limit</label>
            <input
              type="number"
              value={draftFilters.limit}
              onChange={(e) => setDraftFilters({ ...draftFilters, limit: e.target.value })}
              className={inputCls}
              placeholder="200"
            />
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setFilters({ ...draftFilters })}
            className="flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium bg-quant-gold text-white hover:bg-quant-gold/90"
          >
            应用过滤
          </button>
          <button
            onClick={() => {
              setDraftFilters(DEFAULT_FILTERS)
              setFilters(DEFAULT_FILTERS)
            }}
            className="px-3 py-2 rounded-md text-xs font-medium bg-quant-bg-secondary text-muted-foreground hover:text-foreground"
          >
            重置
          </button>
        </div>

        {/* Epoch 表格 */}
        {epochsLoading ? (
          <div className="text-sm text-muted-foreground py-8 text-center">加载中…</div>
        ) : sortedEpochs.length === 0 ? (
          <EmptyState
            title="暂无 epoch 记录"
            description="先运行一个优化任务（带 strategy_id 的任务支持一键回写）"
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-xs text-muted-foreground border-b border-quant-border">
                  <th className="py-2 pr-3">轮次</th>
                  <th className="py-2 pr-3">
                    <button
                      className="flex items-center gap-1 hover:text-foreground"
                      onClick={() => toggleSort('loss')}
                    >
                      Loss {sortIcon('loss')}
                    </button>
                  </th>
                  <th className="py-2 pr-3">
                    <button
                      className="flex items-center gap-1 hover:text-foreground"
                      onClick={() => toggleSort('total_return_pct')}
                    >
                      收益% {sortIcon('total_return_pct')}
                    </button>
                  </th>
                  <th className="py-2 pr-3">Sharpe</th>
                  <th className="py-2 pr-3">回撤%</th>
                  <th className="py-2 pr-3">交易数</th>
                  <th className="py-2 pr-3">状态</th>
                  <th className="py-2 text-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {sortedEpochs.map((ep) => (
                  <tr
                    key={ep.id}
                    className="border-b border-quant-border/50 hover:bg-quant-bg-secondary/50"
                  >
                    <td className="py-2 pr-3 text-muted-foreground">#{ep.trial_id}</td>
                    <td className="py-2 pr-3 font-mono">{ep.loss.toFixed(4)}</td>
                    <td className="py-2 pr-3 font-mono">
                      {(ep.metrics?.total_return_pct ?? 0).toFixed(2)}
                    </td>
                    <td className="py-2 pr-3 font-mono">
                      {(ep.metrics?.sharpe_ratio ?? 0).toFixed(2)}
                    </td>
                    <td className="py-2 pr-3 font-mono">
                      {(ep.metrics?.max_drawdown_pct ?? 0).toFixed(2)}
                    </td>
                    <td className="py-2 pr-3 font-mono">
                      {ep.metrics?.total_trades ?? 0}
                    </td>
                    <td className="py-2 pr-3">
                      {ep.applied ? (
                        <span className="inline-flex items-center gap-1 text-xs text-green-400">
                          <CheckCircle2 className="w-3.5 h-3.5" />
                          已回写
                        </span>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="py-2 text-right">
                      {ep.applied ? null : (
                        <button
                          onClick={() => setConfirmEpoch(ep)}
                          disabled={!ep.strategy_id}
                          title={ep.strategy_id ? '把该轮超参回写到策略配置' : '任务未绑定 strategy_id，无法回写'}
                          className={cn(
                            'inline-flex items-center gap-1 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors',
                            ep.strategy_id
                              ? 'bg-quant-gold/10 text-quant-gold hover:bg-quant-gold hover:text-white'
                              : 'bg-quant-bg-secondary text-muted-foreground cursor-not-allowed'
                          )}
                        >
                          <UploadCloud className="w-3.5 h-3.5" />
                          回写到策略
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* 回写结果（diff 留痕） */}
        {applyResult && (
          <div className="rounded-md border border-green-500/30 bg-green-500/5 p-3 space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-xs text-green-400 flex items-center gap-1.5">
                <CheckCircle2 className="w-3.5 h-3.5" />
                已回写到策略 {applyResult.strategy_id}
              </span>
              <button onClick={() => setApplyResult(null)} className="text-muted-foreground hover:text-foreground">
                <X className="w-3.5 h-3.5" />
              </button>
            </div>
            <div className="text-xs font-mono text-muted-foreground space-y-0.5">
              {Object.entries(applyResult.diff ?? {}).map(([k, d]) => {
                const change = d as { old: unknown; new: unknown }
                return (
                  <div key={k}>
                    {k}: {change.old === null ? '(新增)' : JSON.stringify(change.old)} → {JSON.stringify(change.new)}
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </div>

      {/* 确认弹窗 */}
      {confirmEpoch && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
          <div className="w-full max-w-md rounded-xl border border-quant-border bg-quant-bg p-5 space-y-4">
            <h3 className="text-sm font-semibold text-foreground">
              确认回写第 #{confirmEpoch.trial_id} 轮超参？
            </h3>
            <p className="text-xs text-muted-foreground">
              将把以下参数写入策略配置（{confirmEpoch.strategy_id}），原值会在响应 diff 中留痕：
            </p>
            <div className="rounded-md bg-quant-bg-secondary p-3 text-xs font-mono text-muted-foreground max-h-48 overflow-y-auto space-y-0.5">
              {Object.entries(confirmEpoch.params ?? {}).map(([k, v]) => (
                <div key={k}>
                  {k} = {JSON.stringify(v)}
                </div>
              ))}
            </div>
            {applyMutation.isError && (
              <p className="text-xs text-red-400">
                回写失败：{(applyMutation.error as Error)?.message ?? '未知错误'}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setConfirmEpoch(null)}
                className="px-3 py-2 rounded-md text-xs font-medium bg-quant-bg-secondary text-muted-foreground hover:text-foreground"
              >
                取消
              </button>
              <button
                onClick={() => applyMutation.mutate(confirmEpoch.id)}
                disabled={applyMutation.isPending}
                className="px-3 py-2 rounded-md text-xs font-medium bg-quant-gold text-white hover:bg-quant-gold/90 disabled:opacity-50"
              >
                {applyMutation.isPending ? '回写中…' : '确认回写'}
              </button>
            </div>
          </div>
        </div>
      )}
    </SectionCard>
  )
}

export default EpochExplorer
