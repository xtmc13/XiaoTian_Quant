import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Brain, Plus, Search, Star, Trash2, X } from 'lucide-react'
import { agentMemoryApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'

const KINDS = [
  { value: '', label: '全部' },
  { value: 'fact', label: '事实' },
  { value: 'preference', label: '偏好' },
  { value: 'observation', label: '观察' },
  { value: 'market_note', label: '市场笔记' },
]

const KIND_LABELS: Record<string, string> = {
  fact: '事实',
  preference: '偏好',
  observation: '观察',
  market_note: '市场笔记',
}

export interface MemoryPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

// ── 记忆面板：浏览 / 搜索 / 手动添加 / 删除 ──
export function MemoryPanel({ onClose, bare }: MemoryPanelProps) {
  const queryClient = useQueryClient()
  const [kind, setKind] = useState('')
  const [query, setQuery] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [content, setContent] = useState('')
  const [newKind, setNewKind] = useState('fact')
  const [importance, setImportance] = useState(1)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-memories', kind],
    queryFn: () => agentMemoryApi.list(kind || undefined),
    retry: false,
  })
  const memories = useMemo(() => {
    const list = data?.memories || []
    const q = query.trim().toLowerCase()
    if (!q) return list
    return list.filter((m) => m.content.toLowerCase().includes(q))
  }, [data, query])

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-memories'] })

  const createMut = useMutation({
    mutationFn: () => agentMemoryApi.create({ content: content.trim(), kind: newKind, importance }),
    onSuccess: () => {
      toast('success', '记忆已保存')
      setShowCreate(false)
      setContent('')
      setImportance(1)
      invalidate()
    },
    onError: (e: Error) => toast('error', e.message || '保存失败'),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => agentMemoryApi.remove(id),
    onSuccess: invalidate,
    onError: () => toast('error', '删除失败'),
  })

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="记忆"
    >
      <div
        className={
          bare
            ? 'mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
            : 'flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
        }
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <Brain size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">记忆</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={() => setShowCreate((v) => !v)}
            aria-label="新增记忆"
            className="flex items-center gap-1 rounded-full bg-[var(--ag-text1)] px-2.5 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85"
          >
            <Plus size={12} />
            新增
          </button>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭记忆面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        {/* 类型过滤 + 搜索 */}
        <div className="flex items-center gap-1.5 border-b border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-3 py-2">
          {KINDS.map((k) => (
            <button
              key={k.value}
              type="button"
              onClick={() => setKind(k.value)}
              aria-pressed={kind === k.value}
              className={cn(
                'shrink-0 rounded-full border px-2 py-0.5 text-[11px] transition-colors',
                kind === k.value
                  ? 'border-[var(--ag-accent)]/50 bg-[var(--ag-accent)]/8 text-[var(--ag-accent)]'
                  : 'border-[var(--ag-stroke3)] text-[var(--ag-text3)] hover:text-[var(--ag-text1)]'
              )}
            >
              {k.label}
            </button>
          ))}
          <span className="min-w-0 flex-1" />
          <div className="flex items-center gap-1">
            <Search size={12} className="text-[var(--ag-text4)]" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="搜索…"
              aria-label="搜索记忆"
              className="w-24 rounded-md border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-1.5 py-0.5 text-[11px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/50 focus:outline-none"
            />
          </div>
        </div>

        {/* 创建表单 */}
        {showCreate && (
          <div className="space-y-2 border-b border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-4 py-3">
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              rows={2}
              placeholder="记忆内容，如：用户偏好低杠杆短线，止损纪律严格"
              aria-label="记忆内容"
              className="w-full resize-none rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
            />
            <div className="flex flex-wrap items-center gap-2">
              <select
                value={newKind}
                onChange={(e) => setNewKind(e.target.value)}
                aria-label="记忆类型"
                className="rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] focus:outline-none"
              >
                {KINDS.filter((k) => k.value).map((k) => (
                  <option key={k.value} value={k.value}>
                    {k.label}
                  </option>
                ))}
              </select>
              <select
                value={importance}
                onChange={(e) => setImportance(Number(e.target.value))}
                aria-label="重要度"
                className="rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1.5 text-[12px] text-[var(--ag-text1)] focus:outline-none"
              >
                {[1, 2, 3, 4, 5].map((n) => (
                  <option key={n} value={n}>
                    重要度 {n}
                  </option>
                ))}
              </select>
              <button
                type="button"
                disabled={!content.trim() || createMut.isPending}
                onClick={() => createMut.mutate()}
                className="ml-auto rounded-lg bg-[var(--ag-accent)] px-3 py-1.5 text-[12px] font-medium text-white hover:opacity-90 disabled:opacity-40"
              >
                保存
              </button>
            </div>
          </div>
        )}

        {/* 列表 */}
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {!isLoading && memories.length === 0 && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              {query ? '无匹配记忆' : '暂无记忆——聊天时说「记住…」，或点右上角「新增」'}
            </p>
          )}
          {memories.map((m) => (
            <div
              key={m.id}
              className="group mb-1 flex items-start gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2 hover:border-[var(--ag-stroke2)]"
            >
              <span
                title={`重要度 ${m.importance}`}
                className={cn(
                  'mt-0.5 shrink-0',
                  m.importance >= 4 ? 'text-[var(--ag-amber)]' : 'text-[var(--ag-stroke1)]'
                )}
              >
                <Star size={12} fill={m.importance >= 4 ? 'currentColor' : 'none'} />
              </span>
              <div className="min-w-0 flex-1">
                <p className="whitespace-pre-wrap break-words text-[12px] leading-relaxed text-[var(--ag-text1)]">
                  {m.content}
                </p>
                <p className="mt-0.5 text-[10px] text-[var(--ag-text4)]">{KIND_LABELS[m.kind] || m.kind}</p>
              </div>
              <button
                type="button"
                title="删除"
                aria-label={`删除记忆 ${m.content.slice(0, 12)}`}
                onClick={() => deleteMut.mutate(m.id)}
                className="shrink-0 rounded p-1 text-[var(--ag-text3)] opacity-0 transition-opacity hover:bg-black/5 hover:text-[var(--ag-red)] group-hover:opacity-100"
              >
                <Trash2 size={12} />
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export default MemoryPanel
