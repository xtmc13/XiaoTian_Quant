import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'
import { DataTable } from '@/components/DataTable'
import { healthApi, alertApi, type AlertEvent } from '@/lib/api'
import { ExchangeHealthPanel } from '@/components/ExchangeHealthPanel'
import { IntegrationStatusPanel } from '@/components/IntegrationStatusPanel'
import { Activity, Server, Database, Cpu, Radio, Bell } from 'lucide-react'
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

function severityVariant(severity: string): 'success' | 'warning' | 'error' | 'neutral' {
  switch (severity) {
    case 'critical': return 'error'
    case 'warning': return 'warning'
    default: return 'neutral'
  }
}

function fmtAlertTime(ms: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleString('zh-CN')
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

  const { data: activeAlerts } = useQuery({
    queryKey: ['alerts-active'],
    queryFn: () => alertApi.active(),
    refetchInterval: 15000,
  })

  const { data: alertHistory } = useQuery({
    queryKey: ['alerts-history'],
    queryFn: () => alertApi.history(20),
    refetchInterval: 30000,
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
                <div className="text-sm text-foreground font-mono">{health.version}</div>
              </div>
              <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                <div className="text-[10px] text-muted-foreground">运行时间</div>
                <div className="text-sm text-foreground font-mono">{health.uptime}</div>
              </div>
              <div className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                <div className="text-[10px] text-muted-foreground">日志级别</div>
                <div className="text-sm text-foreground font-mono">{health.log_level}</div>
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
                      <span className="text-sm text-foreground">{item.name}</span>
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

        <SectionCard title={
          <span className="flex items-center gap-2">
            <Bell className="w-4 h-4 text-quant-gold" />
            告警
            {activeAlerts && activeAlerts.alerts.length > 0 && (
              <Badge variant="error">{activeAlerts.alerts.length} 活跃</Badge>
            )}
          </span>
        }>
          {/* 活跃告警：Prometheus 评估 → Alertmanager → /api/alerts/webhook 落库 */}
          {activeAlerts && activeAlerts.alerts.length > 0 ? (
            <div className="space-y-2 mb-4">
              {activeAlerts.alerts.map((a: AlertEvent) => (
                <div key={a.fingerprint} className="rounded-lg border border-quant-border bg-quant-bg-secondary p-3">
                  <div className="flex items-center gap-2 flex-wrap">
                    <Badge variant={severityVariant(a.severity)}>{a.severity}</Badge>
                    <span className="text-sm font-medium text-foreground">{a.alertname}</span>
                    <span className="text-[10px] text-muted-foreground">始于 {fmtAlertTime(a.first_seen)}</span>
                  </div>
                  {a.summary && <div className="mt-1 text-xs text-muted-foreground">{a.summary}</div>}
                </div>
              ))}
            </div>
          ) : (
            <div className="mb-4">
              <EmptyState title="当前无活跃告警" description="Prometheus 告警经 Alertmanager 推送到网关后在此显示" />
            </div>
          )}

          {/* 最近告警历史（含已恢复）*/}
          <div className="text-xs text-muted-foreground mb-2">最近告警历史</div>
          {alertHistory && alertHistory.alerts.length > 0 ? (
            <DataTable
              data={alertHistory.alerts}
              keyExtractor={(item) => item.fingerprint}
              columns={[
                {
                  key: 'last_seen',
                  title: '时间',
                  render: (item) => <span className="text-xs text-muted-foreground">{fmtAlertTime(item.last_seen)}</span>,
                },
                {
                  key: 'alertname',
                  title: '告警',
                  render: (item) => <span className="text-sm text-foreground">{item.alertname}</span>,
                },
                {
                  key: 'severity',
                  title: '级别',
                  render: (item) => <Badge variant={severityVariant(item.severity)}>{item.severity}</Badge>,
                },
                {
                  key: 'status',
                  title: '状态',
                  render: (item) => (
                    <Badge variant={item.status === 'firing' ? 'warning' : 'success'}>
                      {item.status === 'firing' ? 'firing' : 'resolved'}
                    </Badge>
                  ),
                },
                {
                  key: 'summary',
                  title: '摘要',
                  render: (item) => <span className="text-xs text-muted-foreground">{item.summary || '-'}</span>,
                },
              ]}
            />
          ) : (
            <EmptyState title="暂无告警历史" description="尚未收到 Alertmanager 推送（检查 observability profile 是否启动）" />
          )}
        </SectionCard>

        <ExchangeHealthPanel />

        <IntegrationStatusPanel />
      </div>
    </div>
  )
}
