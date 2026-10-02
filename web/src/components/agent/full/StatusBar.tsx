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
  version: string
  /** 会话统计，如 "5 条 · ≈1.2k tok"（居中展示，对标 dsh 底部状态条） */
  stats?: string
}

// ── 底部状态条：左 Gateway 健康，居中会话统计，右 版本 ──
export function AgentStatusBar({ version, stats }: AgentStatusBarProps) {
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
      className="relative flex h-5 shrink-0 items-center gap-2 border-t border-[var(--ag-sidebar-edge)] bg-[var(--ag-sidebar)] px-3 text-[11px] text-[var(--ag-text3)]"
    >
      <span className={cn('flex items-center gap-1', color)} title="后端网关健康状态">
        <Activity size={10} />
        {label}
      </span>
      <span className="pointer-events-none absolute left-1/2 max-w-[50%] -translate-x-1/2 truncate text-[10px] tabular-nums text-[var(--ag-text4)]">
        {stats}
      </span>
      <span className="min-w-0 flex-1" />
      <span aria-label="版本" className="shrink-0 font-mono text-[10px] text-[var(--ag-text4)]">
        {version}
      </span>
    </footer>
  )
}

export default AgentStatusBar
