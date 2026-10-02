import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BarChart3, Brain, Gauge, Wrench, X, Zap } from 'lucide-react'
import { agentInsightsApi } from '@/lib/api'

export interface InsightsPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

function fmtK(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n)
}

function fmtMs(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

function Section({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) {
  return (
    <div className="border-b border-[var(--ag-stroke3)] px-4 py-3 last:border-0">
      <div className="flex items-center gap-1.5 pb-1 text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">
        {icon}
        {title}
      </div>
      {children}
    </div>
  )
}

// ── 用量报告面板：近 30 天概览 / 按天 / 按模型 / 常用工具与技能（insights 插件） ──
export function InsightsPanel({ onClose, bare }: InsightsPanelProps) {
  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-insights', 30],
    queryFn: () => agentInsightsApi.get(30),
    retry: false,
    staleTime: 30_000,
  })

  const totals = data?.totals ?? null
  const byDay = data?.by_day ?? []
  const byModel = data?.by_model ?? []
  const topTools = (data?.top_tools ?? []).slice(0, 8)
  const topSkills = (data?.top_skills ?? []).slice(0, 8)
  const dayMax = byDay.reduce((a, d) => Math.max(a, d.prompt_tokens + d.completion_tokens), 0)
  const toolMax = topTools.reduce((a, t) => Math.max(a, t.count), 0)

  const overviewRows = totals
    ? [
        { label: '活跃天数', value: `${data?.active_days ?? 0} / ${data?.days ?? 30} 天` },
        { label: '总轮次', value: `${totals.rounds} 轮` },
        { label: '输入 tokens', value: fmtK(totals.prompt_tokens) },
        { label: '输出 tokens', value: fmtK(totals.completion_tokens) },
        { label: 'LLM 总耗时', value: fmtMs(totals.llm_ms) },
        { label: '新增记忆', value: `${data?.memories_added ?? 0} 条` },
        { label: '新增技能', value: `${data?.skills_added ?? 0} 个` },
      ]
    : []

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="用量报告"
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
          <Gauge size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">用量报告</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭用量报告"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {isLoading && <p className="px-4 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {!isLoading && !data && (
            <p className="px-4 py-6 text-center text-[11px] text-[var(--ag-text4)]">暂无统计数据——先聊几轮再来看看</p>
          )}

          {data && (
            <>
              {/* 概览 */}
              <Section icon={<Gauge size={11} />} title="概览">
                {overviewRows.map((r) => (
                  <div
                    key={r.label}
                    className="flex items-center gap-2.5 border-b border-[var(--ag-stroke3)] py-2 last:border-0"
                  >
                    <span className="w-20 shrink-0 text-[12px] text-[var(--ag-text3)]">{r.label}</span>
                    <span className="min-w-0 flex-1 truncate text-right text-[12px] tabular-nums text-[var(--ag-text1)]">
                      {r.value}
                    </span>
                  </div>
                ))}
              </Section>

              {/* 按天用量 */}
              <Section icon={<BarChart3 size={11} />} title="按天用量">
                {byDay.length === 0 ? (
                  <p className="py-1 text-[11px] text-[var(--ag-text4)]">近 {data.days} 天暂无调用记录</p>
                ) : (
                  <div className="space-y-1 py-1" aria-label="按天用量">
                    {byDay.map((d) => {
                      const total = d.prompt_tokens + d.completion_tokens
                      const pct = dayMax > 0 ? Math.round((total / dayMax) * 100) : 0
                      return (
                        <div key={d.date} className="flex items-center gap-2">
                          <span className="w-14 shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]">
                            {d.date.slice(5)}
                          </span>
                          <div className="h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-black/6">
                            <div
                              className="h-full rounded-full bg-[var(--ag-accent)]/70"
                              style={{ width: `${Math.max(pct, total > 0 ? 2 : 0)}%` }}
                            />
                          </div>
                          <span className="w-16 shrink-0 text-right text-[10px] tabular-nums text-[var(--ag-text3)]">
                            {fmtK(total)} · {d.rounds} 轮
                          </span>
                        </div>
                      )
                    })}
                  </div>
                )}
              </Section>

              {/* 按模型 */}
              <Section icon={<Zap size={11} />} title="按模型">
                {byModel.length === 0 ? (
                  <p className="py-1 text-[11px] text-[var(--ag-text4)]">暂无模型用量</p>
                ) : (
                  byModel.map((m) => (
                    <div key={m.model} className="flex items-center gap-2.5 py-1.5">
                      <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--ag-text2)]">{m.model}</span>
                      <span className="shrink-0 text-[11px] tabular-nums text-[var(--ag-text3)]">
                        输入 {fmtK(m.prompt_tokens)} · 输出 {fmtK(m.completion_tokens)}
                      </span>
                    </div>
                  ))
                )}
              </Section>

              {/* 常用工具 TOP8 */}
              <Section icon={<Wrench size={11} />} title="常用工具 TOP 8">
                {topTools.length === 0 ? (
                  <p className="py-1 text-[11px] text-[var(--ag-text4)]">暂无工具调用</p>
                ) : (
                  <div className="space-y-1 py-1" aria-label="常用工具">
                    {topTools.map((t) => {
                      const pct = toolMax > 0 ? Math.round((t.count / toolMax) * 100) : 0
                      return (
                        <div key={t.name} className="flex items-center gap-2">
                          <span className="w-28 shrink-0 truncate text-[11px] text-[var(--ag-text2)]">{t.name}</span>
                          <div className="h-2 min-w-0 flex-1 overflow-hidden rounded-full bg-black/6">
                            <div
                              className="h-full rounded-full bg-[var(--ag-green)]/70"
                              style={{ width: `${Math.max(pct, t.count > 0 ? 2 : 0)}%` }}
                            />
                          </div>
                          <span className="w-8 shrink-0 text-right text-[10px] tabular-nums text-[var(--ag-text3)]">
                            {t.count}
                          </span>
                        </div>
                      )
                    })}
                  </div>
                )}
              </Section>

              {/* 常用技能 TOP8 */}
              <Section icon={<Brain size={11} />} title="常用技能 TOP 8">
                {topSkills.length === 0 ? (
                  <p className="py-1 text-[11px] text-[var(--ag-text4)]">暂无技能使用记录</p>
                ) : (
                  topSkills.map((s) => (
                    <div key={s.name} className="flex items-center gap-2.5 py-1.5">
                      <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--ag-text2)]">{s.name}</span>
                      <span className="shrink-0 text-[11px] tabular-nums text-[var(--ag-text3)]">{s.count} 次</span>
                    </div>
                  ))
                )}
              </Section>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

export default InsightsPanel
