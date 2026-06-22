import { useState, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  BrainCircuit, Store, Radio, Bot, BarChart3, Plus
} from 'lucide-react'
import { PageHeader } from '@/components/ui/PageHeader'
import { Button } from '@/components/ui/Button'
import { cn } from '@/lib/utils'
import { useAIBotData } from '@/hooks/useAIBotData'
import { useAIBotRealtime } from '@/hooks/useAIBotRealtime'
import { useAuthStore } from '@/stores/authStore'
import { AIBotCatalog } from '@/components/ai-bots/AIBotCatalog'
import { AIBotProviderMarket } from '@/components/ai-bots/AIBotProviderMarket'
import { AIBotManagement } from '@/components/ai-bots/AIBotManagement'
import { AIBotAnalytics } from '@/components/ai-bots/AIBotAnalytics'
import { AIBotCreateWizard } from '@/components/ai-bots/AIBotCreateWizard'
import { AIBotSubscriptions } from '@/components/ai-bots/AIBotSubscriptions'
import type { AIBotCatalogItem, AIBotInstance } from '@/types'

type TabKey = 'marketplace' | 'providers' | 'mybots' | 'analytics' | 'subscriptions'

const TABS: { key: TabKey; label: string; icon: React.ReactNode }[] = [
  { key: 'marketplace', label: '机器人市场', icon: <Store className="w-4 h-4" /> },
  { key: 'providers', label: '信号源市场', icon: <Radio className="w-4 h-4" /> },
  { key: 'mybots', label: '我的机器人', icon: <Bot className="w-4 h-4" /> },
  { key: 'analytics', label: '数据分析', icon: <BarChart3 className="w-4 h-4" /> },
  { key: 'subscriptions', label: '订阅管理', icon: <BrainCircuit className="w-4 h-4" /> },
]

export default function AIBots() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [activeTab, setActiveTab] = useState<TabKey>((searchParams.get('tab') as TabKey) || 'marketplace')
  const [wizardOpen, setWizardOpen] = useState(false)
  const [wizardCatalog, setWizardCatalog] = useState<AIBotCatalogItem | null>(null)
  const [editInstance, setEditInstance] = useState<AIBotInstance | undefined>(undefined)
  const [analyticsBotId, setAnalyticsBotId] = useState<string | undefined>(undefined)

  const { user } = useAuthStore()
  useAIBotRealtime(user ? Number(user.id) : undefined)

  const {
    instances,
    catalog,
    subscriptions,
    isLoading,
    kpi,
    actionLoadingId,
    startBot,
    stopBot,
    pauseBot,
    resumeBot,
    deleteBot,
    cloneBot,
    updateBot,
    batchStart,
    batchStop,
    batchDelete,
  } = useAIBotData()

  const handleTabChange = (tab: TabKey) => {
    setActiveTab(tab)
    setSearchParams({ tab })
  }

  const handleDeploy = (bot: AIBotCatalogItem) => {
    setWizardCatalog(bot)
    setEditInstance(undefined)
    setWizardOpen(true)
    setActiveTab('mybots')
  }

  const handleCreate = () => {
    setWizardCatalog(null)
    setEditInstance(undefined)
    setWizardOpen(true)
  }

  const handleEdit = (bot: AIBotInstance) => {
    setWizardCatalog(bot.catalog_id ? catalog.find((c) => c.id === bot.catalog_id) || null : null)
    setEditInstance(bot)
    setWizardOpen(true)
  }

  const handleWizardClose = () => {
    setWizardOpen(false)
    setWizardCatalog(null)
    setEditInstance(undefined)
  }

  const handleViewAnalytics = (bot: AIBotInstance) => {
    setAnalyticsBotId(bot.id)
    setActiveTab('analytics')
  }

  const isBatchLoading = batchStart.isPending || batchStop.isPending || batchDelete.isPending

  return (
    <div className="h-full overflow-y-auto p-5">
      <PageHeader
        title="AI Bots"
        subtitle="CryptoRobotics 风格的智能交易机器人市场与管理"
        actions={
          activeTab === 'mybots' && (
            <Button size="sm" leftIcon={<Plus className="w-4 h-4" />} onClick={handleCreate}>
              创建机器人
            </Button>
          )
        }
      />

      <div className="flex border-b border-[#1c1c1c] mb-5">
        {TABS.map((t) => (
          <button
            key={t.key}
            onClick={() => handleTabChange(t.key)}
            className={cn(
              'flex items-center gap-1.5 px-4 py-2.5 text-xs font-medium transition-colors relative',
              activeTab === t.key ? 'text-[#1890ff]' : 'text-[#888] hover:text-[#e0e0e0]'
            )}
          >
            {t.icon}
            {t.label}
            {activeTab === t.key && <span className="absolute bottom-0 left-0 right-0 h-0.5 bg-[#1890ff]" />}
          </button>
        ))}
      </div>

      <div className="space-y-5">
        {activeTab === 'marketplace' && <AIBotCatalog onDeploy={handleDeploy} />}
        {activeTab === 'providers' && <AIBotProviderMarket />}
        {activeTab === 'mybots' && (
          <AIBotManagement
            instances={instances}
            isLoading={isLoading}
            actionLoadingId={actionLoadingId}
            kpi={kpi}
            onStart={startBot}
            onStop={stopBot}
            onPause={pauseBot}
            onResume={resumeBot}
            onClone={cloneBot}
            onDelete={deleteBot}
            onEdit={handleEdit}
            onViewAnalytics={handleViewAnalytics}
            onCreate={handleCreate}
            onBatchStart={(ids) => batchStart.mutate(ids)}
            onBatchStop={(ids) => batchStop.mutate(ids)}
            onBatchDelete={(ids) => batchDelete.mutate(ids)}
            batchLoading={isBatchLoading}
          />
        )}
        {activeTab === 'analytics' && (
          <AIBotAnalytics
            instances={instances}
            selectedBotId={analyticsBotId}
            onSelectBot={setAnalyticsBotId}
          />
        )}
        {activeTab === 'subscriptions' && (
          <AIBotSubscriptions subscriptions={subscriptions} instances={instances} />
        )}
      </div>

      <AIBotCreateWizard
        open={wizardOpen}
        initialCatalog={wizardCatalog}
        editInstance={editInstance}
        onClose={handleWizardClose}
        onCreated={() => {
          handleWizardClose()
          setActiveTab('mybots')
        }}
        onUpdated={() => {
          handleWizardClose()
          setActiveTab('mybots')
        }}
      />
    </div>
  )
}
