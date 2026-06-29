import { PageHeader } from '@/components/ui/PageHeader'
import { StrategyEditor as StrategyEditorComponent } from '@/components/strategy/StrategyEditor'
import { Code } from 'lucide-react'

export function StrategyEditor() {
  return (
    <div className="h-full flex flex-col min-w-0">
      <div className="shrink-0 px-5 pt-5 pb-0">
        <PageHeader title="策略编辑器" subtitle="Python 策略脚本编辑" icon={<Code className="w-5 h-5" />} />
      </div>
      <div className="flex-1 overflow-hidden">
        <StrategyEditorComponent />
      </div>
    </div>
  )
}
