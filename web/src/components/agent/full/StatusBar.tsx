import { useEffect, useState } from 'react'
import { Activity } from 'lucide-react'
import { cn } from '@/lib/utils'

type GwState = 'ready' | 'connecting' | 'offline'

async function ping(): Promise<boolean> {
  try {
    const res = await fetch('/api/health', { cache: 'no-store' })
    return res.ok
  } catch {
    return false
  }
}

export interface AgentStatusBarProps {
  /** "provider" 或 "provider:model"，空串 = 默认 */
  model: string
  version: string
}

// ── 底部状态条：左 Gateway 健康，右 模型 + 版本 ──
export function AgentStatusBar({ model, version }: AgentStatusBarProps) {
  const [state, setState] = useState<GwState>('connecting')

  useEffect(() => {
    let alive = true
    const tick = async () => {
      const ok = await ping()
      if (alive) setState(ok ? 'ready' : 'offline')
    }
    void tick()
    const t = setInterval(tick, 30_000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])

  const label = state === 'ready' ? 'Gateway 就绪' : state === 'connecting' ? '连接中…' : 'Gateway 离线'
  const color =
    state === 'ready'
      ? 'text-[var(--ag-green)]'
      : state === 'connecting'
        ? 'text-[var(--ag-amber)]'
        : 'text-[var(--ag-red)]'

  return (
    <footer
      aria-label="状态条"
      className="flex h-5 shrink-0 items-center gap-2 border-t border-[var(--ag-sidebar-edge)] bg-[var(--ag-sidebar)] px-2 text-[11px] text-[var(--ag-text3)]"
    >
      <span className={cn('flex items-center gap-1', color)} title="后端网关健康状态">
        <Activity size={10} />
        {label}
      </span>
      <span className="min-w-0 flex-1" />
      {model ? (
        <span className="max-w-[40%] truncate font-mono text-[10px]" title="当前模型">
          {model}
        </span>
      ) : (
        <span className="text-[10px] text-[var(--ag-text4)]">默认模型</span>
      )}
      <span aria-label="版本" className="shrink-0 font-mono text-[10px] text-[var(--ag-text4)]">
        {version}
      </span>
    </footer>
  )
}

export default AgentStatusBar
