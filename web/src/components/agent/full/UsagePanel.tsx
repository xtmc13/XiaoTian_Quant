import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BarChart3, Gauge, MessageSquare, X } from 'lucide-react'
import { agentUsageApi } from '@/lib/api'
import type { AgentChatMsg } from '../types'

export interface UsagePanelProps {
  messages: AgentChatMsg[]
  onClose: () => void
  /** 当前会话 id（用于拉取该会话真实用量；未保存会话可缺省） */
  conversationId?: string | null
}

function fmtK(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n)
}

function fmtMs(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

// ── /usage 概览：当前会话真实用量 + 上下文估算条 + 近 30 天统计 ──
export function UsagePanel({ messages, onClose, conversationId }: UsagePanelProps) {
  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const chars = messages.reduce((acc, m) => acc + (m.content?.length || 0) + (m.reasoning?.length || 0), 0)
  // 中英混合粗估：1 token ≈ 2 字符
  const estTokens = Math.ceil(chars / 2)
  // 估算条分母：按 128K 上下文粗估占比
  const ctxPct = Math.min(100, Math.round((estTokens / 128_000) * 100))

  const { data: usage } = useQuery({
    queryKey: ['agent-usage', conversationId ?? null],
    queryFn: () => agentUsageApi.get(conversationId ?? undefined, 30),
    retry: false,
    staleTime: 30_000,
  })

  const userMsgs = messages.filter((m) => m.role === 'user').length
  const assistantMsgs = messages.filter((m) => m.role === 'assistant').length
  const session = usage?.session ?? null
  const totals = usage?.totals ?? null
  const byDay = usage?.by_day ?? []
  const byModel = usage?.by_model ?? []
  const dayMax = byDay.reduce((a, d) => Math.max(a, d.prompt_tokens + d.completion_tokens), 0)

  const sessionRows = session
    ? [
        { label: '轮次', value: `${session.rounds} 轮` },
        { label: '输入 tokens', value: fmtK(session.prompt_tokens) },
        { label: '输出 tokens', value: fmtK(session.completion_tokens) },
        { label: 'LLM 总耗时', value: fmtMs(session.llm_ms) },
      ]
    : [{ label: '消息', value: `${messages.length} 条（我 ${userMsgs} / 助手 ${assistantMsgs}）` }]

  return (
    <div
      className="absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="用量与概览"
    >
      <div
        className="flex max-h-full w-full max-w-sm flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <Gauge size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">用量与概览</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭用量面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {/* 当前会话 */}
          <div className="px-4 pb-1 pt-3">
            <div className="flex items-center gap-1.5 pb-1 text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">
              <MessageSquare size={11} />
              当前会话
            </div>
            {sessionRows.map((r) => (
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
            {!session && <p className="pb-1 text-[10px] text-[var(--ag-text4)]">真实用量在会话保存后由后端统计。</p>}
          </div>

          {/* 上下文估算条 */}
          <div className="border-t border-[var(--ag-stroke3)] px-4 py-3">
            <div className="flex items-center justify-between pb-1.5">
              <span className="text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">上下文估算</span>
              <span className="text-[11px] tabular-nums text-[var(--ag-text3)]">≈ {fmtK(estTokens)} / 128K tok</span>
            </div>
            <div
              role="progressbar"
              aria-label="上下文估算"
              aria-valuenow={ctxPct}
              className="h-1.5 overflow-hidden rounded-full bg-black/8"
            >
              <div
                className="h-full rounded-full bg-[var(--ag-accent)] transition-all"
                style={{ width: `${Math.max(ctxPct, estTokens > 0 ? 2 : 0)}%` }}
              />
            </div>
            <p className="pt-1.5 text-[10px] text-[var(--ag-text4)]">{fmtK(chars)} 字符，按 1 token ≈ 2 字符粗估。</p>
          </div>

          {/* 近 30 天 */}
          {totals && (
            <div className="border-t border-[var(--ag-stroke3)] px-4 py-3">
              <div className="flex items-center gap-1.5 pb-1 text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">
                <BarChart3 size={11} />近 30 天
              </div>
              <div className="flex items-center gap-2.5 border-b border-[var(--ag-stroke3)] py-2">
                <span className="w-20 shrink-0 text-[12px] text-[var(--ag-text3)]">合计</span>
                <span className="min-w-0 flex-1 text-right text-[12px] tabular-nums text-[var(--ag-text1)]">
                  {totals.rounds} 轮 · 输入 {fmtK(totals.prompt_tokens)} · 输出 {fmtK(totals.completion_tokens)} ·{' '}
                  {fmtMs(totals.llm_ms)}
                </span>
              </div>

              {byDay.length > 0 && (
                <div className="space-y-1 py-2" aria-label="按天用量">
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
                        <span className="w-10 shrink-0 text-right text-[10px] tabular-nums text-[var(--ag-text3)]">
                          {fmtK(total)}
                        </span>
                      </div>
                    )
                  })}
                </div>
              )}

              {byModel.length > 0 && (
                <div className="border-t border-[var(--ag-stroke3)] pt-1" aria-label="按模型用量">
                  {byModel.map((m) => (
                    <div key={m.model} className="flex items-center gap-2.5 py-1.5">
                      <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--ag-text2)]">{m.model}</span>
                      <span className="shrink-0 text-[11px] tabular-nums text-[var(--ag-text3)]">
                        输入 {fmtK(m.prompt_tokens)} · 输出 {fmtK(m.completion_tokens)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        <p className="shrink-0 border-t border-[var(--ag-stroke3)] px-4 py-2 text-[10px] leading-relaxed text-[var(--ag-text4)]">
          tokens 为字符粗估（1 token ≈ 2 字符），模型实际上下文以服务商统计为准。
        </p>
      </div>
    </div>
  )
}

export default UsagePanel
