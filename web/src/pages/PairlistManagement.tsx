import { useState, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { pairlistApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { EmptyState } from '@/components/ui/EmptyState'
import { SectionCard } from '@/components/ui/SectionCard'
import { PageHeader } from '@/components/ui/PageHeader'
import { KPICard } from '@/components/ui/KPICard'
import type { PairlistConfig } from '@/types'
import {
  ListFilter,
  RefreshCw,
  Plus,
  Trash2,
  CheckCircle2,
  Globe,
  DollarSign,
  Hash,
  TrendingUp,
  Activity,
  Settings2,
  ChevronDown,
  ChevronUp,
  Search,
  Layers,
} from 'lucide-react'

/* ── Types ── */
// Use PairlistConfig from @/types (has index signature for compatibility)

/* ── Producer Templates ── */
interface FieldDef {
  key: string
  label: string
  type: 'tags' | 'number'
  min?: number
  max?: number
  step?: number
}

interface ProducerTemplate {
  name: string
  label: string
  description: string
  defaultParams: Record<string, unknown>
  fields: FieldDef[]
}

const PRODUCER_TEMPLATES: ProducerTemplate[] = [
  {
    name: 'StaticPairList',
    label: '静态交易对列表',
    description: '手动指定交易对',
    defaultParams: { pairs: ['BTCUSDT', 'ETHUSDT', 'SOLUSDT'] },
    fields: [
      { key: 'pairs', label: '交易对', type: 'tags' },
    ],
  },
  {
    name: 'VolumePairList',
    label: '成交量排行',
    description: '按成交量排序选取前 N 名',
    defaultParams: { top_n: 30, min_volume: 1000000 },
    fields: [
      { key: 'top_n', label: '前 N 名', type: 'number', min: 1, max: 200 },
      { key: 'min_volume', label: '最小成交量', type: 'number', min: 0 },
    ],
  },
]

/* ── Filter Templates ── */
interface FilterTemplate {
  name: string
  label: string
  description: string
  defaultParams: Record<string, unknown>
  fields: FieldDef[]
}

const FILTER_TEMPLATES: FilterTemplate[] = [
  {
    name: 'PriceFilter',
    label: '价格过滤',
    description: '过滤价格超出范围的交易对',
    defaultParams: { min_price: 0.000001, max_price: 100000 },
    fields: [
      { key: 'min_price', label: '最小价格', type: 'number', min: 0 },
      { key: 'max_price', label: '最大价格', type: 'number', min: 0 },
    ],
  },
  {
    name: 'SpreadFilter',
    label: '价差过滤',
    description: '过滤价差过大的交易对',
    defaultParams: { max_spread_pct: 0.5 },
    fields: [
      { key: 'max_spread_pct', label: '最大价差 (%)', type: 'number', min: 0, max: 10, step: 0.1 },
    ],
  },
  {
    name: 'VolatilityFilter',
    label: '波动率过滤',
    description: '过滤波动率异常的交易对',
    defaultParams: { min_volatility_pct: 0.5, max_volatility_pct: 15 },
    fields: [
      { key: 'min_volatility_pct', label: '最小波动率 (%)', type: 'number', min: 0, max: 50, step: 0.1 },
      { key: 'max_volatility_pct', label: '最大波动率 (%)', type: 'number', min: 0, max: 100, step: 0.1 },
    ],
  },
  {
    name: 'PrecisionFilter',
    label: '精度过滤',
    description: '过滤精度不足的交易对',
    defaultParams: { min_price_precision: 2, min_qty_precision: 2 },
    fields: [
      { key: 'min_price_precision', label: '最小价格精度', type: 'number', min: 0, max: 8 },
      { key: 'min_qty_precision', label: '最小数量精度', type: 'number', min: 0, max: 8 },
    ],
  },
  {
    name: 'MaxPairsFilter',
    label: '最大数量限制',
    description: '限制最终交易对数量',
    defaultParams: { max_pairs: 50 },
    fields: [
      { key: 'max_pairs', label: '最大数量', type: 'number', min: 1, max: 500 },
    ],
  },
  {
    name: 'AgeFilter',
    label: '上市时间过滤',
    description: '过滤上市时间太短的交易对',
    defaultParams: { min_age_days: 7 },
    fields: [
      { key: 'min_age_days', label: '最小上市天数', type: 'number', min: 0, max: 365 },
    ],
  },
  {
    name: 'PerformanceFilter',
    label: '表现过滤',
    description: '保留表现最好的 N 个交易对',
    defaultParams: { top_n: 20 },
    fields: [
      { key: 'top_n', label: '保留前 N', type: 'number', min: 1, max: 200 },
    ],
  },
]

/* ── Page ── */
export function PairlistManagement() {
  const queryClient = useQueryClient()
  const [producers, setProducers] = useState<PairlistConfig['producers']>([])
  const [filters, setFilters] = useState<PairlistConfig['filters']>([])
  const [showProducerForm, setShowProducerForm] = useState(false)
  const [showFilterForm, setShowFilterForm] = useState(false)
  const [expandedSection, setExpandedSection] = useState<'producers' | 'filters' | 'result'>('result')

  // Queries
  const { data: whitelist, isLoading: whitelistLoading } = useQuery({
    queryKey: ['pairlist-whitelist'],
    queryFn: () => pairlistApi.whitelist(),
  })

  const { data: config } = useQuery({
    queryKey: ['pairlist-config'],
    queryFn: () => pairlistApi.config(),
  })

  // Mutations
  const refreshMutation = useMutation({
    mutationFn: () => pairlistApi.refresh(),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pairlist-whitelist'] }),
  })

  const configureMutation = useMutation({
    mutationFn: (data: PairlistConfig) => pairlistApi.configure(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pairlist-whitelist', 'pairlist-config'] })
    },
  })

  const handleAddProducer = useCallback((templateName: string) => {
    const template = PRODUCER_TEMPLATES.find((t) => t.name === templateName)
    if (!template) return
    setProducers((prev) => [...prev, { name: template.name, params: { ...template.defaultParams } }])
    setShowProducerForm(false)
  }, [])

  const handleAddFilter = useCallback((templateName: string) => {
    const template = FILTER_TEMPLATES.find((t) => t.name === templateName)
    if (!template) return
    setFilters((prev) => [...prev, { name: template.name, params: { ...template.defaultParams } }])
    setShowFilterForm(false)
  }, [])

  const handleUpdateParam = useCallback((
    type: 'producer' | 'filter',
    index: number,
    key: string,
    value: string | number | boolean | string[]
  ) => {
    if (type === 'producer') {
      setProducers((prev) => {
        const next = [...prev]
        next[index] = { ...next[index], params: { ...next[index].params, [key]: value } }
        return next
      })
    } else {
      setFilters((prev) => {
        const next = [...prev]
        next[index] = { ...next[index], params: { ...next[index].params, [key]: value } }
        return next
      })
    }
  }, [])

  const handleRemove = useCallback((type: 'producer' | 'filter', index: number) => {
    if (type === 'producer') {
      setProducers((prev) => prev.filter((_, i) => i !== index))
    } else {
      setFilters((prev) => prev.filter((_, i) => i !== index))
    }
  }, [])

  const handleSave = useCallback(() => {
    configureMutation.mutate({ producers, filters })
  }, [producers, filters, configureMutation])

  const pairs = (whitelist?.whitelist as string[]) || []

  return (
    <div className="h-full overflow-y-auto">
      <div className="p-4 md:p-6 space-y-6 max-w-7xl mx-auto">
        <PageHeader
          title="交易对筛选"
          subtitle="配置交易对来源和过滤规则"
          actions={<ListFilter className="w-6 h-6 text-quant-gold" />}
        />

        {/* KPI Cards */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <KPICard
            label="当前白名单"
            value={String(whitelist?.whitelist?.length ?? '-')}
            icon={<Layers className="w-4 h-4 text-quant-gold" />}
            subValue="交易对"
            trend="up"
          />
          <KPICard
            label="交易所"
            value={String(whitelist?.exchange ?? 'binance')}
            icon={<Globe className="w-4 h-4 text-quant-gold" />}
            subValue="数据源"
            trend="neutral"
          />
          <KPICard
            label="计价资产"
            value={String(whitelist?.quote_asset ?? 'USDT')}
            icon={<DollarSign className="w-4 h-4 text-quant-gold" />}
            subValue="基准"
            trend="neutral"
          />
          <KPICard
            label="最后更新"
            value={whitelist?.generated_at ? new Date(whitelist.generated_at).toLocaleTimeString() : '-'}
            icon={<RefreshCw className="w-4 h-4 text-quant-gold" />}
            subValue="最近"
            trend="neutral"
          />
        </div>

        {/* Result Section */}
        <SectionCard
          title={
            <button
              onClick={() => setExpandedSection(expandedSection === 'result' ? 'producers' : 'result')}
              className="flex items-center gap-2"
            >
              当前白名单
              {expandedSection === 'result' ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
            </button>
          }
          headerAction={
            <button
              onClick={() => refreshMutation.mutate()}
              disabled={refreshMutation.isPending}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-quant-gold/10 text-quant-gold text-xs font-medium hover:bg-quant-gold/20 transition-colors"
            >
              <RefreshCw className={cn('w-3.5 h-3.5', refreshMutation.isPending && 'animate-spin')} />
              刷新
            </button>
          }
        >
          {expandedSection === 'result' && (
            <div>
              {whitelistLoading ? (
                <div className="grid grid-cols-4 md:grid-cols-8 gap-2">
                  {Array.from({ length: 16 }).map((_, i) => (
                    <div key={i} className="h-8 rounded-md bg-quant-bg-secondary animate-pulse" />
                  ))}
                </div>
              ) : pairs.length === 0 ? (
                <EmptyState
                  icon={<Search className="w-10 h-10 text-muted-foreground" />}
                  title="暂无交易对"
                  description="配置生产器和过滤器后点击刷新"
                />
              ) : (
                <div className="grid grid-cols-3 md:grid-cols-6 lg:grid-cols-8 gap-2">
                  {pairs.map((pair: string) => (
                    <div
                      key={pair}
                      className="px-2 py-1.5 rounded-md bg-quant-bg-secondary text-xs font-medium text-center hover:bg-white/5 transition-colors"
                    >
                      {pair}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </SectionCard>

        {/* Producers Section */}
        <SectionCard
          title={
            <button
              onClick={() => setExpandedSection(expandedSection === 'producers' ? 'result' : 'producers')}
              className="flex items-center gap-2"
            >
              生产器 (Producers)
              {expandedSection === 'producers' ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
            </button>
          }
          headerAction={
            <button
              onClick={() => setShowProducerForm(!showProducerForm)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-quant-gold/10 text-quant-gold text-xs font-medium hover:bg-quant-gold/20 transition-colors"
            >
              <Plus className="w-3.5 h-3.5" />
              添加
            </button>
          }
        >
          {expandedSection === 'producers' && (
            <div className="space-y-3">
              {showProducerForm && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-2 p-3 rounded-md bg-quant-bg-secondary">
                  {PRODUCER_TEMPLATES.map((t) => (
                    <button
                      key={t.name}
                      onClick={() => handleAddProducer(t.name)}
                      className="flex items-start gap-2 p-2 rounded-md hover:bg-white/5 transition-colors text-left"
                    >
                      <Plus className="w-4 h-4 text-quant-gold shrink-0 mt-0.5" />
                      <div>
                        <div className="text-sm font-medium">{t.label}</div>
                        <div className="text-xs text-muted-foreground">{t.description}</div>
                      </div>
                    </button>
                  ))}
                </div>
              )}
              {producers.length === 0 ? (
                <div className="text-sm text-muted-foreground text-center py-4">未配置生产器</div>
              ) : (
                producers.map((p, i) => {
                  const template = PRODUCER_TEMPLATES.find((t) => t.name === p.name)
                  return (
                    <div key={i} className="p-3 rounded-md bg-quant-bg-secondary">
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-sm font-medium">{template?.label || p.name}</span>
                        <button
                          onClick={() => handleRemove('producer', i)}
                          className="p-1 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                      <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                        {template?.fields.map((field) => (
                          <div key={field.key} className="space-y-1">
                            <label className="text-xs text-muted-foreground">{field.label}</label>
                            {field.type === 'tags' ? (
                              <input
                                type="text"
                                value={Array.isArray(p.params[field.key]) ? (p.params[field.key] as string[]).join(',') : ''}
                                onChange={(e) => handleUpdateParam('producer', i, field.key, e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
                                className="w-full px-2 py-1 rounded-md bg-quant-bg border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                                placeholder="BTCUSDT,ETHUSDT,..."
                              />
                            ) : (
                              <input
                                type="number"
                                value={p.params[field.key] as number}
                                onChange={(e) => handleUpdateParam('producer', i, field.key, parseFloat(e.target.value))}
                                className="w-full px-2 py-1 rounded-md bg-quant-bg border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                              />
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  )
                })
              )}
            </div>
          )}
        </SectionCard>

        {/* Filters Section */}
        <SectionCard
          title={
            <button
              onClick={() => setExpandedSection(expandedSection === 'filters' ? 'result' : 'filters')}
              className="flex items-center gap-2"
            >
              过滤器 (Filters)
              {expandedSection === 'filters' ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
            </button>
          }
          headerAction={
            <button
              onClick={() => setShowFilterForm(!showFilterForm)}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-md bg-quant-gold/10 text-quant-gold text-xs font-medium hover:bg-quant-gold/20 transition-colors"
            >
              <Plus className="w-3.5 h-3.5" />
              添加
            </button>
          }
        >
          {expandedSection === 'filters' && (
            <div className="space-y-3">
              {showFilterForm && (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2 p-3 rounded-md bg-quant-bg-secondary">
                  {FILTER_TEMPLATES.map((t) => (
                    <button
                      key={t.name}
                      onClick={() => handleAddFilter(t.name)}
                      className="flex items-start gap-2 p-2 rounded-md hover:bg-white/5 transition-colors text-left"
                    >
                      <Plus className="w-4 h-4 text-quant-gold shrink-0 mt-0.5" />
                      <div>
                        <div className="text-sm font-medium">{t.label}</div>
                        <div className="text-xs text-muted-foreground">{t.description}</div>
                      </div>
                    </button>
                  ))}
                </div>
              )}
              {filters.length === 0 ? (
                <div className="text-sm text-muted-foreground text-center py-4">未配置过滤器</div>
              ) : (
                filters.map((f, i) => {
                  const template = FILTER_TEMPLATES.find((t) => t.name === f.name)
                  return (
                    <div key={i} className="p-3 rounded-md bg-quant-bg-secondary">
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-sm font-medium">{template?.label || f.name}</span>
                        <button
                          onClick={() => handleRemove('filter', i)}
                          className="p-1 rounded-md hover:bg-red-500/10 text-muted-foreground hover:text-red-400 transition-colors"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                      <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
                        {template?.fields.map((field) => (
                          <div key={field.key} className="space-y-1">
                            <label className="text-xs text-muted-foreground">{field.label}</label>
                            <input
                              type="number"
                              value={f.params[field.key] as number}
                              onChange={(e) => handleUpdateParam('filter', i, field.key, parseFloat(e.target.value))}
                              step={field.step || 1}
                              className="w-full px-2 py-1 rounded-md bg-quant-bg border border-quant-border text-sm focus:outline-none focus:border-quant-gold"
                            />
                          </div>
                        ))}
                      </div>
                    </div>
                  )
                })
              )}
            </div>
          )}
        </SectionCard>

        {/* Save */}
        <div className="flex items-center justify-end gap-3">
          {configureMutation.isError && (
            <span className="text-xs text-red-400">保存失败: {configureMutation.error?.message}</span>
          )}
          {configureMutation.isSuccess && (
            <span className="text-xs text-green-400 flex items-center gap-1">
              <CheckCircle2 className="w-3 h-3" />
              已保存
            </span>
          )}
          <button
            onClick={handleSave}
            disabled={configureMutation.isPending}
            className={cn(
              'flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors',
              configureMutation.isPending
                ? 'bg-muted text-muted-foreground cursor-not-allowed'
                : 'bg-quant-gold text-white hover:bg-quant-gold/90'
            )}
          >
            {configureMutation.isPending ? <RefreshCw className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
            {configureMutation.isPending ? '保存中...' : '保存配置'}
          </button>
        </div>
      </div>
    </div>
  )
}
