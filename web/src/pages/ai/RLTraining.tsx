import { useState, useEffect, useCallback } from 'react'
import { Zap } from 'lucide-react'
import { toast } from '@/lib/useToast'
import type { RLModelInfo } from '@/types'

import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { RLPanel } from '../AI/components/RLPanel'

export function RLTraining() {
  const [rlModels, setRlModels] = useState<RLModelInfo[]>([])
  const [selectedSymbol, setSelectedSymbol] = useState<string | undefined>(undefined)

  const loadRlModels = useCallback(async () => {
    try {
      const { rlApi } = await import('@/lib/api')
      const data = await rlApi.list()
      setRlModels(data || [])
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', 'Failed to load RL models: ' + err.message)
    }
  }, [])

  useEffect(() => {
    loadRlModels()
  }, [loadRlModels])

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="RL 强化学习" subtitle="强化学习训练与评估" icon={<Zap className="w-5 h-5" />} />

        <SectionCard title="RL 模型面板">
          <RLPanel selectedSymbol={selectedSymbol} rlModels={rlModels} loadRlModels={loadRlModels} />
        </SectionCard>
      </div>
    </div>
  )
}
