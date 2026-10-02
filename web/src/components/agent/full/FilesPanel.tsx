import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, FileClock, History, ShieldAlert, Undo2, X } from 'lucide-react'
import { agentFilesApi, type AgentFileCheckpoint } from '@/lib/api'
import { useAuthStore } from '@/stores/authStore'
import { toast } from '@/lib/useToast'
import { relativeTime } from '../types'

export interface FilesPanelProps {
  onClose: () => void
  /** 整页模式（嵌入主内容区，非弹窗） */
  bare?: boolean
}

function fmtSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

// ── 文件回滚面板：助手改动的文件检查点列表，可一键恢复（仅管理员） ──
export function FilesPanel({ onClose, bare }: FilesPanelProps) {
  const queryClient = useQueryClient()
  const isAdmin = useAuthStore((s) => s.user?.role === 'admin')
  const [confirmId, setConfirmId] = useState<AgentFileCheckpoint['id'] | null>(null)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const { data, isLoading } = useQuery({
    queryKey: ['agent-file-checkpoints'],
    queryFn: () => agentFilesApi.checkpoints(),
    enabled: isAdmin,
    retry: false,
  })
  const checkpoints = data?.checkpoints || []

  const rollbackMut = useMutation({
    mutationFn: (id: AgentFileCheckpoint['id']) => agentFilesApi.rollback(id),
    onSuccess: (res) => {
      setConfirmId(null)
      toast('success', `已恢复 ${res.restored}`)
      queryClient.invalidateQueries({ queryKey: ['agent-file-checkpoints'] })
    },
    onError: (e: Error) => {
      setConfirmId(null)
      toast('error', e.message || '回滚失败')
    },
  })

  return (
    <div
      className={
        bare ? 'flex h-full flex-col' : 'absolute inset-0 z-20 flex items-center justify-center bg-black/50 p-4'
      }
      onClick={onClose}
      role="dialog"
      aria-label="文件回滚"
    >
      <div
        className={
          bare
            ? 'mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
            : 'flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] shadow-[var(--ag-shadow-panel)]'
        }
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-4 py-3">
          <History size={15} className="text-[var(--ag-accent)]" />
          <span className="text-[13px] font-semibold text-[var(--ag-text1)]">文件回滚</span>
          <span className="text-[11px] text-[var(--ag-text4)]">助手修改文件前的检查点</span>
          <span className="min-w-0 flex-1" />
          <button
            type="button"
            onClick={onClose}
            aria-label="关闭文件回滚面板"
            className="rounded p-1 text-[var(--ag-text3)] hover:bg-black/5"
          >
            <X size={15} />
          </button>
        </div>

        {!isAdmin ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-10">
            <ShieldAlert size={20} className="text-[var(--ag-amber)]" />
            <p className="text-[12px] text-[var(--ag-text3)]">无权限：文件回滚仅对管理员开放</p>
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {isLoading && <p className="px-2 py-6 text-center text-[11px] text-[var(--ag-text4)]">加载中…</p>}
            {!isLoading && checkpoints.length === 0 && (
              <div className="flex flex-col items-center gap-2 px-2 py-10">
                <FileClock size={20} className="text-[var(--ag-text4)]" />
                <p className="text-center text-[11px] leading-relaxed text-[var(--ag-text4)]">
                  暂无检查点——助手修改文件时会自动创建
                </p>
              </div>
            )}
            {checkpoints.map((c) => (
              <div
                key={String(c.id)}
                className="mb-1 flex items-center gap-2 rounded-xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-3 py-2 hover:border-[var(--ag-stroke2)]"
              >
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono text-[12px] text-[var(--ag-text1)]" title={c.path}>
                    {c.path}
                  </p>
                  <p className="mt-0.5 flex items-center gap-1.5 text-[10px] text-[var(--ag-text4)]">
                    <span className="tabular-nums">{fmtSize(c.size)}</span>
                    <span>·</span>
                    <span>{relativeTime(c.created_at)}</span>
                    {c.conversation_id && (
                      <>
                        <span>·</span>
                        <span className="font-mono" title={`会话 ${c.conversation_id}`}>
                          会话 {c.conversation_id.slice(0, 8)}
                        </span>
                      </>
                    )}
                  </p>
                </div>
                {confirmId === c.id ? (
                  <button
                    type="button"
                    onClick={() => rollbackMut.mutate(c.id)}
                    disabled={rollbackMut.isPending}
                    aria-label={`确认恢复 ${c.path}`}
                    className="flex shrink-0 items-center gap-1 rounded-md border border-[var(--ag-red)]/40 px-2 py-1 text-[11px] font-medium text-[var(--ag-red)] hover:bg-[var(--ag-red)]/8 disabled:opacity-50"
                  >
                    <Check size={11} />
                    确认恢复？
                  </button>
                ) : (
                  <button
                    type="button"
                    onClick={() => setConfirmId(c.id)}
                    aria-label={`恢复 ${c.path}`}
                    className="flex shrink-0 items-center gap-1 rounded-md border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-text1)]"
                  >
                    <Undo2 size={11} />
                    恢复
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export default FilesPanel
