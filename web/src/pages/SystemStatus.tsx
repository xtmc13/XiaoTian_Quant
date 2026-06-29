import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { DataTable } from '@/components/DataTable'
import { healthApi } from '@/lib/api'
import { Activity, Server, Database, Cpu, Radio } from 'lucide-react'
import { cn } from '@/lib/utils'

const COMPONENT_ICONS: Record<string, React.ReactNode> = {
  gateway: <Server className="w-4 h-4" />,
  rust_engine: <Cpu className="w-4 h-4" />,
  sqlite: <Database className="w-4 h-4" />,
  redis: <Radio className="w-4 h-4" />,
  ml_server: <Activity className="w-4 h-4" />,
}

function statusVariant(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  switch (status) {
    case 'healthy': return 'success'
    case 'degraded': return 'warning'
    case 'unhealthy': return 'error'
    default: return 'neutral'
  }
}

export function SystemStatus() {
  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => healthApi.health(),
    refetchInterval: 10000,
  })

  const { data: components, isLoading: componentsLoading } = useQuery({
    queryKey: ['health-components'],
    queryFn: () => healthApi.components(),
    refetchInterval: 10000,
  })

  const isLoading = healthLoading || componentsLoading

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="系统状态" subtitle="Gateway / Rust / 数据库 / ML 健康监控" icon={<Activity className="w-5 h-5" />} />

        {isLoading ? (
          <SectionCard title="基础信息"><Skeleton variant="text" lines={4} /></SectionCard>
        ) : health ? (
          <SectionCard title="基础信息">
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                <div className="text-[10px] text-muted-foreground">状态</div>
                <Badge variant={health.status === 'ok' || health.status === 'healthy' ? 'success' : 'warning'}>{health.status}</Badge>
              </div>
              <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                <div className="text-[10px] text-muted-foreground">版本</div>
                <div className="text-sm text-white font-mono">{health.version}</div>
              </div>
              <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                <div className="text-[10px] text-muted-foreground">运行时间</div>
                <div className="text-sm text-white font-mono">{health.uptime}</div>
              </div>
              <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                <div className="text-[10px] text-muted-foreground">日志级别</div>
                <div className="text-sm text-white font-mono">{health.log_level}</div>
              </div>
            </div>
          </SectionCard>
        ) : null}

        <SectionCard title="组件健康">
          {components && components.length > 0 ? (
            <DataTable
              data={components}
              keyExtractor={(item, i) => `${item.name}-${i}`}
              columns={[
                {
                  key: 'name',
                  title: '组件',
                  render: (item) => (
                    <div className="flex items-center gap-2">
                      <span className="text-quant-gold">{COMPONENT_ICONS[item.name.toLowerCase()] || <Server className="w-4 h-4" />}</span>
                      <span className="text-sm text-white">{item.name}</span>
                    </div>
                  ),
                },
                {
                  key: 'status',
                  title: '状态',
                  render: (item) => <Badge variant={statusVariant(item.status)}>{item.status}</Badge>,
                },
                {
                  key: 'message',
                  title: '信息',
                  render: (item) => <span className={cn('text-xs', item.status === 'healthy' ? 'text-muted-foreground' : 'text-quant-orange')}>{item.message || '-'}</span>,
                },
                {
                  key: 'last_check',
                  title: '最后检查',
                  render: (item) => <span className="text-xs text-muted-foreground">{item.last_check ? new Date(item.last_check).toLocaleString('zh-CN') : '-'}</span>,
                },
              ]}
            />
          ) : (
            <EmptyState title="暂无组件状态" description="健康检查接口未返回组件数据" />
          )}
        </SectionCard>
      </div>
    </div>
  )
}
