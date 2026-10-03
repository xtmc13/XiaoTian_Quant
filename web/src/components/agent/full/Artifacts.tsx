import React, { useEffect, useState } from 'react'
import { Download, Eye, FileCode2, FileText, X } from 'lucide-react'
import { agentFilesApi } from '@/lib/api'

// ── Artifacts：助手写文件工具（write_file）产出的沙箱文件卡片 + 预览弹窗 ──

/** 可预览的扩展名 → 预览方式 */
type PreviewKind = 'iframe' | 'image'

function previewKind(path: string): PreviewKind | null {
  const ext = path.split('.').pop()?.toLowerCase() || ''
  if (['html', 'htm', 'txt', 'md', 'markdown', 'csv', 'json', 'pdf', 'log'].includes(ext)) return 'iframe'
  if (['svg', 'png', 'jpg', 'jpeg', 'gif', 'webp', 'ico'].includes(ext)) return 'image'
  return null
}

export function ArtifactsCard({ paths }: { paths: string[] }) {
  const [preview, setPreview] = useState<string | null>(null)
  const files = [...new Set(paths)].slice(0, 6)
  if (files.length === 0) return null
  return (
    <div className="mt-2 space-y-1" data-testid="artifacts-card">
      {files.map((p) => {
        const kind = previewKind(p)
        const name = p.split('/').pop() || p
        return (
          <div
            key={p}
            className="flex items-center gap-2 rounded-lg border border-[var(--ag-stroke3)] bg-[var(--ag-card)] px-2.5 py-1.5 text-[12px]"
          >
            {/\.(html?|svg|png|jpe?g|gif|webp)$/i.test(name) ? (
              <FileCode2 size={13} className="shrink-0 text-[var(--ag-accent)]" />
            ) : (
              <FileText size={13} className="shrink-0 text-[var(--ag-text3)]" />
            )}
            <span className="min-w-0 flex-1 truncate font-mono text-[11.5px] text-[var(--ag-text2)]" title={p}>
              {name}
            </span>
            {kind && (
              <button
                type="button"
                onClick={() => setPreview(p)}
                title="预览"
                aria-label={`预览 ${name}`}
                className="shrink-0 rounded p-1 text-[var(--ag-text3)] transition-colors hover:bg-black/5 hover:text-[var(--ag-accent)]"
              >
                <Eye size={13} />
              </button>
            )}
            <a
              href={agentFilesApi.contentUrl(p, true)}
              download={name}
              title="下载"
              aria-label={`下载 ${name}`}
              className="shrink-0 rounded p-1 text-[var(--ag-text3)] transition-colors hover:bg-black/5 hover:text-[var(--ag-text1)]"
            >
              <Download size={13} />
            </a>
          </div>
        )
      })}
      {preview && <PreviewModal path={preview} onClose={() => setPreview(null)} />}
    </div>
  )
}

function PreviewModal({ path, onClose }: { path: string; onClose: () => void }) {
  const kind = previewKind(path)
  const name = path.split('/').pop() || path
  const url = agentFilesApi.contentUrl(path)

  // Esc 关闭
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div
      role="dialog"
      aria-label={`预览 ${name}`}
      className="fixed inset-0 z-[95] flex flex-col bg-black/45 p-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="mx-auto flex h-full w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-bg)] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 border-b border-[var(--ag-stroke3)] px-3 py-2">
          <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-[var(--ag-text2)]" title={path}>
            {name}
          </span>
          <a
            href={agentFilesApi.contentUrl(path, true)}
            download={name}
            title="下载"
            aria-label="下载文件"
            className="shrink-0 rounded p-1 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)]"
          >
            <Download size={14} />
          </a>
          <button
            type="button"
            onClick={onClose}
            title="关闭预览"
            aria-label="关闭预览"
            className="shrink-0 rounded p-1 text-[var(--ag-text3)] hover:bg-black/5 hover:text-[var(--ag-text1)]"
          >
            <X size={14} />
          </button>
        </div>
        <div className="min-h-0 flex-1 bg-white">
          {kind === 'image' ? (
            <img src={url} alt={name} className="mx-auto max-h-full max-w-full object-contain" />
          ) : (
            <iframe src={url} title={name} sandbox="allow-same-origin" className="h-full w-full border-0" />
          )}
        </div>
      </div>
    </div>
  )
}
