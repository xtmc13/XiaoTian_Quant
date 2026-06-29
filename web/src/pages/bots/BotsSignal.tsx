import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { SignalExecutorPanel } from '@/components/bots/SignalExecutorPanel'
import { Radio } from 'lucide-react'

export function BotsSignal() {
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title="信号机器人"
          subtitle="接收外部信号并按阶梯止盈/止损自动执行"
          icon={<Radio className="w-5 h-5" />}
        />
        <SectionCard title="信号执行器" className="w-full">
          <SignalExecutorPanel />
        </SectionCard>
      </div>
    </div>
  )
}
