import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Brain,
  Clock,
  Download,
  FileText,
  MessageSquare,
  MoreHorizontal,
  Pin,
  PinOff,
  Pencil,
  Plus,
  Puzzle,
  FolderOpen,
  Search,
  Send,
  Trash2,
  Zap,
} from 'lucide-react'
import type { AgentConversationDetail, AgentConversationSummary } from '@/lib/api'
import { agentConversationApi } from '@/lib/api'
import { useAppStore } from '@/stores/appStore'
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

// ── 会话导出为 Markdown（思考过程折叠为引用行） ──
function conversationToMarkdown(detail: AgentConversationDetail): string {
  const lines: string[] = [`# ${detail.title || '未命名会话'}`, '']
  for (const m of detail.messages || []) {
    if (m.role === 'user') {
      lines.push('**用户**', '', m.content, '')
    } else if (m.role === 'assistant') {
      lines.push('**助手**', '')
      if (m.reasoning) {
        for (const l of m.reasoning.split('\n')) lines.push(`> 思考:${l}`)
        lines.push('')
      }
      if (m.content) lines.push(m.content, '')
    }
  }
  return lines.join('\n')
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
  pluginNav,
  onPluginNav,
}: AgentSidebarProps) {
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [pinned, setPinned] = useState<string[]>(loadPinned)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')
  const [menuFor, setMenuFor] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [exporting, setExporting] = useState(false)

  // ── 折叠：与交易系统侧栏共用 appStore（hover 悬停展开 / 点击 logo 切换） ──
  const { sidebarCollapsed, setSidebarCollapsed, sidebarBehavior, toggleSidebar } = useAppStore()
  const isHover = sidebarBehavior === 'hover'
  const handleMouseEnter = () => {
    if (isHover) setSidebarCollapsed(false)
  }
  const handleMouseLeave = () => {
    if (isHover) setSidebarCollapsed(true)
  }
  const handleToggle = () => {
    if (!isHover) toggleSidebar()
  }

  // ── 服务端全文搜索：输入防抖 300ms ──
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query.trim()), 300)
    return () => clearTimeout(t)
  }, [query])

  const searching = searchOpen && query.trim().length > 0
  const { data: searchData, isFetching: searchFetching } = useQuery({
    queryKey: ['agent-conversations-search', debouncedQuery],
    queryFn: () => agentConversationApi.search(debouncedQuery),
    enabled: searchOpen && debouncedQuery.length > 0,
    retry: false,
    staleTime: 15_000,
  })
  const searchResults = searching ? (searchData?.results ?? []) : []

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

  // 搜索态由服务端结果替换列表；此处不再做客户端标题过滤
  const pinnedRows = useMemo(() => conversations.filter((c) => pinned.includes(c.id)), [conversations, pinned])
  const restRows = useMemo(() => conversations.filter((c) => !pinned.includes(c.id)), [conversations, pinned])

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

  const exportConvMarkdown = async (c: AgentConversationSummary) => {
    setMenuFor(null)
    setExporting(true)
    try {
      const detail = await agentConversationApi.get(c.id)
      const blob = new Blob([conversationToMarkdown(detail)], { type: 'text/markdown' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `${c.title || 'conversation'}.md`
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
          active
            ? 'bg-[var(--ag-active-bg)] text-[var(--ag-text1)]'
            : 'text-[var(--ag-text2)] hover:bg-[var(--ag-active-hover)]'
        )}
      >
        <MessageSquare size={13} className="size-3.5 shrink-0 text-[var(--ag-text4)]" />
        <span className="min-w-0 flex-1 truncate text-[13px]">{c.title || '未命名会话'}</span>
        <span className="shrink-0 text-[11px] tabular-nums text-[var(--ag-text4)] group-hover:hidden">
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
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-[var(--ag-active-hover)]"
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
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-[var(--ag-active-hover)]"
                >
                  <Pencil size={12} />
                  重命名
                </button>
                <button
                  type="button"
                  role="menuitem"
                  disabled={exporting}
                  onClick={() => exportConv(c)}
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-[var(--ag-active-hover)] disabled:opacity-50"
                >
                  <Download size={12} />
                  导出 JSON
                </button>
                <button
                  type="button"
                  role="menuitem"
                  disabled={exporting}
                  onClick={() => exportConvMarkdown(c)}
                  className="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-left text-[12px] text-[var(--ag-text2)] hover:bg-[var(--ag-active-hover)] disabled:opacity-50"
                >
                  <FileText size={12} />
                  导出 Markdown
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
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      className={cn(
        'flex h-full shrink-0 flex-col border-r border-[var(--ag-sidebar-edge)] bg-[var(--ag-sidebar)] transition-[width] duration-200',
        sidebarCollapsed ? 'w-14' : 'w-40'
      )}
    >
      {/* 品牌行（对标 deepseek HARNESS logo 区，点击折叠/展开） */}
      <div
        className="flex cursor-pointer items-center gap-1.5 px-3.5 pb-2 pt-3.5"
        onClick={handleToggle}
        title={sidebarCollapsed ? '展开侧栏' : '折叠侧栏'}
      >
        <DefaultPet size={22} />
        {!sidebarCollapsed && (
          <>
            <span className="text-[15px] font-bold tracking-tight text-[var(--ag-text1)]">小天量化</span>
            <span className="rounded-[4px] bg-[var(--ag-fg)] px-1 py-px text-[8px] font-semibold tracking-wider text-[var(--ag-bg)]">
              AGENT
            </span>
          </>
        )}
      </div>

      {/* 导航 */}
      <nav className="flex flex-col gap-0.5 px-2.5 pb-1 pt-1">
        <button
          type="button"
          onClick={onNew}
          title="新会话"
          className={cn(
            'flex h-9 items-center gap-1.5 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 text-[13px] font-medium text-[var(--ag-text1)] transition-colors hover:border-[var(--ag-accent)]/50',
            sidebarCollapsed && 'justify-center'
          )}
        >
          <Plus size={14} />
          {!sidebarCollapsed && '新会话'}
        </button>
        <div className="mt-1 flex flex-col">
          {(pluginNav || []).length > 0 && !sidebarCollapsed && (
            <div className="px-2 pb-0.5 pt-1 text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">
              扩展
            </div>
          )}
          {(pluginNav || []).map((n) => (
            <button
              key={n.id}
              type="button"
              onClick={() => onPluginNav?.(n.id)}
              title={n.label}
              className={cn(
                'flex h-7 items-center gap-1.5 rounded-md px-2 text-[13px] font-medium text-[var(--ag-text2)] hover:bg-[var(--ag-active-hover)] hover:text-[var(--ag-text1)]',
                sidebarCollapsed && 'justify-center'
              )}
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
              {!sidebarCollapsed && n.label}
            </button>
          ))}
        </div>
      </nav>

      {!sidebarCollapsed && (
        <>
      {/* 文件夹行（对标工作区目录） */}
      <div className="mx-2.5 mb-0.5 flex items-center gap-1.5 rounded-md px-1.5 py-1 text-[12px] text-[var(--ag-text2)]">
        <FolderOpen size={13} className="shrink-0 text-[var(--ag-accent)]" />
        <span className="min-w-0 flex-1 truncate">全部会话</span>
      </div>

      {/* 会话区头：标题 + 搜索图标（点击展开输入框，对标 dsh 工作区区头） */}
      <div className="flex items-center px-3 pb-0.5 pt-2">
        <span className="text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">会话</span>
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

      {/* 会话列表：搜索态显示服务端全文结果（标题 + 摘要），否则按日期分组 */}
      <div className="xt-thread-scroll min-h-0 flex-1 overflow-y-auto px-2.5 pb-2" aria-label="会话列表">
        {searching ? (
          <>
            {searchResults.length === 0 && (
              <p className="px-1 py-4 text-center text-[11px] text-[var(--ag-text4)]">
                {searchFetching ? '搜索中…' : '无匹配会话'}
              </p>
            )}
            {searchResults.map((r) => (
              <div
                key={r.id}
                role="button"
                tabIndex={0}
                aria-current={r.id === currentId ? 'true' : undefined}
                onClick={() => onSelect(r.id)}
                onKeyDown={(e) => e.key === 'Enter' && onSelect(r.id)}
                className={cn(
                  'flex cursor-pointer flex-col gap-0.5 rounded-md px-2 py-1.5',
                  r.id === currentId ? 'bg-[var(--ag-active-bg)]' : 'hover:bg-[var(--ag-active-hover)]'
                )}
              >
                <span className="truncate text-[12px] text-[var(--ag-text1)]">{r.title || '未命名会话'}</span>
                {r.snippet && <span className="line-clamp-2 text-[11px] text-[var(--ag-text4)]">{r.snippet}</span>}
              </div>
            ))}
          </>
        ) : (
          <>
            {conversations.length === 0 && (
              <p className="px-1 py-4 text-center text-[11px] text-[var(--ag-text4)]">暂无历史会话</p>
            )}

            {pinnedRows.length > 0 && (
              <div className="pt-1">
                <div className="flex items-center gap-1.5 px-1 pb-0.5 pt-1">
                  <span className="text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">置顶</span>
                  <span className="h-px flex-1 bg-[var(--ag-stroke3)]" />
                </div>
                {pinnedRows.map(renderRow)}
              </div>
            )}

            {groups.map(({ bucket, rows }) => (
              <div key={bucket} className="pt-1">
                <div className="flex items-center gap-1.5 px-1 pb-0.5 pt-1">
                  <span className="text-[11px] font-semibold tracking-[0.06em] text-[var(--ag-text4)]">{bucket}</span>
                  <span className="h-px flex-1 bg-[var(--ag-stroke3)]" />
                </div>
                {rows.map(renderRow)}
              </div>
            ))}
          </>
        )}
      </div>
        </>
      )}
    </aside>
  )
}

export default AgentSidebar
