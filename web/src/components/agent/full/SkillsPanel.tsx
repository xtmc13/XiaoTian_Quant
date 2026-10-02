import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Play, Plus, Trash2, X, Zap } from 'lucide-react'
import { agentSkillApi } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { cn } from '@/lib/utils'

export interface SkillsPanelProps {
  /** 「使用」技能：关闭面板并把技能正文发到当前会话执行 */
  onUse: (skill: { name: string; body: string }) => void
  onClose: () => void
}

// ── 技能面板：浏览 / 创建 / 使用 / 删除（skills 插件的 UI 入口） ──
export function SkillsPanel({ onUse, onClose }: SkillsPanelProps) {
  const queryClient = useQueryClient()
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [body, setBody] = useState('')
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-skills'],
    queryFn: () => agentSkillApi.list(),
    retry: false,
  })
  const skills = data?.skills || []

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['agent-skills'] })

  const createMut = useMutation({
    mutationFn: () => agentSkillApi.create({ name: name.trim(), description: description.trim(), body: body.trim() }),
    onSuccess: () => {
      toast('success', '技能已保存（同名覆盖）')
      setShowCreate(false)
      setName('')
      setDescription('')
      setBody('')
      invalidate()
    },
    onError: (e: Error) => toast('error', e.message || '保存失败'),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) => agentSkillApi.remove(id),
    onSuccess: () => {
      setConfirmDelete(null)
      invalidate()
    },
    onError: () => toast('error', '删除失败'),
  })

  return (
    <div
      className="absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      onClick={onClose}
      role="dialog"
      aria-label="技能"
    >
      <div
        className="flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <Zap size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">技能</span>
          <span className="text-[10px] text-[var(--ag-text4)]">聊天输入 /技能名 直接调用</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={() => setShowCreate((v) => !v)}
            aria-label="新建技能"
            className="flex items-center gap-1 rounded-full bg-[var(--ag-text1)] px-2.5 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85"
          >
            <Plus size={12} />
            新建
          </button>
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭技能面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        {/* 创建表单 */}
        {showCreate && (
          <div className="space-y-2 border-b border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-4 py-3">
            <div className="flex gap-2">
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="技能名，如：每日复盘"
                aria-label="技能名"
                className="w-40 shrink-0 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
              />
              <input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="一句话说明（可选），如：收盘后复盘流程"
                aria-label="技能说明"
                className="min-w-0 flex-1 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1.5 text-[12px] text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
              />
            </div>
            <textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={4}
              placeholder="程序性指令正文：步骤、检查项、产出格式。如：&#10;1. 汇总当前持仓与今日盈亏&#10;2. 检查运行中机器人状态&#10;3. 给出 3 条明日关注点"
              aria-label="技能正文"
              className="w-full resize-none rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2.5 py-1.5 font-mono text-[11.5px] leading-relaxed text-[var(--ag-text1)] focus:border-[var(--ag-accent)]/60 focus:outline-none"
            />
            <div className="flex justify-end">
              <button
                type="button"
                disabled={!name.trim() || !body.trim() || createMut.isPending}
                onClick={() => createMut.mutate()}
                className="rounded-lg bg-[var(--ag-accent)] px-3 py-1.5 text-[12px] font-medium text-white hover:opacity-90 disabled:opacity-40"
              >
                保存
              </button>
            </div>
          </div>
        )}

        {/* 列表 */}
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
          {!isLoading && skills.length === 0 && (
            <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">
              暂无技能——点右上角「新建」，或聊天时说「把刚才的流程存成技能」
            </p>
          )}
          {skills.map((s) => (
            <div
              key={s.id}
              className="group mb-1 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2 hover:border-[var(--ag-stroke2)]"
            >
              <div className="flex items-center gap-2">
                <code className="shrink-0 rounded bg-[var(--ag-inline-code-bg)] px-1 py-0.5 font-mono text-[10.5px] text-[var(--ag-accent)]">
                  /{s.name}
                </code>
                <span className="min-w-0 flex-1 truncate text-[12px] text-[var(--ag-text3)]">
                  {s.description || '（无说明）'}
                </span>
                {s.source === 'agent' && (
                  <span className="shrink-0 rounded-full bg-[var(--ag-accent)]/8 px-1.5 py-0.5 text-[9px] text-[var(--ag-accent)]">
                    对话沉淀
                  </span>
                )}
                <span className="shrink-0 text-[10px] tabular-nums text-[var(--ag-text4)]">×{s.usage_count}</span>
                <button
                  type="button"
                  title="在会话中执行"
                  aria-label={`使用技能 ${s.name}`}
                  onClick={() => onUse({ name: s.name, body: s.body })}
                  className="shrink-0 rounded p-1 text-[var(--ag-text3)] opacity-0 transition-opacity hover:bg-black/5 hover:text-[var(--ag-accent)] group-hover:opacity-100"
                >
                  <Play size={12} />
                </button>
                <button
                  type="button"
                  title="删除"
                  aria-label={`删除技能 ${s.name}`}
                  onClick={() => setConfirmDelete(confirmDelete === s.id ? null : s.id)}
                  className={cn(
                    'shrink-0 rounded p-1 opacity-0 transition-opacity group-hover:opacity-100',
                    confirmDelete === s.id
                      ? 'bg-[var(--ag-red)]/10 text-[var(--ag-red)] opacity-100'
                      : 'text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-red)]'
                  )}
                >
                  <Trash2 size={12} />
                </button>
              </div>
              {confirmDelete === s.id && (
                <div className="mt-1.5 flex items-center gap-2 pl-1">
                  <span className="text-[11px] text-[var(--ag-red)]">确认删除该技能？</span>
                  <button
                    type="button"
                    aria-label={`确认删除技能 ${s.name}`}
                    onClick={() => deleteMut.mutate(s.id)}
                    className="rounded-md bg-[var(--ag-red)] px-2 py-0.5 text-[11px] font-medium text-white hover:opacity-90"
                  >
                    确认
                  </button>
                  <button
                    type="button"
                    onClick={() => setConfirmDelete(null)}
                    className="rounded-md border border-[var(--ag-stroke2)] px-2 py-0.5 text-[11px] text-[var(--ag-text3)]"
                  >
                    取消
                  </button>
                </div>
              )}
              <p className="mt-1 line-clamp-2 whitespace-pre-wrap break-words pl-1 font-mono text-[10.5px] leading-relaxed text-[var(--ag-text3)]">
                {s.body}
              </p>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export default SkillsPanel
