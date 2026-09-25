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
  type: 'tags' | 'number' | 'text' | 'select' | 'bool'
  min?: number
  max?: number
  step?: number
  options?: { value: string; label: string }[]
  placeholder?: string
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
  {
    name: 'MarketCapPairList',
    label: '市值排行',
    description: '按市值排名取前 N（需配置市值数据源，未配置时降级报错）',
    defaultParams: { number_assets: 30, max_rank: 30, refresh_period_sec: 86400 },
    fields: [
      { key: 'number_assets', label: '取前 N', type: 'number', min: 1, max: 250 },
      { key: 'max_rank', label: '最大市值排名', type: 'number', min: 1, max: 250 },
      { key: 'refresh_period_sec', label: '刷新间隔(秒)', type: 'number', min: 60 },
    ],
  },
  {
    name: 'PercentChangePairList',
    label: '涨跌幅排行',
    description: '按 N 周期涨跌幅排序选币（K线回溯或 24h ticker）',
    defaultParams: { number_assets: 30, lookback_period: 0, lookback_timeframe: '1h', sort_direction: 'desc' },
    fields: [
      { key: 'number_assets', label: '取前 N', type: 'number', min: 1, max: 500 },
      { key: 'lookback_period', label: '回溯K线数(0=24h)', type: 'number', min: 0, max: 500 },
      {
        key: 'lookback_timeframe', label: 'K线周期', type: 'select',
        options: ['1m', '5m', '15m', '1h', '4h', '1d'].map((v) => ({ value: v, label: v })),
      },
      {
        key: 'sort_direction', label: '排序方向', type: 'select',
        options: [
          { value: 'desc', label: '涨幅降序 (desc)' },
          { value: 'asc', label: '涨幅升序 (asc)' },
        ],
      },
      { key: 'min_value', label: '涨跌幅下限(%)', type: 'number', step: 0.1 },
      { key: 'max_value', label: '涨跌幅上限(%)', type: 'number', step: 0.1 },
    ],
  },
  {
    name: 'RemotePairList',
    label: '远端名单',
    description: '从 URL 拉取名单（带超时与本地缓存兜底）',
    defaultParams: { pairlist_url: 'https://example.com/pairlist.json', number_assets: 0, refresh_period_sec: 1800, read_timeout_sec: 10, keep_pairlist_on_failure: true },
    fields: [
      { key: 'pairlist_url', label: '名单 URL', type: 'text', placeholder: 'https://... 或 file:///path.json' },
      { key: 'number_assets', label: '取前 N(0=不限)', type: 'number', min: 0 },
      { key: 'refresh_period_sec', label: '刷新间隔(秒)', type: 'number', min: 10 },
      { key: 'read_timeout_sec', label: '读取超时(秒)', type: 'number', min: 1, max: 120 },
      { key: 'bearer_token', label: 'Bearer Token', type: 'text' },
      { key: 'keep_pairlist_on_failure', label: '失败时缓存兜底', type: 'bool' },
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
  {
    name: 'DelistFilter',
    label: '退市过滤',
    description: '剔除非交易状态/计划退市/长期无成交的交易对',
    defaultParams: { max_days_from_now: -1, max_inactive_days: 0 },
    fields: [
      { key: 'max_days_from_now', label: 'N天内将退市即剔除(-1关闭)', type: 'number', min: -1 },
      { key: 'max_inactive_days', label: 'N天无成交即剔除(0关闭)', type: 'number', min: 0 },
    ],
  },
  {
    name: 'RemotePairList',
    label: '远端名单',
    description: '用远端名单过滤/追加/剔除（支持黑/白名单模式）',
    defaultParams: { pairlist_url: 'https://example.com/pairlist.json', mode: 'whitelist', processing_mode: 'filter', number_assets: 0, refresh_period_sec: 1800, read_timeout_sec: 10, keep_pairlist_on_failure: true },
    fields: [
      { key: 'pairlist_url', label: '名单 URL', type: 'text', placeholder: 'https://... 或 file:///path.json' },
      {
        key: 'mode', label: '模式', type: 'select',
        options: [
          { value: 'whitelist', label: '白名单' },
          { value: 'blacklist', label: '黑名单' },
        ],
      },
      {
        key: 'processing_mode', label: '合并方式', type: 'select',
        options: [
          { value: 'filter', label: '过滤（取交集）' },
          { value: 'append', label: '追加（并集）' },
        ],
      },
      { key: 'number_assets', label: '取前 N(0=不限)', type: 'number', min: 0 },
      { key: 'refresh_period_sec', label: '刷新间隔(秒)', type: 'number', min: 10 },
      { key: 'read_timeout_sec', label: '读取超时(秒)', type: 'number', min: 1, max: 120 },
      { key: 'bearer_token', label: 'Bearer Token', type: 'text' },
      { key: 'keep_pairlist_on_failure', label: '失败时缓存兜底', type: 'bool' },
    ],
  },
  {
    name: 'OffsetFilter',
    label: '偏移过滤',
    description: '跳过名单前 N 个交易对',
    defaultParams: { offset: 0 },
    fields: [
      { key: 'offset', label: '跳过数量', type: 'number', min: 0 },
    ],
  },
  {
    name: 'ShuffleFilter',
    label: '随机打乱',
    description: '随机打乱名单顺序，避免过拟合',
    defaultParams: { seed: 0 },
    fields: [
      { key: 'seed', label: '随机种子', type: 'number' },
    ],
  },
  {
    name: 'VolumeFilter',
    label: '成交量过滤',
    description: '剔除 24h 成交量低于阈值的交易对',
    defaultParams: { min_volume: 1000000 },
    fields: [
      { key: 'min_volume', label: '最小成交量', type: 'number', min: 0 },
    ],
  },
  {
    name: 'CorrelationFilter',
    label: '相关性过滤',
    description: '剔除与基准高度相关的同质化交易对',
    defaultParams: { max_correlated: 0.95 },
    fields: [
      { key: 'max_correlated', label: '最大相关性', type: 'number', min: 0, max: 1, step: 0.01 },
    ],
  },
  {
    name: 'RangeStabilityFilter',
    label: '波动区间过滤',
    description: '剔除价格区间过窄的僵尸交易对',
    defaultParams: { min_range_ratio: 0.005 },
    fields: [
      { key: 'min_range_ratio', label: '最小区间比率', type: 'number', min: 0, step: 0.001 },
    ],
  },
  {
    name: 'FullTradesFilter',
    label: '持仓过滤',
    description: '剔除已有持仓的交易对',
    defaultParams: {},
    fields: [],
  },
  {
    name: 'MarketCapFilter',
    label: '市值过滤',
    description: '按市值上下限过滤交易对',
    defaultParams: { min_market_cap: 0, max_market_cap: 0 },
    fields: [
      { key: 'min_market_cap', label: '最小市值', type: 'number', min: 0 },
      { key: 'max_market_cap', label: '最大市值', type: 'number', min: 0 },
    ],
  },
  {
    name: 'VolumeChangeFilter',
    label: '成交量变化过滤',
    description: '剔除成交量骤变（缩量/异常放量）的交易对',
    defaultParams: { min_change: -0.8, max_change: 5.0 },
    fields: [
      { key: 'min_change', label: '最小变化率', type: 'number', step: 0.1 },
      { key: 'max_change', label: '最大变化率', type: 'number', step: 0.1 },
    ],
  },
  {
    name: 'PriceJumpFilter',
    label: '暴涨暴跌过滤',
    description: '剔除近期涨跌幅过大的交易对',
    defaultParams: { max_jump_pct: 20 },
    fields: [
      { key: 'max_jump_pct', label: '最大涨跌幅(%)', type: 'number', min: 0 },
    ],
  },
  {
    name: 'LiquidityFilter',
    label: '深度过滤',
    description: '剔除盘口深度不足的交易对',
    defaultParams: { min_bid_depth: 50000, min_ask_depth: 50000 },
    fields: [
      { key: 'min_bid_depth', label: '最小买盘深度', type: 'number', min: 0 },
      { key: 'min_ask_depth', label: '最小卖盘深度', type: 'number', min: 0 },
    ],
  },
  {
    name: 'FundingRateFilter',
    label: '资金费率过滤',
    description: '剔除资金费率过高的合约对',
    defaultParams: { max_funding_rate: 0.01 },
    fields: [
      { key: 'max_funding_rate', label: '最大资金费率', type: 'number', min: 0, step: 0.001 },
    ],
  },
  {
    name: 'RankFilter',
    label: '综合排名',
    description: '按成交量+表现加权评分取前 N',
    defaultParams: { top_n: 20, volume_weight: 0.5, performance_weight: 0.5 },
    fields: [
      { key: 'top_n', label: '取前 N', type: 'number', min: 1 },
      { key: 'volume_weight', label: '成交量权重', type: 'number', step: 0.1 },
      { key: 'performance_weight', label: '表现权重', type: 'number', step: 0.1 },
    ],
  },
  {
    name: 'LowProfitPairsFilter',
    label: '低收益过滤',
    description: '剔除历史收益低于阈值的交易对',
    defaultParams: { min_profit_pct: 0 },
    fields: [
      { key: 'min_profit_pct', label: '最小收益率(%)', type: 'number', step: 0.1 },
    ],
  },
  {
    name: 'ChangeFilter',
    label: '24h涨跌过滤',
    description: '保留 24h 涨跌幅在区间内的交易对',
    defaultParams: { min_change_pct: -10, max_change_pct: 20 },
    fields: [
      { key: 'min_change_pct', label: '最小涨跌幅(%)', type: 'number', step: 0.1 },
      { key: 'max_change_pct', label: '最大涨跌幅(%)', type: 'number', step: 0.1 },
    ],
  },
  {
    name: 'RangeFilter',
    label: '价格区间过滤',
    description: '保留价格在参考价一定比例区间内的交易对',
    defaultParams: { reference_price: 0, min_pct: 0.5, max_pct: 2.0 },
    fields: [
      { key: 'reference_price', label: '参考价格', type: 'number', min: 0 },
      { key: 'min_pct', label: '最小比例', type: 'number', step: 0.1 },
      { key: 'max_pct', label: '最大比例', type: 'number', step: 0.1 },
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

  // 通用参数字段渲染（tags/number/text/select/bool）
  const renderField = (
    kind: 'producer' | 'filter',
    index: number,
    field: FieldDef,
    value: unknown,
  ) => {
    const inputCls = 'w-full px-2 py-1 rounded-md bg-quant-bg border border-quant-border text-sm focus:outline-none focus:border-quant-gold'
    switch (field.type) {
      case 'tags':
        return (
          <input
            type="text"
            value={Array.isArray(value) ? (value as string[]).join(',') : ''}
            onChange={(e) => handleUpdateParam(kind, index, field.key, e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
            className={inputCls}
            placeholder="BTCUSDT,ETHUSDT,..."
          />
        )
      case 'text':
        return (
          <input
            type="text"
            value={String(value ?? '')}
            onChange={(e) => handleUpdateParam(kind, index, field.key, e.target.value)}
            className={inputCls}
            placeholder={field.placeholder}
          />
        )
      case 'select':
        return (
          <select
            value={String(value ?? '')}
            onChange={(e) => handleUpdateParam(kind, index, field.key, e.target.value)}
            className={inputCls}
          >
            {field.options?.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        )
      case 'bool':
        return (
          <button
            type="button"
            role="switch"
            aria-checked={Boolean(value)}
            onClick={() => handleUpdateParam(kind, index, field.key, !value)}
            className={cn(
              'w-9 h-5 rounded-full transition-colors relative',
              value ? 'bg-quant-gold' : 'bg-quant-border',
            )}
          >
            <span
              className={cn(
                'absolute top-0.5 w-4 h-4 rounded-full bg-white transition-all',
                value ? 'left-[18px]' : 'left-0.5',
              )}
            />
          </button>
        )
      default:
        return (
          <input
            type="number"
            value={value as number}
            onChange={(e) => handleUpdateParam(kind, index, field.key, parseFloat(e.target.value))}
            step={field.step || 1}
            min={field.min}
            max={field.max}
            className={inputCls}
          />
        )
    }
  }

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
                            {renderField('producer', i, field, p.params[field.key])}
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
                            {renderField('filter', i, field, f.params[field.key])}
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
