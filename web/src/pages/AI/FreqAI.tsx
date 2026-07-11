import { useState, useEffect, useCallback } from 'react'
import { Cpu, Zap } from 'lucide-react'
import { toast } from '@/lib/useToast'
import { mlApi } from '@/lib/api'
import type { MLModelInfo, RLModelInfo } from '@/types'

import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { MLPanel } from '../AI/components/MLPanel'

export function FreqAI() {
  const [mlModels, setMlModels] = useState<MLModelInfo[]>([])
  const [rlModels, setRlModels] = useState<RLModelInfo[]>([])
  const [selectedSymbol, setSelectedSymbol] = useState<string | undefined>(undefined)

  const loadMlModels = useCallback(async () => {
    try {
      const data = await mlApi.list()
      setMlModels(data || [])
    } catch (e: unknown) {
      const err = e instanceof Error ? e : new Error(String(e))
      toast('error', 'Failed to load ML models: ' + err.message)
    }
  }, [])

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
    loadMlModels()
    loadRlModels()
  }, [loadMlModels, loadRlModels])

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title="FreqAI" subtitle="机器学习模型训练与管理" icon={<Cpu className="w-5 h-5" />} />

        <SectionCard title="ML 模型面板">
          <MLPanel
            selectedSymbol={selectedSymbol}
            mlModels={mlModels}
            loadMlModels={loadMlModels}
            rlModels={rlModels}
            loadRlModels={loadRlModels}
          />
        </SectionCard>
      </div>
    </div>
  )
}
