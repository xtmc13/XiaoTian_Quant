import { useState } from 'react'
import { PageHeader } from '@/components/ui/PageHeader'
import { SectionCard } from '@/components/ui/SectionCard'
import { AIRobotPanel } from '@/components/bots/AIRobotPanel'
import { MarketBoard } from '@/components/market/MarketBoard'
import { MyListings } from '@/components/market/MyListings'
import { BrainCircuit, Store, ClipboardList, Settings2 } from 'lucide-react'
import { useI18n } from '@/i18n'
import { cn } from '@/lib/utils'

type TabKey = 'config' | 'market' | 'my-listings'

export function BotsAI() {
  const { t } = useI18n()
  const [tab, setTab] = useState<TabKey>('market')

  const TABS: { key: TabKey; label: string; icon: React.ReactNode }[] = [
    { key: 'market', label: t('market.board.tab'), icon: <Store className="h-3 w-3" /> },
    { key: 'my-listings', label: t('market.my.tab'), icon: <ClipboardList className="h-3 w-3" /> },
    { key: 'config', label: t('market.page.tabConfig'), icon: <Settings2 className="h-3 w-3" /> },
  ]

  return (
    <div className="h-full overflow-y-auto p-5">
      <div className="mx-auto max-w-[1600px] space-y-5">
        <PageHeader
          title={t('market.page.pageTitle')}
          subtitle={t('market.page.pageSubtitle')}
          icon={<BrainCircuit className="w-5 h-5" />}
        />
        <div className="flex gap-1 bg-quant-bg-secondary rounded-lg p-0.5 w-fit">
          {TABS.map((item) => (
            <button
              key={item.key}
              onClick={() => setTab(item.key)}
              className={cn(
                'flex items-center gap-1 px-3 py-1.5 rounded text-xs font-medium transition-colors',
                tab === item.key ? 'bg-quant-gold text-white' : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {item.icon}
              {item.label}
            </button>
          ))}
        </div>
        {tab === 'market' && (
          <SectionCard title={t('market.board.tab')} className="w-full">
            <MarketBoard />
          </SectionCard>
        )}
        {tab === 'my-listings' && (
          <SectionCard title={t('market.my.tab')} className="w-full">
            <MyListings />
          </SectionCard>
        )}
        {tab === 'config' && (
          <SectionCard title={t('market.page.tabConfig')} className="w-full">
            <AIRobotPanel />
          </SectionCard>
        )}
      </div>
    </div>
  )
}
