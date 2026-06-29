import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { LogViewer } from '@/components/system/LogViewer'
import { FileText } from 'lucide-react'

export function Logs() {
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="系统日志" subtitle="实时查看 Gateway / Bot / 策略运行日志" icon={<FileText className="w-5 h-5" />} />
        <SectionCard title="日志流" bodyClassName="p-0 overflow-hidden">
          <div className="h-[calc(100vh-220px)] min-h-[400px]">
            <LogViewer lines={200} className="h-full rounded-none border-0" />
          </div>
        </SectionCard>
      </div>
    </div>
  )
}
