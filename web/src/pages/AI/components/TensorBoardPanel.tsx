import { useState, useEffect, useCallback, useMemo } from 'react'
import { BarChart3, RefreshCw, Trash2, TrendingUp, Activity, X, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { tensorboardApi } from '@/lib/api'
import type { TensorBoardRun, TensorBoardScalar } from '@/types'

function SimpleLineChart({ data, color = '#d4a574', height = 120 }: { data: TensorBoardScalar[]; color?: string; height?: number }) {
  if (!data || data.length === 0) return <div className="text-[10px] text-muted-foreground">无数据</div>
  
  const values = data.map(d => d.value)
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const width = 300
  const padding = 4
  
  const points = data.map((d, i) => {
    const x = padding + (i / (data.length - 1 || 1)) * (width - padding * 2)
    const y = height - padding - ((d.value - min) / range) * (height - padding * 2)
    return `${x},${y}`
  }).join(' ')
  
  return (
    <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <polyline fill="none" stroke={color} strokeWidth="1.5" points={points} />
      {data.map((d, i) => {
        const x = padding + (i / (data.length - 1 || 1)) * (width - padding * 2)
        const y = height - padding - ((d.value - min) / range) * (height - padding * 2)
        return <circle key={i} cx={x} cy={y} r="2" fill={color} />
      })}
    </svg>
  )
}

export function TensorBoardPanel() {
  const [runs, setRuns] = useState<TensorBoardRun[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selectedRun, setSelectedRun] = useState<string | null>(null)
  const [scalars, setScalars] = useState<Record<string, TensorBoardScalar[]>>({})
  const [loadingScalars, setLoadingScalars] = useState(false)

  const loadRuns = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const data = await tensorboardApi.listRuns()
      setRuns(data?.runs || [])
    } catch (e: unknown) { const err = e instanceof Error ? e : new Error(String(e)); setError(err.message || '加载失败') }
    finally { setLoading(false) }
  }, [])

  const loadScalars = useCallback(async (runId: string) => {
    setLoadingScalars(true)
    try {
      const data = await tensorboardApi.queryScalars({ run_id: runId })
      setScalars(data?.scalars || {})
    } catch { /* ignore */ }
    finally { setLoadingScalars(false) }
  }, [])

  useEffect(() => { loadRuns() }, [loadRuns])
  useEffect(() => {
    if (selectedRun) loadScalars(selectedRun)
  }, [selectedRun, loadScalars])

  const selectedRunData = useMemo(() => runs.find(r => r.run_id === selectedRun), [runs, selectedRun])

  const tagColors: Record<string, string> = {
    'train/episode_reward': '#d4a574',
    'train/final_balance': '#22c55e',
    'train/epsilon': '#8b5cf6',
    'train/q_table_size': '#3b82f6',
    'train/mean_reward': '#d4a574',
    'train/loss': '#ef4444',
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BarChart3 className="h-4 w-4 text-quant-gold" />
          <span className="text-sm font-medium">TensorBoard 指标</span>
        </div>
        <button onClick={loadRuns} disabled={loading}
          className="p-1.5 rounded text-muted-foreground hover:text-foreground transition-colors">
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
        </button>
      </div>

      {error && (
        <div role="alert" className="flex items-start gap-2 text-xs text-red-400 p-3 rounded-lg bg-red-500/10 border border-red-500/20">
          <X className="h-3.5 w-3.5 mt-0.5 shrink-0" /> {error}
        </div>
      )}

      {/* Run list */}
      <div className="space-y-2">
        <div className="text-[10px] text-muted-foreground uppercase tracking-wider">实验运行</div>
        {runs.length === 0 && !loading && (
          <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
            暂无 TensorBoard 运行记录。训练 RL 或 ML 模型时启用 TensorBoard 即可生成。
          </div>
        )}
        <div className="grid grid-cols-1 gap-1.5">
          {runs.map(run => (
            <button key={run.run_id} onClick={() => setSelectedRun(run.run_id)}
              className={cn(
                'flex items-center justify-between p-2.5 rounded-lg border text-left transition-all',
                selectedRun === run.run_id
                  ? 'border-quant-gold bg-quant-gold/5'
                  : 'border-quant-border bg-quant-bg-secondary hover:border-quant-gold/30'
              )}>
              <div className="min-w-0">
                <div className="text-xs font-medium truncate">{run.run_name || run.run_id}</div>
                <div className="text-[10px] text-muted-foreground flex items-center gap-1.5">
                  <span className={cn('w-1.5 h-1.5 rounded-full',
                    run.status === 'running' ? 'bg-quant-green animate-pulse' :
                    run.status === 'completed' ? 'bg-quant-gold' : 'bg-red-400')} />
                  {run.status} · {run.model_type} · {run.tags?.length || 0} 指标
                </div>
              </div>
              <button onClick={async (e) => {
                e.stopPropagation()
                await tensorboardApi.deleteRun(run.run_id)
                loadRuns()
                if (selectedRun === run.run_id) setSelectedRun(null)
              }} className="p-1 rounded text-muted-foreground hover:text-red-400 shrink-0">
                <Trash2 className="h-3 w-3" />
              </button>
            </button>
          ))}
        </div>
      </div>

      {/* Scalar charts */}
      {selectedRun && selectedRunData && (
        <div className="space-y-3 border-t border-quant-border pt-3">
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-quant-gold" />
            <span className="text-sm font-medium">{selectedRunData.run_name}</span>
            <span className={cn('text-[10px] px-1.5 py-0.5 rounded',
              selectedRunData.status === 'running' ? 'bg-quant-green/10 text-quant-green' :
              'bg-quant-gold/10 text-quant-gold')}>
              {selectedRunData.status}
            </span>
          </div>

          {loadingScalars ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-5 w-5 animate-spin text-quant-gold" />
            </div>
          ) : Object.keys(scalars).length === 0 ? (
            <div className="text-xs text-muted-foreground p-3 bg-quant-bg-secondary rounded-lg">
              该运行暂无标量指标数据
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3">
              {Object.entries(scalars).map(([tag, points]) => (
                <div key={tag} className="p-3 rounded-lg border border-quant-border bg-quant-bg-secondary">
                  <div className="flex items-center justify-between mb-2">
                    <div className="text-xs font-medium flex items-center gap-1.5">
                      <TrendingUp className="h-3 w-3 text-quant-gold" />
                      {tag}
                    </div>
                    <div className="text-[10px] text-muted-foreground">
                      {points.length} 点 · 最新: {points[points.length - 1]?.value?.toFixed(4)}
                    </div>
                  </div>
                  <SimpleLineChart data={points} color={tagColors[tag] || '#d4a574'} />
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
