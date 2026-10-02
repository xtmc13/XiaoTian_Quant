import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Sprout, X } from 'lucide-react'
import { agentJourneyApi, type AgentJourneyItem } from '@/lib/api'
import { cn } from '@/lib/utils'

export interface JourneyPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

const KIND_META: Record<AgentJourneyItem['kind'], { label: string; dot: string }> = {
  memory: { label: '记忆', dot: 'bg-[var(--ag-accent)]' },
  skill: { label: '技能', dot: 'bg-[var(--ag-green)]' },
  conversation: { label: '会话', dot: 'bg-[var(--ag-text4)]' },
}

function relTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts < 1e12 ? ts * 1000 : ts)
  const now = new Date()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  const sameDay =
    d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
  return sameDay ? `今天 ${hm}` : `${d.getMonth() + 1}-${d.getDate()} ${hm}`
}

// ── 学习轨迹面板：助手记忆/技能/会话的成长时间线（journey 插件） ──
export function JourneyPanel({ onClose, bare }: JourneyPanelProps) {
  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-journey'],
    queryFn: () => agentJourneyApi.get(50),
    retry: false,
    staleTime: 30_000,
  })
  const items = data?.items ?? []

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="学习轨迹"
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
          <Sprout size={15} className="text-[var(--ag-green)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">学习轨迹</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭学习轨迹"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-3" aria-label="学习轨迹时间线">
          {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {!isLoading && items.length === 0 && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              暂无轨迹——聊天中让助手「记住…」或沉淀技能后会出现在这里
            </p>
          )}
          {items.map((it, i) => {
            const meta = KIND_META[it.kind] || KIND_META.conversation
            const last = i === items.length - 1
            return (
              <div key={`${it.ts}-${i}`} className="flex gap-3">
                {/* 左：圆点 + 竖线 */}
                <div className="flex w-3 shrink-0 flex-col items-center">
                  <span className={cn('mt-1.5 size-2 shrink-0 rounded-full', meta.dot)} />
                  {!last && <span className="mt-1 w-px min-h-4 flex-1 bg-[var(--ag-stroke3)]" />}
                </div>
                {/* 右：内容 */}
                <div className={cn('min-w-0 flex-1', !last && 'pb-4')}>
                  <div className="flex items-center gap-1.5">
                    <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-[var(--ag-text1)]">
                      {it.title}
                    </span>
                    <span className="shrink-0 rounded-full border border-[var(--ag-stroke3)] px-1.5 py-px text-[10px] text-[var(--ag-text3)]">
                      {meta.label}
                    </span>
                    <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]">{relTime(it.ts)}</span>
                  </div>
                  {it.detail && (
                    <p className="mt-0.5 line-clamp-2 text-[11px] leading-relaxed text-[var(--ag-text3)]">
                      {it.detail}
                    </p>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

export default JourneyPanel
