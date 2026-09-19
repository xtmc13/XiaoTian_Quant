import { PageHeader } from '@/components/ui/PageHeader'
import { Triangle } from 'lucide-react'
import { useI18n } from '@/i18n'
import { TriangularArbitragePanel } from './components/TriangularArbitragePanel'

export function ArbitrageTriangular() {
  const { t } = useI18n()
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title={t('nav.arbitrage-triangular')} subtitle={t('arb.tri.subtitle')} icon={<Triangle className="w-5 h-5" />} />
        <TriangularArbitragePanel />
      </div>
    </div>
  )
}
