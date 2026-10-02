import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Brain, Clock, Gauge, MessageSquare, X, Zap } from 'lucide-react'
import { agentCronApi, agentMemoryApi, agentSkillApi } from '@/lib/api'
import type { AgentChatMsg } from '../types'

export interface UsagePanelProps {
  messages: AgentChatMsg[]
  onClose: () => void
}

function fmtK(n: number): string {
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n)
}

// ── /usage 概览：当前会话上下文估算 + 插件资产计数 ──
export function UsagePanel({ messages, onClose }: UsagePanelProps) {
  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const chars = messages.reduce((acc, m) => acc + (m.content?.length || 0) + (m.reasoning?.length || 0), 0)
  // 中英混合粗估：1 token ≈ 2 字符
  const estTokens = Math.ceil(chars / 2)

  const { data: mem } = useQuery({
    queryKey: ['agent-memories', ''],
    queryFn: () => agentMemoryApi.list(),
    retry: false,
    staleTime: 60_000,
  })
  const { data: skills } = useQuery({
    queryKey: ['agent-skills'],
    queryFn: () => agentSkillApi.list(),
    retry: false,
    staleTime: 60_000,
  })
  const { data: cron } = useQuery({
    queryKey: ['agent-cron-jobs'],
    queryFn: () => agentCronApi.list(),
    retry: false,
    staleTime: 60_000,
  })

  const userMsgs = messages.filter((m) => m.role === 'user').length
  const assistantMsgs = messages.filter((m) => m.role === 'assistant').length

  const rows = [
    {
      icon: <MessageSquare size={13} />,
      label: '当前会话',
      value: `${messages.length} 条消息（我 ${userMsgs} / 助手 ${assistantMsgs}）`,
    },
    { icon: <Gauge size={13} />, label: '估算上下文', value: `≈ ${fmtK(estTokens)} tokens（${fmtK(chars)} 字符）` },
    { icon: <Brain size={13} />, label: '记忆', value: `${mem?.memories?.length ?? 0} 条` },
    { icon: <Zap size={13} />, label: '技能', value: `${skills?.skills?.length ?? 0} 个` },
    { icon: <Clock size={13} />, label: '定时任务', value: `${cron?.jobs?.length ?? 0} 个` },
  ]

  return (
    <div
      className="absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="用量与概览"
    >
      <div
        className="w-full max-w-sm overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
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
        <div className="px-4 py-2">
          {rows.map((r) => (
            <div
              key={r.label}
              className="flex items-center gap-2.5 border-b border-[var(--ag-stroke3)] py-2.5 last:border-0"
            >
              <span className="shrink-0 text-[var(--ag-text3)]">{r.icon}</span>
              <span className="w-20 shrink-0 text-[12px] text-[var(--ag-text3)]">{r.label}</span>
              <span className="min-w-0 flex-1 truncate text-right text-[12px] tabular-nums text-[var(--ag-text1)]">
                {r.value}
              </span>
            </div>
          ))}
        </div>
        <p className="border-t border-[var(--ag-stroke3)] px-4 py-2 text-[10px] leading-relaxed text-[var(--ag-text4)]">
          tokens 为字符粗估（1 token ≈ 2 字符），模型实际上下文以服务商统计为准。
        </p>
      </div>
    </div>
  )
}

export default UsagePanel
