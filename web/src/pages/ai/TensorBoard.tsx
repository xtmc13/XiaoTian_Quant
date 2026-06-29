import { LineChart } from 'lucide-react'

import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { TensorBoardPanel } from '../AI/components/TensorBoardPanel'

export function TensorBoard() {
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="TensorBoard" subtitle="训练过程可视化" icon={<LineChart className="w-5 h-5" />} />

        <SectionCard title="训练指标">
          <TensorBoardPanel />
        </SectionCard>
      </div>
    </div>
  )
}
