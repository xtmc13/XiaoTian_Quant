import { PageHeader } from '@/components/ui/PageHeader'
import { ArrowLeftRight } from 'lucide-react'
import { CrossArbitragePanel } from './components/CrossArbitragePanel'

export function ArbitrageCross() {
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="跨所套利" subtitle="跨交易所套利监控与执行" icon={<ArrowLeftRight className="w-5 h-5" />} />
        <CrossArbitragePanel />
      </div>
    </div>
  )
}
