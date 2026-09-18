import { PageHeader } from '@/components/ui/PageHeader'
import { Triangle } from 'lucide-react'
import { TriangularArbitragePanel } from './components/TriangularArbitragePanel'

export function ArbitrageTriangular() {
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="币种套利" subtitle="同交易所多币种循环路径套利" icon={<Triangle className="w-5 h-5" />} />
        <TriangularArbitragePanel />
      </div>
    </div>
  )
}
