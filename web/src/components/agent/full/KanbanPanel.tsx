import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronLeft, ChevronRight, SquareKanban, Trash2, X } from 'lucide-react'
import { agentKanbanApi, type AgentKanbanCard } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'
import { ProfileChip } from './ProfileChip'

const COLUMNS = [
  { id: 'todo', label: '待办' },
  { id: 'doing', label: '进行中' },
  { id: 'done', label: '已完成' },
] as const

type ColumnId = (typeof COLUMNS)[number]['id']

const EMPTY_HINTS: Record<ColumnId, string> = {
  todo: '暂无待办，输入回车创建',
  doing: '暂无进行中的卡片',
  done: '还没有已完成的卡片',
}

export interface KanbanPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

// ── 看板面板：三列卡片（kanban 插件的 UI 入口），助手与用户共建任务板 ──
export function KanbanPanel({ onClose, bare }: KanbanPanelProps) {
  const queryClient = useQueryClient()
  const [newTitle, setNewTitle] = useState('')
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-kanban'],
    queryFn: () => agentKanbanApi.list(),
    retry: false,
  })
  const cards = data?.cards || []

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-kanban'] })

  const createMut = useMutation({
    mutationFn: (title: string) => agentKanbanApi.create({ title }),
    onSuccess: () => {
      setNewTitle('')
      invalidate()
    },
    onError: () => toast('error', '创建失败'),
  })

  const moveMut = useMutation({
    mutationFn: ({ id, column }: { id: string; column: ColumnId }) => agentKanbanApi.update(id, { column }),
    onSuccess: invalidate,
    onError: () => toast('error', '移动失败'),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => agentKanbanApi.remove(id),
    onSuccess: () => {
      setConfirmDelete(null)
      invalidate()
    },
    onError: () => toast('error', '删除失败'),
  })

  const submitNew = () => {
    const title = newTitle.trim()
    if (!title || createMut.isPending) return
    createMut.mutate(title)
  }

  const renderCard = (card: AgentKanbanCard, colIdx: number) => {
    const prev = colIdx > 0 ? COLUMNS[colIdx - 1].id : null
    const next = colIdx < COLUMNS.length - 1 ? COLUMNS[colIdx + 1].id : null
    return (
      <div
        key={card.id}
        className="group rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] p-2.5 hover:border-[var(--ag-stroke2)]"
        onMouseLeave={() => setConfirmDelete((cur) => (cur === card.id ? null : cur))}
      >
        <div className="text-[13px] font-medium leading-snug text-[var(--ag-text1)]">{card.title}</div>
        {card.description && (
          <p className="mt-1 line-clamp-2 text-[11px] leading-relaxed text-[var(--ag-text3)]">{card.description}</p>
        )}
        <div className="mt-1.5 flex items-center gap-1.5">
          <span
            className={cn(
              'shrink-0 rounded-full px-1.5 py-px text-[10px] font-medium',
              card.created_by === 'agent'
                ? 'bg-[var(--ag-accent)]/10 text-[var(--ag-accent)]'
                : 'bg-black/5 text-[var(--ag-text3)]'
            )}
          >
            {card.created_by === 'agent' ? '助手' : '手动'}
          </span>
          {card.comment && (
            <span className="min-w-0 flex-1 truncate text-[11px] italic text-[var(--ag-text4)]" title={card.comment}>
              {card.comment}
            </span>
          )}
          <span className="min-w-0 flex-1" />
          {/* hover 操作：前移 / 后移 / 删除（二次确认） */}
          <span className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100">
            {prev && (
              <button
                type="button"
                title={`移到${COLUMNS[colIdx - 1].label}`}
                aria-label={`前移 ${card.title}`}
                onClick={() => moveMut.mutate({ id: card.id, column: prev })}
                className="rounded p-0.5 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)]"
              >
                <ChevronLeft size={12} />
              </button>
            )}
            {next && (
              <button
                type="button"
                title={`移到${COLUMNS[colIdx + 1].label}`}
                aria-label={`后移 ${card.title}`}
                onClick={() => moveMut.mutate({ id: card.id, column: next })}
                className="rounded p-0.5 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)]"
              >
                <ChevronRight size={12} />
              </button>
            )}
            {confirmDelete === card.id ? (
              <button
                type="button"
                title="确认删除"
                aria-label={`确认删除 ${card.title}`}
                onClick={() => deleteMut.mutate(card.id)}
                className="rounded p-0.5 text-[var(--ag-red)] hover:bg-[var(--ag-red)]/10"
              >
                <Check size={12} />
              </button>
            ) : (
              <button
                type="button"
                title="删除"
                aria-label={`删除 ${card.title}`}
                onClick={() => setConfirmDelete(card.id)}
                className="rounded p-0.5 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-red)]"
              >
                <Trash2 size={12} />
              </button>
            )}
          </span>
        </div>
      </div>
    )
  }

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="看板"
    >
      <div
        className={
          bare
            ? 'mx-auto flex min-h-0 w-full max-w-5xl flex-1 flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
            : 'flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
        }
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <SquareKanban size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">看板</span>
          <ProfileChip />
          <span className="text-[11px] text-[var(--ag-text4)]">与助手共建的任务板</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭看板面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        {/* 三列 */}
        <div className="grid min-h-0 flex-1 grid-cols-3 gap-3 overflow-hidden p-3">
          {COLUMNS.map((col, colIdx) => {
            const colCards = cards.filter((c) => c.column === col.id)
            return (
              <div
                key={col.id}
                aria-label={`看板列 ${col.label}`}
                className="flex min-h-0 flex-col rounded-xl bg-[var(--ag-sidebar)]/60 p-2"
              >
                <div className="flex items-center gap-1.5 px-1 pb-1.5 pt-0.5">
                  <span className="text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text3)]">
                    {col.label}
                  </span>
                  <span className="rounded-full bg-black/5 px-1.5 text-[10px] tabular-nums text-[var(--ag-text4)]">
                    {colCards.length}
                  </span>
                </div>

                {/* 待办列顶部：内联新建（Enter 创建） */}
                {col.id === 'todo' && (
                  <input
                    value={newTitle}
                    onChange={(e) => setNewTitle(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.nativeEvent.isComposing) return
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        submitNew()
                      }
                    }}
                    placeholder="新建卡片，Enter 创建…"
                    aria-label="新建看板卡片"
                    className="mb-2 w-full shrink-0 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
                  />
                )}

                <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-0.5">
                  {isLoading ? (
                    [0, 1].map((i) => (
                      <div
                        key={i}
                        aria-label="加载中"
                        className="animate-pulse rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] p-2.5"
                      >
                        <div className="h-3 w-3/4 rounded bg-black/8" />
                        <div className="mt-2 h-2.5 w-full rounded bg-black/6" />
                        <div className="mt-2 h-2.5 w-1/3 rounded bg-black/6" />
                      </div>
                    ))
                  ) : colCards.length === 0 ? (
                    <p className="px-1 py-4 text-center text-[11px] text-[var(--ag-text4)]">{EMPTY_HINTS[col.id]}</p>
                  ) : (
                    colCards.map((c) => renderCard(c, colIdx))
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

export default KanbanPanel
