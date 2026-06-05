import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { protectionApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import {
  Shield,
  ShieldAlert,
  ShieldCheck,
  Clock,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  RefreshCw,
  Plus,
  Trash2,
  Activity,
  Lock,
  Unlock,
  TrendingDown,
  Timer,
  Gauge,
  Settings2,
} from 'lucide-react'

/* ── Types ── */
interface ProtectionStatus {
  global_blocked: boolean
  global_reason?: string
  global_resume_in?: string
  pair_blocks: Record<string, {
    reason: string
    resume_in: string
    permanent: boolean
  }>
}

interface ProtectionConfigItem {
  name: string
  params: Record<string, any>
}

/* ── Protection Templates ── */
interface FieldDef {
  key: string
  label: string
  type: 'number' | 'select'
  min?: number
  max?: number
  step?: number
  options?: string[]
}

interface ProtectionTemplate {
  name: string
  label: string
  description: string
  icon: React.ReactNode
  defaultParams: Record<string, any>
  fields: FieldDef[]
}

const PROTECTION_TEMPLATES: ProtectionTemplate[] = [
  {
    name: 'CooldownPeriod',
    label: '交易冷却期',
    description: '平仓后等待 N 根 K线再开仓，避免过度交易',
    icon: <Timer className="w-4 h-4" />,
    defaultParams: { stop_duration_candles: 5, timeframe: '1h' },
    fields: [
      { key: 'stop_duration_candles', label: '冷却 K线数', type: 'number', min: 1, max: 50 },
      { key: 'timeframe', label: '时间周期', type: 'select', options: ['1m', '5m', '15m', '30m', '1h', '4h', '1d'] },
    ],
  },
  {
    name: 'StoplossGuard',
    label: '止损保护',
    description: 'N 次止损后暂停交易，防止连续亏损',
    icon: <ShieldAlert className="w-4 h-4" />,
    defaultParams: { max_stoplosses: 3, lookback_minutes: 60 },
    fields: [
      { key: 'max_stoplosses', label: '最大止损次数', type: 'number', min: 1, max: 20 },
      { key: 'lookback_minutes', label: '观察窗口 (分钟)', type: 'number', min: 5, max: 1440 },
    ],
  },
  {
    name: 'MaxDrawdown',
    label: '最大回撤保护',
    description: '账户回撤超过阈值时暂停全部交易',
    icon: <TrendingDown className="w-4 h-4" />,
    defaultParams: { max_drawdown_pct: 10.0 },
    fields: [
      { key: 'max_drawdown_pct', label: '最大回撤 (%)', type: 'number', min: 1, max: 50, step: 0.5 },
    ],
  },
  {
    name: 'LowProfitPairs',
    label: '低收益交易对保护',
    description: '交易对收益低于阈值时暂停该对交易',
    icon: <Activity className="w-4 h-4" />,
    defaultParams: { min_profit_pct: 1.0, lookback_trades: 10 },
    fields: [
      { key: 'min_profit_pct', label: '最低收益 (%)', type: 'number', min: 0, max: 10, step: 0.1 },
      { key: 'lookback_trades', label: '观察交易数', type: 'number', min: 3, max: 50 },
    ],
  },
]

/* ── Page ── */
export function RiskControl() {
  const queryClient = useQueryClient()
  const [activeProtections, setActiveProtections] = useState<ProtectionConfigItem[]>([])
  const [showAddForm, setShowAddForm] = useState(false)
  const [selectedTemplate, setSelectedTemplate] = useState<string>('')

  // Queries
  const { data: status, isLoading: statusLoading } = useQuery({
    queryKey: ['protection-status'],
    queryFn: async () => {
      const res = await protectionApi.status()
      return res as ProtectionStatus
    },
    refetchInterval: 10000,
  })

  // Mutations
  const configMutation = useMutation({
    mutationFn: (data: { protections: ProtectionConfigItem[] }) => protectionApi.config(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['protection-status'] })
      setShowAddForm(false)
    },
  })

  const resetMutation = useMutation({
    mutationFn: (params: { scope?: 'global' | 'pair' | 'all'; symbol?: string }) =>
      protectionApi.reset(params.scope, params.symbol),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['protection-status'] }),
  })

  const handleAddProtection = useCallback((templateName: string) => {
    const template = PROTECTION_TEMPLATES.find((t) => t.name === templateName)
    if (!template) return
    setActiveProtections((prev) => [
      ...prev,
      { name: template.name, params: { ...template.defaultParams } },
    ])
    setSelectedTemplate('')
  }, [])

  const handleUpdateParam = useCallback((index: number, key: string, value: any) => {
    setActiveProtections((prev) => {
      const next = [...prev]
      next[index] = { ...next[index], params: { ...next[index].params, [key]: value } }
      return next
    })
  }, [])

  const handleRemoveProtection = useCallback((index: number) => {
    setActiveProtections((prev) => prev.filter((_, i) => i !== index))
  }, [])

  const handleSave = useCallback(() => {
    configMutation.mutate({ protections: activeProtections })
  }, [activeProtections, configMutation])

  const isGloballyBlocked = status?.global_blocked ?? false
  const pairBlockCount = status?.pair_blocks ? Object.keys(status.pair_blocks).length : 0

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="风控中心"
          subtitle="配置交易保护机制，防止过度交易和重大亏损"
          actions={<Shield className="w-6 h-6 text-quant-gold" />}
        />

        {/* Status Overview */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="全局状态"
            value={isGloballyBlocked ? '已阻断' : '正常'}
            icon={isGloballyBlocked ? <ShieldAlert className="w-4 h-4 text-red-400" /> : <ShieldCheck className="w-4 h-4 text-green-400" />}
            subValue={isGloballyBlocked ? (status?.global_reason || '交易暂停') : '可交易'}
            trend={isGloballyBlocked ? 'down' : 'up'}
          />
          <KPICard
            label="交易对阻断"
            value={pairBlockCount}
            icon={<Lock className="w-4 h-4 text-quant-gold" />}
            subValue="被暂停"
            trend={pairBlockCount === 0 ? 'up' : 'down'}
          />
          <KPICard
            label="保护规则"
            value={activeProtections.length}
            icon={<Settings2 className="w-4 h-4 text-quant-gold" />}
            subValue="已配置"
            trend="up"
          />
          <KPICard
            label="冷却中"
            value={status?.global_resume_in || '-'}
            icon={<Clock className="w-4 h-4 text-quant-gold" />}
            subValue="剩余时间"
            trend="neutral"
          />
        </div>

        {/* Active Blocks */}
        {(isGloballyBlocked || pairBlockCount > 0) && (
          <SectionCard
            title={<>当前阻断状态 <AlertTriangle className="w-4 h-4 inline ml-1 text-red-400" /></>}
            className="border-red-500/20"
          >
            <div className="space-y-2">
              {isGloballyBlocked && (
                <div className="flex items-center justify-between p-3 rounded-md bg-red-500/10 border border-red-500/20">
                  <div className="flex items-center gap-2">
                    <ShieldAlert className="w-4 h-4 text-red-400" />
                    <span className="text-sm font-medium text-red-400">全局交易阻断</span>
                    <span className="text-xs text-muted-foreground">{status?.global_reason}</span>
                  </div>
                  <button
                    onClick={() => resetMutation.mutate({ scope: 'global' })}
                    disabled={resetMutation.isPending}
                    className="flex items-center gap-1 px-3 py-1.5 rounded-md bg-red-500/20 text-red-400 text-xs font-medium hover:bg-red-500/30 transition-colors"
                  >
                    <Unlock className="w-3 h-3" />
                    解除阻断
                  </button>
                </div>
              )}
              {status?.pair_blocks && Object.entries(status.pair_blocks).map(([pair, info]) => (
                <div key={pair} className="flex items-center justify-between p-3 rounded-md bg-quant-bg-secondary">
                  <div className="flex items-center gap-2">
                    <Lock className="w-4 h-4 text-quant-gold" />
                    <span className="text-sm font-medium">{pair}</span>
                    <span className="text-xs text-muted-foreground">{info.reason}</span>
                    {info.permanent && (
                      <span className="px-1.5 py-0.5 rounded text-[10px] font-medium bg-red-500/10 text-red-400">永久</span>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">{info.resume_in}</span>
                    <button
                      onClick={() => resetMutation.mutate({ scope: 'pair', symbol: pair })}
                      disabled={resetMutation.isPending}
                      className="flex items-center gap-1 px-3 py-1.5 rounded-md bg-quant-gold/10 text-quant-gold text-xs font-medium hover:bg-quant-gold/20 transition-colors"
                    >
                      <Unlock className="w-3 h-3" />
                      解除
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </SectionCard>
        )}

        {/* Protection Configuration */}
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-foreground">保护规则配置</h2>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setActiveProtections([])}
              className="flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-white/5 transition-colors"
            >
              <Trash2 className="w-3.5 h-3.5" />
              清空
            </button>
            <button
              onClick={() => setShowAddForm(!showAddForm)}
              className={cn(
                'flex items-center gap-1.5 px-3 py-2 rounded-md text-xs font-medium transition-colors',
                showAddForm
                  ? 'bg-quant-gold/10 text-quant-gold'
                  : 'bg-quant-gold text-white hover:bg-quant-gold/90'
              )}
            >
              <Plus className="w-3.5 h-3.5" />
              {showAddForm ? '取消' : '添加规则'}
            </button>
          </div>
        </div>

        {/* Add Form */}
        {showAddForm && (
          <SectionCard title={<>选择保护类型 <Shield className="w-4 h-4 inline ml-1 text-quant-gold" /></>}>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {PROTECTION_TEMPLATES.map((template) => (
                <button
                  key={template.name}
                  onClick={() => handleAddProtection(template.name)}
                  className="flex items-start gap-3 p-3 rounded-md bg-quant-bg-secondary hover:bg-white/5 transition-colors text-left"
                >
                  <div className="w-8 h-8 rounded-md bg-quant-gold/10 flex items-center justify-center shrink-0">
                    {template.icon}
                  </div>
                  <div>
                    <div className="text-sm font-medium">{template.label}</div>
                    <div className="text-xs text-muted-foreground mt-0.5">{template.description}</div>
                  </div>
                </button>
              ))}
            </div>
          </SectionCard>
        )}

        {/* Active Protection List */}
        {activeProtections.length === 0 ? (
          <EmptyState
            icon={<Shield className="w-10 h-10 text-muted-foreground" />}
            title="未配置保护规则"
            description="点击「添加规则」配置交易保护机制，防止过度交易和重大亏损"
          />
        ) : (
          <div className="space-y-3">
            {activeProtections.map((protection, index) => {
              const template = PROTECTION_TEMPLATES.find((t) => t.name === protection.name)
              return (
                <SectionCard key={index} className="relative">
                  <div className="flex items-center justify-between mb-3">
                    <div className="flex items-center gap-2">
                      {template?.icon || <Shield className="w-4 h-4" />}
                      <span className="font-medium text-sm">{template?.label || protection.name}</span>
                    </div>
                    <button
                      onClick={() => handleRemoveProtection(index)}
                      className="p-1.5 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
                    {template?.fields.map((field) => (
                      <div key={field.key} className="space-y-1">
                        <label className="text-xs text-muted-foreground">{field.label}</label>
                        {field.type === 'select' ? (
                          <select
                            value={protection.params[field.key]}
                            onChange={(e) => handleUpdateParam(index, field.key, e.target.value)}
                            className="w-full px-2.5 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                          >
                            {field.options?.map((opt) => (
                              <option key={opt} value={opt}>{opt}</option>
                            ))}
                          </select>
                        ) : (
                          <input
                            type="number"
                            value={protection.params[field.key]}
                            onChange={(e) => {
                              const val = field.step && field.step < 1
                                ? parseFloat(e.target.value)
                                : parseInt(e.target.value)
                              handleUpdateParam(index, field.key, val)
                            }}
                            min={field.min}
                            max={field.max}
                            step={field.step}
                            className="w-full px-2.5 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                          />
                        )}
                      </div>
                    ))}
                  </div>
                </SectionCard>
              )
            })}

            {/* Save Button */}
            <div className="flex items-center justify-end gap-3">
              {configMutation.isError && (
                <span className="text-xs text-red-400">
                  保存失败: {(configMutation.error as any)?.message}
                </span>
              )}
              {configMutation.isSuccess && (
                <span className="text-xs text-green-400 flex items-center gap-1">
                  <CheckCircle2 className="w-3 h-3" />
                  已保存
                </span>
              )}
              <button
                onClick={handleSave}
                disabled={configMutation.isPending || activeProtections.length === 0}
                className={cn(
                  'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
                  configMutation.isPending || activeProtections.length === 0
                    ? 'bg-muted text-muted-foreground cursor-not-allowed'
                    : 'bg-quant-gold text-white hover:bg-quant-gold/90'
                )}
              >
                {configMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
                {configMutation.isPending ? '保存中...' : '保存配置'}
              </button>
            </div>
          </div>
        )}

        {/* Quick Actions */}
        <SectionCard title={<>快捷操作 <Activity className="w-4 h-4 inline ml-1 text-quant-gold" /></>}>
          <div className="flex flex-wrap gap-2">
            <button
              onClick={() => resetMutation.mutate({ scope: 'all' })}
              disabled={resetMutation.isPending}
              className="flex items-center gap-1.5 px-3 py-2 rounded-md bg-quant-bg-secondary text-xs font-medium hover:bg-white/5 transition-colors"
            >
              <RefreshCw className={cn('w-3.5 h-3.5', resetMutation.isPending && 'animate-spin')} />
              重置全部阻断
            </button>
            <button
              onClick={() => queryClient.invalidateQueries({ queryKey: ['protection-status'] })}
              className="flex items-center gap-1.5 px-3 py-2 rounded-md bg-quant-bg-secondary text-xs font-medium hover:bg-white/5 transition-colors"
            >
              <Activity className="w-3.5 h-3.5" />
              刷新状态
            </button>
          </div>
        </SectionCard>
      </div>
    </div>
  )
}
