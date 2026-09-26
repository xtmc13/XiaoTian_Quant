import { useState } from 'react'
import { Check, MessageSquare, Pencil, Plus, Trash2 } from 'lucide-react'
import type { AgentConversationSummary } from '@/lib/api'
import { relativeTime } from './types'
import { cn } from '@/lib/utils'

export interface HistorySidebarProps {
  conversations: AgentConversationSummary[]
  currentId: string | null
  onSelect: (id: string) => void
  onNew: () => void
  onRename: (id: string, title: string) => void
  onRemove: (id: string) => void
}

// ── 历史侧栏：会话列表 + 新对话 + 重命名/删除 ──
export function HistorySidebar({ conversations, currentId, onSelect, onNew, onRename, onRemove }: HistorySidebarProps) {
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')

  const commitRename = () => {
    if (editingId && editValue.trim()) onRename(editingId, editValue.trim())
    setEditingId(null)
  }

  return (
    <aside className="flex w-[180px] shrink-0 flex-col border-r border-quant-border bg-quant-bg-secondary/50">
      <button
        type="button"
        onClick={onNew}
        className="mx-2 mb-1 mt-2 flex items-center gap-1.5 rounded-lg border border-quant-border px-2 py-1.5 text-[12px] text-foreground/80 transition-colors hover:border-[#1890ff]/50 hover:text-[#1890ff]"
      >
        <Plus size={13} />
        新对话
      </button>
      <div className="flex-1 min-h-0 overflow-y-auto px-1.5 pb-2" aria-label="会话列表">
        {conversations.length === 0 && <p className="px-1 py-3 text-center text-[11px] text-[#666]">暂无历史会话</p>}
        {conversations.map((c) => {
          const active = c.id === currentId
          if (editingId === c.id) {
            return (
              <div key={c.id} className="mb-0.5 flex items-center gap-1 px-1 py-1">
                <input
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') commitRename()
                    if (e.key === 'Escape') setEditingId(null)
                  }}
                  aria-label="重命名会话"
                  autoFocus
                  className="min-w-0 flex-1 rounded border border-[#1890ff]/60 bg-quant-bg-secondary px-1.5 py-1 text-[12px] text-foreground focus:outline-none"
                />
                <button
                  type="button"
                  title="确认"
                  aria-label="确认重命名"
                  onClick={commitRename}
                  className="shrink-0 rounded p-1 text-[#52c41a] hover:bg-quant-hover"
                >
                  <Check size={12} />
                </button>
              </div>
            )
          }
          return (
            <div
              key={c.id}
              className={cn(
                'group mb-0.5 flex cursor-pointer items-center gap-1 rounded-lg px-1.5 py-1.5 transition-colors',
                active ? 'bg-[#1890ff]/15 text-foreground' : 'text-foreground/70 hover:bg-quant-hover'
              )}
              onClick={() => onSelect(c.id)}
              onKeyDown={(e) => e.key === 'Enter' && onSelect(c.id)}
              role="button"
              tabIndex={0}
              aria-current={active ? 'true' : undefined}
            >
              <MessageSquare size={12} className="shrink-0 opacity-50" />
              <span className="min-w-0 flex-1 truncate text-[12px]">{c.title || '未命名会话'}</span>
              <span className="shrink-0 text-[10px] text-[#666]">{relativeTime(c.updated_at)}</span>
              <span className="hidden shrink-0 items-center gap-0.5 group-hover:flex">
                <button
                  type="button"
                  title="重命名"
                  aria-label={`重命名会话 ${c.title}`}
                  onClick={(e) => {
                    e.stopPropagation()
                    setEditValue(c.title || '')
                    setEditingId(c.id)
                  }}
                  className="rounded p-0.5 text-[#888] hover:text-foreground"
                >
                  <Pencil size={11} />
                </button>
                <button
                  type="button"
                  title="删除"
                  aria-label={`删除会话 ${c.title}`}
                  onClick={(e) => {
                    e.stopPropagation()
                    onRemove(c.id)
                  }}
                  className="rounded p-0.5 text-[#888] hover:text-[#f5222d]"
                >
                  <Trash2 size={11} />
                </button>
              </span>
            </div>
          )
        })}
      </div>
    </aside>
  )
}

export default HistorySidebar
