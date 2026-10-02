import { useQuery } from '@tanstack/react-query'
import { Layers } from 'lucide-react'
import { agentProfilesApi } from '@/lib/api'

/** 当前生效档案的小徽标（共享 ['agent-profiles'] 查询缓存） */
export function ProfileChip() {
  const { data } = useQuery({
    queryKey: ['agent-profiles'],
    queryFn: () => agentProfilesApi.list(),
    staleTime: 30_000,
    retry: false,
  })
  const active = (data?.profiles || []).find((p) => p.is_active)
  if (!active) return null
  return (
    <span
      aria-label="当前档案"
      title={`当前档案：${active.name}（记忆/技能等按档案隔离）`}
      className="flex shrink-0 items-center gap-1 rounded-full border border-[var(--ag-stroke3)] bg-[var(--ag-sidebar)]/60 px-2 py-0.5 text-[10px] font-medium text-[var(--ag-text3)]"
    >
      <Layers size={10} className="text-[var(--ag-accent)]" />
      档案:{active.name}
    </span>
  )
}

export default ProfileChip
