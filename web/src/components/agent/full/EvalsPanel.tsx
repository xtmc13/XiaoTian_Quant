import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, FlaskConical, Loader2, Play, X } from 'lucide-react'
import { agentEvalsApi, ApiError, type AgentEvalRun } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'

export interface EvalsPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

function fmtRunDuration(run: AgentEvalRun): string {
  const ms = run.status === 'running' ? Date.now() - run.started_at * 1000 : run.finished_ms
  if (!Number.isFinite(ms) || ms < 0) return ''
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`
}

function ToolChips({ label, tools, tone }: { label: string; tools: string[]; tone: 'expected' | 'used' }) {
  if (!tools || tools.length === 0) return null
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-1">
      <span className="shrink-0 text-[10px] text-[var(--ag-text4)]">{label}</span>
      {tools.map((t) => (
        <span
          key={t}
          className={cn(
            'rounded-full border px-1.5 py-px font-mono text-[10px]',
            tone === 'expected'
              ? 'border-[var(--ag-stroke3)] text-[var(--ag-text3)]'
              : 'border-[var(--ag-accent)]/40 bg-[var(--ag-accent)]/8 text-[var(--ag-accent)]'
          )}
        >
          {t}
        </span>
      ))}
    </span>
  )
}

// ── 评测面板：套件列表 + 运行 + 最近一次运行的逐条用例结果（evals 插件） ──
export function EvalsPanel({ onClose, bare }: EvalsPanelProps) {
  const queryClient = useQueryClient()
  // 手动触发的运行 id：优先展示它的结果卡（否则展示列表里最近的一次运行）
  const [activeRunId, setActiveRunId] = useState<string | null>(null)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading, isError } = useQuery({
    queryKey: ['agent-evals'],
    queryFn: () => agentEvalsApi.list(),
    retry: false,
    staleTime: 5_000,
    // 有运行中的评测时 2s 轮询
    refetchInterval: (query) => ((query.state.data?.runs || []).some((r) => r.status === 'running') ? 2_000 : false),
  })
  const suites = data?.suites ?? []
  const runs = data?.runs ?? []

  const runMut = useMutation({
    mutationFn: (suite: string) => agentEvalsApi.run(suite),
    onSuccess: (res) => {
      setActiveRunId(res.run_id)
      queryClient.invalidateQueries({ queryKey: ['agent-evals'] })
    },
    onError: (e: Error) => {
      if (e instanceof ApiError && e.status === 409) toast('warning', '已有评测在运行')
      else toast('error', e.message || '启动评测失败')
    },
  })

  // 结果卡对应的运行：手动触发的优先，其次列表里最近的一次
  const shownRunId = activeRunId ?? [...runs].sort((a, b) => b.started_at - a.started_at)[0]?.id ?? null

  const { data: runDetail } = useQuery({
    queryKey: ['agent-eval-run', shownRunId],
    queryFn: () => agentEvalsApi.getRun(shownRunId as string),
    enabled: !!shownRunId,
    retry: false,
    staleTime: 2_000,
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2_000 : false),
  })

  const duration = runDetail ? fmtRunDuration(runDetail) : ''
  const allPass = runDetail ? runDetail.pass === runDetail.total && runDetail.total > 0 : false

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="评测"
    >
      <div
        className={
          bare
            ? 'mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
            : 'flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
        }
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <FlaskConical size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">评测</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭评测面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {isError && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              评测服务暂不可用——后端 evals 插件就绪后会显示在这里
            </p>
          )}

          {/* 套件列表 */}
          {!isLoading && !isError && suites.length === 0 && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">暂无评测套件</p>
          )}
          {suites.length > 0 && (
            <div aria-label="评测套件" className="mb-3">
              <div className="px-1 pb-1 text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">
                评测套件
              </div>
              {suites.map((s) => (
                <div
                  key={s.id}
                  className="mb-1.5 flex items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2.5 last:mb-0"
                >
                  <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-[var(--ag-text1)]">
                    {s.name}
                  </span>
                  <span className="shrink-0 text-[11px] tabular-nums text-[var(--ag-text4)]">{s.count} 用例</span>
                  <button
                    type="button"
                    onClick={() => runMut.mutate(s.id)}
                    disabled={runMut.isPending}
                    aria-label={`运行 ${s.name}`}
                    className="flex shrink-0 items-center gap-1 rounded-lg bg-[var(--ag-accent)] px-2.5 py-1 text-[11px] font-medium text-[var(--ag-accent-fg)] transition-opacity hover:opacity-85 disabled:opacity-50"
                  >
                    <Play size={11} />
                    运行
                  </button>
                </div>
              ))}
            </div>
          )}

          {/* 最近一次运行结果卡 */}
          {runDetail && (
            <div
              aria-label="评测结果"
              className="rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-3"
            >
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--ag-text1)]">
                  {suites.find((s) => s.id === runDetail.suite)?.name || runDetail.suite}
                </span>
                {runDetail.status === 'running' ? (
                  <span className="flex shrink-0 items-center gap-1 text-[11px] text-[var(--ag-accent)]">
                    <Loader2 size={11} className="animate-spin" />
                    运行中…
                  </span>
                ) : (
                  <span
                    aria-label="评测通过率"
                    className={cn(
                      'shrink-0 text-[20px] font-bold tabular-nums leading-none',
                      allPass ? 'text-[var(--ag-green)]' : 'text-[var(--ag-red)]'
                    )}
                  >
                    {runDetail.pass}/{runDetail.total}
                  </span>
                )}
                {duration && (
                  <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]">{duration}</span>
                )}
              </div>

              {runDetail.status === 'done' && (runDetail.cases || []).length > 0 && (
                <div className="mt-2 space-y-1.5" aria-label="评测用例">
                  {(runDetail.cases || []).map((c, i) => (
                    <div
                      key={`${c.name}-${i}`}
                      className="rounded-lg border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/40 px-2.5 py-2"
                    >
                      <div className="flex items-center gap-1.5">
                        {c.ok ? (
                          <Check size={12} aria-label="通过" className="shrink-0 text-[var(--ag-green)]" />
                        ) : (
                          <X size={12} aria-label="未通过" className="shrink-0 text-[var(--ag-red)]" />
                        )}
                        <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--ag-text1)]">
                          {c.name}
                        </span>
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
                        <ToolChips label="期望工具" tools={c.expected_tools} tone="expected" />
                        <ToolChips label="实际工具" tools={c.used_tools} tone="used" />
                      </div>
                      {c.missing_keywords.length > 0 && (
                        <p className="mt-1 text-[11px] leading-relaxed text-[var(--ag-red)]">
                          缺失关键词：{c.missing_keywords.join('、')}
                        </p>
                      )}
                      {c.answer_summary && (
                        <p className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-[var(--ag-text3)]">
                          {c.answer_summary}
                        </p>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export default EvalsPanel
