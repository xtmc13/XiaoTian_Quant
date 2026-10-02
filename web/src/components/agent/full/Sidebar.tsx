import { useMemo, useState } from 'react'
import {
  Brain,
  Clock,
  Download,
  MessageSquare,
  MoreHorizontal,
  Pin,
  PinOff,
  Pencil,
  Plus,
  Puzzle,
  Search,
  Send,
  Settings,
  Trash2,
  Zap,
} from 'lucide-react'
import type { AgentConversationSummary } from '@/lib/api'
import { agentConversationApi } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'
import { DefaultPet } from '../pet/DefaultPet'
import { cn } from '@/lib/utils'

const PINNED_KEY = 'xt-agent-pinned'

function loadPinned(): string[] {
  try {
    return JSON.parse(localStorage.getItem(PINNED_KEY) || '[]')
  } catch {
    return []
  }
}

function toMs(ts?: number | string | null): number {
  if (ts === undefined || ts === null || ts === '') return 0
  const t = typeof ts === 'number' ? (ts < 1e12 ? ts * 1000 : ts) : new Date(ts).getTime()
  return Number.isNaN(t) ? 0 : t
}

function dateBucket(ts?: number | string | null): string {
  const t = toMs(ts)
  if (!t) return '更早'
  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const day = 86_400_000
  if (t >= startOfToday) return '今天'
  if (t >= startOfToday - day) return '昨天'
  if (t >= startOfToday - 7 * day) return '近 7 天'
  return '更早'
}

function shortTime(ts?: number | string | null): string {
  const t = toMs(ts)
  if (!t) return ''
  const d = new Date(t)
  const today = dateBucket(t) === '今天'
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return today ? hm : `${d.getMonth() + 1}/${d.getDate()} ${hm}`
}

export interface PluginNavItem {
  id: string
  label: string
  icon?: string
}

export interface AgentSidebarProps {
  conversations: AgentConversationSummary[]
  currentId: string | null
  onSelect: (id: string) => void
  onNew: () => void
  onRename: (id: string, title: string) => void
  onRemove: (id: string) => void
  onOpenSettings: () => void
  /** 插件贡献的导航入口（来自 /api/agent/plugins 清单） */
  pluginNav?: PluginNavItem[]
  onPluginNav?: (id: string) => void
}

// ── 左侧栏：导航 + 搜索 + 置顶 + 按日期分组的会话列表 ──
export function AgentSidebar({
  conversations,
  currentId,
  onSelect,
  onNew,
  onRename,
  onRemove,
  onOpenSettings,
  pluginNav,
  onPluginNav,
}: AgentSidebarProps) {
  const [query, setQuery] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [pinned, setPinned] = useState<string[]>(loadPinned)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')
  const [menuFor, setMenuFor] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [exporting, setExporting] = useState(false)
  const nickname = useAuthStore((s) => s.user?.nickname || s.user?.username || '')

  const togglePin = (id: string) => {
    setPinned((prev) => {
      const next = prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
      try {
        localStorage.setItem(PINNED_KEY, JSON.stringify(next))
      } catch {
        /* 静默 */
      }
      return next
    })
  }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return conversations
    return conversations.filter((c) => (c.title || '').toLowerCase().includes(q))
  }, [conversations, query])

  const pinnedRows = useMemo(() => filtered.filter((c) => pinned.includes(c.id)), [filtered, pinned])
  const restRows = useMemo(() => filtered.filter((c) => !pinned.includes(c.id)), [filtered, pinned])

  const groups = useMemo(() => {
    const order = ['今天', '昨天', '近 7 天', '更早']
    const map = new Map<string, AgentConversationSummary[]>()
    for (const c of restRows) {
      const b = dateBucket(c.updated_at)
      if (!map.has(b)) map.set(b, [])
      map.get(b)!.push(c)
    }
    return order.filter((b) => map.has(b)).map((b) => ({ bucket: b, rows: map.get(b)! }))
  }, [restRows])

  const commitRename = () => {
    if (editingId && editValue.trim()) onRename(editingId, editValue.trim())
    setEditingId(null)
  }

  const exportConv = async (c: AgentConversationSummary) => {
    setMenuFor(null)
    setExporting(true)
    try {
      const detail = await agentConversationApi.get(c.id)
      const blob = new Blob([JSON.stringify(detail, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${c.title || 'conversation'}.json`
      a.click()
      URL.revokeObjectURL(url)
    } catch {
      /* 导出失败静默 */
    } finally {
      setExporting(false)
    }
  }

  const renderRow = (c: AgentConversationSummary) => {
    const active = c.id === currentId
    if (editingId === c.id) {
      return (
        <div key={c.id} className="flex items-center gap-1 px-2 py-0.5">
          <input
            value={editValue}
            onChange={(e) => setEditValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename()
              if (e.key === 'Escape') setEditingId(null)
            }}
            onBlur={commitRename}
            aria-label="重命名会话"
            autoFocus
            className="min-w-0 flex-1 rounded-md border border-[var(--ag-accent)]/50 bg-[var(--ag-card)] px-1.5 py-1 text-[12px] text-[var(--ag-text1)] focus:outline-none"
          />
        </div>
      )
    }
    return (
      <div
        key={c.id}
        role="button"
        tabIndex={0}
        aria-current={active ? 'true' : undefined}
        onClick={() => onSelect(c.id)}
        onKeyDown={(e) => e.key === 'Enter' && onSelect(c.id)}
        className={cn(
          'group relative flex min-h-[1.625rem] cursor-pointer items-center gap-1.5 rounded-md py-0.5 pl-2 pr-2',
          active ? 'bg-black/5 text-[var(--ag-text1)]' : 'text-[var(--ag-text2)] hover:bg-black/4'
        )}
      >
        <MessageSquare size={12} className="size-3.5 shrink-0 text-[var(--ag-text4)]" />
        <span className="min-w-0 flex-1 truncate text-[12px]">{c.title || '未命名会话'}</span>
        <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)] group-hover:hidden">
          {shortTime(c.updated_at)}
        </span>
        <span className="hidden shrink-0 items-center group-hover:flex">
          <button
            type="button"
            title="会话操作"
            aria-label={`会话操作 ${c.title}`}
            onClick={(e) => {
              e.stopPropagation()
              setConfirmDelete(null)
              setMenuFor(menuFor === c.id ? null : c.id)
            }}
            className="rounded p-0.5 text-[var(--ag-text3)] hover:text-[var(--ag-text1)]"
          >
            <MoreHorizontal size={13} />
          </button>
        </span>
        {menuFor === c.id && (
          <div
            role="menu"
            tabIndex={-1}
            className="absolute right-0 top-6 z-30 w-36 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] py-1 shadow-[var(--ag-shadow-panel)]"
            onClick={(e) => e.stopPropagation()}
            onMouseLeave={() => {
              setMenuFor(null)
              setConfirmDelete(null)
            }}
          >
            {confirmDelete === c.id ? (
              <button
                type="button"
                role="menuitem"
                aria-label={`确认删除会话 ${c.title}`}
                onClick={() => {
                  setMenuFor(null)
                  setConfirmDelete(null)
                  onRemove(c.id)
                }}
                className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] font-medium text-[var(--ag-red)] hover:bg-[var(--ag-red)]/8"
              >
                <Trash2 size={12} />
                确认删除？
              </button>
            ) : (
              <>
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    togglePin(c.id)
                    setMenuFor(null)
                  }}
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-black/5"
                >
                  {pinned.includes(c.id) ? <PinOff size={12} /> : <Pin size={12} />}
                  {pinned.includes(c.id) ? '取消置顶' : '置顶'}
                </button>
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    setEditValue(c.title || '')
                    setEditingId(c.id)
                    setMenuFor(null)
                  }}
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-black/5"
                >
                  <Pencil size={12} />
                  重命名
                </button>
                <button
                  type="button"
                  role="menuitem"
                  disabled={exporting}
                  onClick={() => exportConv(c)}
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-black/5 disabled:opacity-50"
                >
                  <Download size={12} />
                  导出 JSON
                </button>
                <button
                  type="button"
                  role="menuitem"
                  aria-label={`删除会话 ${c.title}`}
                  onClick={() => setConfirmDelete(c.id)}
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-red)] hover:bg-[var(--ag-red)]/8"
                >
                  <Trash2 size={12} />
                  删除
                </button>
              </>
            )}
          </div>
        )}
      </div>
    )
  }

  return (
    <aside
      aria-label="助手侧栏"
      className="flex h-full w-[var(--ag-sidebar-w)] shrink-0 flex-col border-r border-[var(--ag-sidebar-edge)] bg-[var(--ag-sidebar)]"
    >
      {/* 品牌行（对标 deepseek HARNESS logo 区） */}
      <div className="flex items-center gap-1.5 px-3 pb-2 pt-3">
        <DefaultPet size={20} />
        <span className="text-[14px] font-bold tracking-tight text-[var(--ag-text1)]">小天量化</span>
        <span className="rounded-sm border border-[var(--ag-stroke2)] px-1 py-px text-[8px] font-semibold tracking-wider text-[var(--ag-text4)]">
          AGENT
        </span>
      </div>

      {/* 导航 */}
      <nav className="flex flex-col gap-0.5 px-2.5 pb-1 pt-1">
        <button
          type="button"
          onClick={onNew}
          className="flex h-8 items-center justify-center gap-1.5 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 text-[13px] font-medium text-[var(--ag-text1)] transition-colors hover:border-[var(--ag-accent)]/50"
        >
          <Plus size={13} />
          新会话
        </button>
        <div className="mt-1 flex flex-col">
          <button
            type="button"
            onClick={onOpenSettings}
            className="flex h-7 items-center gap-1.5 rounded-md px-2 text-[13px] font-medium text-[var(--ag-text2)] hover:bg-[var(--ag-card)] hover:text-[var(--ag-text1)]"
          >
            <Settings size={13} />
            助手设置
          </button>
          {(pluginNav || []).map((n) => (
            <button
              key={n.id}
              type="button"
              onClick={() => onPluginNav?.(n.id)}
              className="flex h-7 items-center gap-1.5 rounded-md px-2 text-[13px] font-medium text-[var(--ag-text2)] hover:bg-[var(--ag-card)] hover:text-[var(--ag-text1)]"
            >
              {n.icon === 'clock' ? (
                <Clock size={13} />
              ) : n.icon === 'brain' ? (
                <Brain size={13} />
              ) : n.icon === 'zap' ? (
                <Zap size={13} />
              ) : n.icon === 'send' ? (
                <Send size={13} />
              ) : (
                <Puzzle size={13} />
              )}
              {n.label}
            </button>
          ))}
        </div>
      </nav>

      {/* 会话区头：标题 + 搜索图标（点击展开输入框，对标 dsh 工作区区头） */}
      <div className="flex items-center px-3 pb-0.5 pt-2">
        <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">会话</span>
        <span className="min-w-0 flex-1" />
        <button
          type="button"
          title="搜索会话"
          aria-label="搜索会话开关"
          aria-expanded={searchOpen}
          onClick={() => setSearchOpen((v) => !v)}
          className={cn(
            'rounded p-1 transition-colors hover:bg-[var(--ag-card)]',
            searchOpen ? 'text-[var(--ag-accent)]' : 'text-[var(--ag-text4)] hover:text-[var(--ag-text2)]'
          )}
        >
          <Search size={13} />
        </button>
      </div>
      {searchOpen && (
        <div className="px-2.5 pb-1 pt-0.5">
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜索会话…"
            aria-label="搜索会话"
            autoFocus
            className="w-full rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1 text-[12px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/50 focus:outline-none"
          />
        </div>
      )}

      {/* 会话列表 */}
      <div className="xt-thread-scroll min-h-0 flex-1 overflow-y-auto px-2.5 pb-2" aria-label="会话列表">
        {filtered.length === 0 && (
          <p className="px-1 py-4 text-center text-[11px] text-[var(--ag-text4)]">
            {query ? '无匹配会话' : '暂无历史会话'}
          </p>
        )}

        {pinnedRows.length > 0 && (
          <div className="pt-1">
            <div className="flex items-center gap-1.5 px-1 pb-0.5 pt-1">
              <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">置顶</span>
              <span className="h-px flex-1 bg-[var(--ag-stroke3)]" />
            </div>
            {pinnedRows.map(renderRow)}
          </div>
        )}

        {groups.map(({ bucket, rows }) => (
          <div key={bucket} className="pt-1">
            <div className="flex items-center gap-1.5 px-1 pb-0.5 pt-1">
              <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
                {bucket}
              </span>
              <span className="h-px flex-1 bg-[var(--ag-stroke3)]" />
            </div>
            {rows.map(renderRow)}
          </div>
        ))}
      </div>

      {/* 底部用户条（对标 dsh 侧栏底部） */}
      <div className="flex items-center gap-2 border-t border-[var(--ag-sidebar-edge)] px-3 py-2">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-[var(--ag-accent)]/20 text-[11px] font-bold text-[var(--ag-accent)]">
          {(nickname || '天')[0].toUpperCase()}
        </span>
        <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--ag-text2)]">{nickname || '小天用户'}</span>
        <span className="shrink-0 font-mono text-[9px] text-[var(--ag-text4)]">AGENT</span>
      </div>
    </aside>
  )
}

export default AgentSidebar
