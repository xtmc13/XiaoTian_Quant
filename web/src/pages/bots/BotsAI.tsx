import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { AIRobotPanel } from '@/components/bots/AIRobotPanel'
import { BrainCircuit } from 'lucide-react'

export function BotsAI() {
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title="AI 机器人"
          subtitle="AI 驱动决策，智能过滤波动市场，带置信度评估"
          icon={<BrainCircuit className="w-5 h-5" />}
        />
        <SectionCard title="AI 配置与信号" className="w-full">
          <AIRobotPanel />
        </SectionCard>
      </div>
    </div>
  )
}
