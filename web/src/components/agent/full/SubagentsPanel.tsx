import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Bot, X } from 'lucide-react'
import { agentSubagentsApi, type AgentSubagentRun } from '@/lib/api'
import { cn } from '@/lib/utils'

export interface SubagentsPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

const STATUS_META: Record<AgentSubagentRun['status'], { label: string; cls: string }> = {
  running: { label: '运行中', cls: 'border-[var(--ag-accent)]/40 bg-[var(--ag-accent)]/10 text-[var(--ag-accent)]' },
  done: { label: '完成', cls: 'border-[var(--ag-green)]/40 bg-[var(--ag-green)]/10 text-[var(--ag-green)]' },
  error: { label: '失败', cls: 'border-[var(--ag-red)]/40 bg-[var(--ag-red)]/10 text-[var(--ag-red)]' },
}

function fmtDuration(run: AgentSubagentRun): string {
  const ms = run.status === 'running' ? Date.now() - run.started_at * 1000 : run.finished_ms
  if (!Number.isFinite(ms) || ms < 0) return ''
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`
}

// ── 子代理面板：派发出去的子任务运行状态（subagents 插件；未就绪时降级为空态） ──
export function SubagentsPanel({ onClose, bare }: SubagentsPanelProps) {
  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading, isError } = useQuery({
    queryKey: ['agent-subagents'],
    queryFn: () => agentSubagentsApi.list(),
    retry: false,
    staleTime: 5_000,
    // 有运行中的子代理时 5s 轮询
    refetchInterval: (query) => ((query.state.data?.runs || []).some((r) => r.status === 'running') ? 5_000 : false),
  })
  const runs = data?.runs ?? []

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="子代理"
    >
      <div
        className={
          bare
            ? 'mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
            : 'flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
        }
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <Bot size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">子代理</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭子代理面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-3" aria-label="子代理运行列表">
          {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {isError && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              子代理服务暂不可用——后端 subagents 插件就绪后会显示在这里
            </p>
          )}
          {!isLoading && !isError && runs.length === 0 && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              暂无子代理运行——聊天中让助手「派一个子任务…」后会出现在这里
            </p>
          )}
          {runs.map((r) => {
            const meta = STATUS_META[r.status] || STATUS_META.done
            const duration = fmtDuration(r)
            return (
              <div
                key={r.id}
                className="mb-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2.5 last:mb-0"
              >
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-[var(--ag-text1)]">
                    {r.task}
                  </span>
                  <span
                    className={cn(
                      'shrink-0 rounded-full border px-1.5 py-px text-[10px] font-medium',
                      meta.cls,
                      r.status === 'running' && 'xt-shimmer-text'
                    )}
                  >
                    {meta.label}
                  </span>
                  {duration && (
                    <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]">{duration}</span>
                  )}
                </div>
                {r.result_summary && (
                  <p className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-[var(--ag-text3)]">
                    {r.result_summary}
                  </p>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

export default SubagentsPanel
