import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { cn, formatCurrency } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import { Button } from '@/components/ui/Button'
import {
  Plus,
  Bot,
  Activity,
  PauseCircle,
  TrendingUp,
  TrendingDown,
  FlaskConical,
} from 'lucide-react'
import { useStrategyData } from '@/hooks/useStrategyData'
import { StrategyList } from '@/components/strategy/StrategyList'
import { StrategyDetailPanel } from '@/components/strategy/StrategyDetailPanel'
import { StrategyCreateModal } from '@/components/strategy/StrategyCreateModal'
import { StrategyCreatePanel } from '@/components/strategy/StrategyCreatePanel'
import type { StrategyItem } from '@/types'

/**
 * 策略管理独立页（/strategies，一级菜单）。
 * 由 Strategy.tsx 的"策略管理"标签页迁移而来：列表 + 详情 + 创建/编辑 +
 * 批量操作 + 启动失败红条 + RuntimePanel 实时运行面板，数据经
 * useStrategyData（已带 kind=strategy，与策略机器人互斥隔离）。
 */
export function Strategies() {
  const { strategies, isLoading, stop, delete: del, startAsync, batchStart, batchStop } = useStrategyData()
  const navigate = useNavigate()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [creatingType, setCreatingType] = useState<string | null>(null)
  const [editingStrategy, setEditingStrategy] = useState<StrategyItem | null>(null)
  // 启动失败持久化反馈（会话级）：strategyId → 后端错误信息。
  const [startErrors, setStartErrors] = useState<Record<string, string>>({})
  // 空状态模板预填。
  const [templatePreset, setTemplatePreset] = useState<{
    strategyType: string
    config?: Record<string, unknown>
  } | null>(null)

  const selected = strategies.find((s) => s.id === selectedId) || null

  const running = strategies.filter((s) => s.status === 'running').length
  const stopped = strategies.filter((s) => s.status === 'stopped').length
  const totalPnl = strategies.reduce((sum, s) => sum + (s.total_pnl || 0), 0)

  const handleCloseCreate = () => {
    setShowCreate(false)
    setCreatingType(null)
    setEditingStrategy(null)
    setTemplatePreset(null)
  }

  const clearStartError = (id: string) =>
    setStartErrors((prev) => {
      if (!(id in prev)) return prev
      const next = { ...prev }
      delete next[id]
      return next
    })

  const handleStart = async (id: string) => {
    try {
      await startAsync(id)
      clearStartError(id)
    } catch (e) {
      setStartErrors((prev) => ({ ...prev, [id]: e instanceof Error ? e.message : String(e) }))
    }
  }

  return (
    <div className="h-full overflow-y-auto p-4 sm:p-5">
      <div className="mx-auto max-w-[1600px] space-y-4">
        {/* 页头 */}
        <PageHeader
          title="策略管理"
          subtitle="创建、配置并运行你的量化策略（模拟盘），运行中可查看实时持仓与加仓阶梯"
          icon={<Bot className="w-5 h-5" />}
          actions={
            <Button
              variant="primary"
              size="sm"
              leftIcon={<Plus className="w-3 h-3" />}
              onClick={() => {
                setEditingStrategy(null)
                setShowCreate(true)
              }}
            >
              新建策略
            </Button>
          }
        />

        {/* KPI 行 */}
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
          <KPICard
            icon={<Activity className="h-4 w-4 text-[#52c41a]" />}
            label="运行中"
            value={String(running)}
            subValue={`共 ${strategies.length} 个`}
          />
          <KPICard
            icon={<PauseCircle className="h-4 w-4 text-[#faad14]" />}
            label="已停止"
            value={String(stopped)}
          />
          <KPICard
            icon={
              totalPnl >= 0 ? (
                <TrendingUp className="h-4 w-4 text-[#52c41a]" />
              ) : (
                <TrendingDown className="h-4 w-4 text-[#f5222d]" />
              )
            }
            label="累计盈亏"
            value={`${totalPnl >= 0 ? '+' : ''}${formatCurrency(totalPnl)}`}
            trend={totalPnl >= 0 ? 'up' : 'down'}
            variant={totalPnl >= 0 ? 'success' : 'error'}
            primary
          />
          <KPICard
            icon={<FlaskConical className="h-4 w-4 text-[#1890ff]" />}
            label="策略实验室"
            value="更多工具"
            onClick={() => navigate('/strategy')}
          />
        </div>

        {/* 左列表 + 右详情 */}
        <div className="h-full flex">
          <StrategyList
            strategies={strategies}
            isLoading={isLoading}
            selectedId={selectedId}
            onSelect={setSelectedId}
            onStart={(id) => {
              void handleStart(id)
            }}
            onStop={stop}
            onEdit={(s) => {
              setEditingStrategy(s)
              setShowCreate(true)
            }}
            onDelete={(id) => {
              clearStartError(id)
              del(id)
            }}
            onCreate={() => {
              setEditingStrategy(null)
              setShowCreate(true)
            }}
            onCreateType={(type) => {
              setEditingStrategy(null)
              setCreatingType(type)
              setSelectedId(null)
            }}
            startErrors={startErrors}
            onDismissStartError={clearStartError}
            onBatchStart={(ids) => {
              void batchStart(ids)
            }}
            onBatchStop={(ids) => {
              void batchStop(ids)
            }}
            onCreateTemplate={(preset) => {
              setTemplatePreset(preset)
              setEditingStrategy(null)
              setCreatingType(null)
              setShowCreate(true)
            }}
          />

          <div className={cn('flex-1', creatingType ? 'overflow-hidden' : 'overflow-y-auto p-4 sm:p-6')}>
            {creatingType ? (
              <StrategyCreatePanel strategyType={creatingType} onClose={handleCloseCreate} onSaved={handleCloseCreate} />
            ) : !selected ? (
              <div className="h-full flex items-center justify-center">
                <EmptyState
                  icon={<Bot className="w-6 h-6" />}
                  title="选择或创建一个策略"
                  description="从左侧列表选择策略查看详情，或点击创建按钮新建策略"
                  actionLabel="创建策略"
                  onAction={() => {
                    setEditingStrategy(null)
                    setShowCreate(true)
                  }}
                />
              </div>
            ) : (
              <StrategyDetailPanel
                strategy={selected}
                onStart={() => {
                  void handleStart(selected.id)
                }}
                onStop={() => stop(selected.id)}
                onEdit={() => {
                  setEditingStrategy(selected)
                  setShowCreate(true)
                }}
                onDelete={() => {
                  if (confirm(`删除策略 "${selected.name}"？`)) {
                    clearStartError(selected.id)
                    del(selected.id)
                  }
                }}
                startError={startErrors[selected.id] ?? null}
                onDismissStartError={() => clearStartError(selected.id)}
              />
            )}
          </div>

          {showCreate && (
            <StrategyCreateModal
              editing={editingStrategy}
              defaultStrategyType={templatePreset?.strategyType}
              defaultConfig={templatePreset?.config}
              onClose={handleCloseCreate}
              onSaved={handleCloseCreate}
            />
          )}
        </div>
      </div>
    </div>
  )
}

export default Strategies
