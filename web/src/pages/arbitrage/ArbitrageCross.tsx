import { PageHeader } from '@/components/ui/PageHeader'
import { ArrowLeftRight } from 'lucide-react'
import { useI18n } from '@/i18n'
import { CrossArbitragePanel } from './components/CrossArbitragePanel'

export function ArbitrageCross() {
  const { t } = useI18n()
  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader title={t('nav.arbitrage-cross')} subtitle={t('arb.cross.subtitle')} icon={<ArrowLeftRight className="w-5 h-5" />} />
        <CrossArbitragePanel />
      </div>
    </div>
  )
}
