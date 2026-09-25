import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { integrationsApi, type IntegrationStatus } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { Badge } from '@/components/ui/Badge'
import { SectionCard } from '@/components/ui/SectionCard'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { cn } from '@/lib/utils'
import { AxiosError } from 'axios'
import { Loader2, Plug, RefreshCw } from 'lucide-react'

// ── 集成状态面板（外部集成预检）──
// 数据源：GET /api/integrations/status；单项重检：POST /api/integrations/:name/check（admin）。
// 语义约定（与后端一致）：
//   绿灯 = 已配置且最近一次探测可达
//   红灯 = 已配置但探测失败/超时
//   黄灯 = 已配置但从未探测
//   灰灯 = 未配置（降级，所需 env 名见 notes；密钥取值永不下发）

function statusLight(it: IntegrationStatus): { color: string; label: string } {
  if (!it.configured) return { color: 'bg-[#666]', label: '未配置' }
  if (it.reachable === true) return { color: 'bg-[#52c41a]', label: '可达' }
  if (it.reachable === false) return { color: 'bg-[#f5222d]', label: '不可达' }
  return { color: 'bg-[#d4a017]', label: '未探测' }
}

function statusVariant(it: IntegrationStatus): 'success' | 'warning' | 'error' | 'neutral' {
  if (!it.configured) return 'neutral'
  if (it.reachable === true) return 'success'
  if (it.reachable === false) return 'error'
  return 'warning'
}

export function IntegrationStatusPanel() {
  const queryClient = useQueryClient()
  const [pendingName, setPendingName] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['integrations-status'],
    queryFn: () => integrationsApi.status(),
    refetchInterval: 60000,
  })

  const checkMut = useMutation({
    mutationFn: (name: string) => integrationsApi.check(name),
    onMutate: (name) => setPendingName(name),
    onSuccess: (fresh) => {
      queryClient.setQueryData<{ integrations: IntegrationStatus[] }>(['integrations-status'], (old) =>
        old
          ? { integrations: old.integrations.map((it) => (it.name === fresh.name ? fresh : it)) }
          : old,
      )
      toast('success', `${fresh.display_name} 检测完成`)
    },
    onError: (err) => {
      const status = (err as AxiosError)?.response?.status
      toast('error', status === 403 ? '重新检测需要管理员权限' : '检测失败，请稍后重试')
    },
    onSettled: () => setPendingName(null),
  })

  const items = data?.integrations ?? []

  return (
    <SectionCard
      title="集成状态"
      headerAction={
        <span className="text-[10px] text-muted-foreground">连通性预检 · 不触发真实短信/扣款/下单</span>
      }
    >
      {isLoading ? (
        <Skeleton variant="text" lines={4} />
      ) : items.length === 0 ? (
        <EmptyState title="暂无集成状态" description="集成预检接口未返回数据" />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-3">
          {items.map((it) => {
            const light = statusLight(it)
            const checking = pendingName === it.name && checkMut.isPending
            return (
              <div
                key={it.name}
                className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3 space-y-2"
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <Plug className="w-3.5 h-3.5 text-quant-gold shrink-0" />
                    <span className="text-sm text-foreground truncate" title={it.display_name}>
                      {it.display_name}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <span className={cn('w-2 h-2 rounded-full', light.color)} title={light.label} />
                    <Badge variant={statusVariant(it)}>{it.configured ? light.label : '未配置'}</Badge>
                  </div>
                </div>

                {it.notes && (
                  <div className="text-[11px] text-muted-foreground leading-relaxed break-words" title={it.notes}>
                    {it.notes}
                  </div>
                )}

                <div className="flex items-center justify-between">
                  <span className="text-[10px] text-muted-foreground">
                    {it.last_verified_at
                      ? `最后验证 ${new Date(it.last_verified_at).toLocaleString('zh-CN')}`
                      : '从未验证'}
                  </span>
                  <button
                    onClick={() => checkMut.mutate(it.name)}
                    disabled={checkMut.isPending}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium text-quant-gold border border-quant-gold/20 hover:bg-quant-gold/10 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                  >
                    {checking ? (
                      <Loader2 className="w-3 h-3 animate-spin" />
                    ) : (
                      <RefreshCw className="w-3 h-3" />
                    )}
                    重新检测
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </SectionCard>
  )
}
