import React, { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Save,
  TrendingUp,
  TrendingDown,
  ArrowLeftRight,
  Gauge,
  AlertTriangle,
  BarChart3,
  Settings2,
} from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Switch } from '@/components/ui/Switch'
import { Slider } from '@/components/ui/Slider'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Skeleton } from '@/components/ui/Skeleton'
import { contractApi } from '@/lib/api'
import type { ContractParams, ContractMarginInfo } from '@/types'

const INDICATOR_OPTIONS = [
  { value: 'macd_golden', label: 'MACD 金叉' },
  { value: 'macd_death', label: 'MACD 死叉' },
  { value: 'ema_counter', label: 'EMA 逆势' },
  { value: 'ema_follow', label: 'EMA 顺势' },
  { value: 'none', label: '无指标' },
]

const TIMEFRAME_OPTIONS = [
  { value: '5m', label: '5 分钟' },
  { value: '15m', label: '15 分钟' },
  { value: '30m', label: '30 分钟' },
  { value: '1h', label: '1 小时' },
  { value: '4h', label: '4 小时' },
  { value: '8h', label: '8 小时' },
]

const DIRECTION_OPTIONS = [
  { value: 'long', label: '做多' },
  { value: 'short', label: '做空' },
  { value: 'both', label: '双向' },
]

const MARGIN_MODE_OPTIONS = [
  { value: 'isolated', label: '逐仓' },
  { value: 'cross', label: '全仓' },
]

interface ContractFormValues {
  leverage: number
  direction: 'long' | 'short' | 'both'
  margin_mode: 'isolated' | 'cross'
  open_indicator: 'macd_golden' | 'macd_death' | 'ema_counter' | 'ema_follow' | 'none'
  indicator_timeframe: '5m' | '15m' | '30m' | '1h' | '4h' | '8h'
  enable_trend_following: boolean
  max_positions: number
  symbol: string
}

const DEFAULT_VALUES: ContractFormValues = {
  leverage: 10,
  direction: 'both',
  margin_mode: 'cross',
  open_indicator: 'macd_golden',
  indicator_timeframe: '1h',
  enable_trend_following: true,
  max_positions: 5,
  symbol: 'BTCUSDT',
}

/* ── Margin Info Card ── */
interface MarginInfoProps {
  data: ContractMarginInfo | undefined
  isLoading: boolean
}

const MarginInfoCard: React.FC<MarginInfoProps> = ({ data, isLoading }) => {
  if (isLoading) {
    return <Skeleton className="h-48 rounded-xl" />
  }

  if (!data) {
    return (
      <div className="text-center py-8 text-[#555]">
        <AlertTriangle className="w-8 h-8 mx-auto mb-2 opacity-50" />
        <p className="text-sm">暂无保证金信息</p>
      </div>
    )
  }

  const items = [
    { label: '钱包余额', value: formatCurrency(data.wallet_balance), color: 'text-[#e0e0e0]' },
    { label: '可用余额', value: formatCurrency(data.available_balance), color: 'text-[#52c41a]' },
    { label: '保证金余额', value: formatCurrency(data.margin_balance), color: 'text-[#1890ff]' },
    { label: '维持保证金', value: formatCurrency(data.maintenance_margin), color: 'text-[#faad14]' },
    { label: '未实现盈亏', value: `${data.unrealized_pnl >= 0 ? '+' : ''}${formatCurrency(data.unrealized_pnl)}`, color: data.unrealized_pnl >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]' },
    { label: '今日已实现', value: `${data.realized_pnl_today >= 0 ? '+' : ''}${formatCurrency(data.realized_pnl_today)}`, color: data.realized_pnl_today >= 0 ? 'text-[#52c41a]' : 'text-[#f5222d]' },
  ]

  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
      {items.map((item) => (
        <div
          key={item.label}
          className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-3"
        >
          <div className="text-[10px] text-[#888] mb-1">{item.label}</div>
          <div className={cn('text-sm font-semibold', item.color)}>{item.value}</div>
        </div>
      ))}
      {data.liquidation_price && (
        <div className="rounded-xl border border-[#f5222d]/20 bg-[#f5222d]/5 p-3 col-span-full sm:col-span-1">
          <div className="text-[10px] text-[#f5222d] mb-1 flex items-center gap-1">
            <AlertTriangle className="w-3 h-3" />
            预估强平价
          </div>
          <div className="text-sm font-semibold text-[#f5222d]">{formatCurrency(data.liquidation_price)}</div>
        </div>
      )}
    </div>
  )
}

/* ── Liquidation Calculator ── */
const LiquidationCalc: React.FC = () => {
  const [entryPrice, setEntryPrice] = useState<number>(50000)
  const [leverage, setLeverage] = useState<number>(10)
  const [side, setSide] = useState<'LONG' | 'SHORT'>('LONG')

  const { data: liqData, isFetching } = useQuery({
    queryKey: ['contract', 'liquidation', entryPrice, side, leverage],
    queryFn: () => contractApi.getLiquidationPrice({
      entry_price: entryPrice,
      side,
      leverage,
    }).then((r) => r.data),
    enabled: entryPrice > 0 && leverage > 0,
  })

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <Input
          label="入场价格"
          type="number"
          min={1}
          value={entryPrice}
          onChange={(e) => setEntryPrice(Number(e.target.value))}
        />
        <Input
          label="杠杆"
          type="number"
          min={1}
          max={125}
          value={leverage}
          onChange={(e) => setLeverage(Number(e.target.value))}
        />
        <div>
          <label className="block text-xs font-medium text-[#aaaaaa] mb-1.5">方向</label>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setSide('LONG')}
              className={cn(
                'flex-1 rounded-xl border py-2 text-xs font-medium transition-all',
                side === 'LONG'
                  ? 'border-[#52c41a]/40 bg-[#52c41a]/10 text-[#52c41a]'
                  : 'border-[#2a2a2a] bg-[#111] text-[#888] hover:border-[#333]'
              )}
            >
              <TrendingUp className="w-3 h-3 inline mr-1" />
              多
            </button>
            <button
              type="button"
              onClick={() => setSide('SHORT')}
              className={cn(
                'flex-1 rounded-xl border py-2 text-xs font-medium transition-all',
                side === 'SHORT'
                  ? 'border-[#f5222d]/40 bg-[#f5222d]/10 text-[#f5222d]'
                  : 'border-[#2a2a2a] bg-[#111] text-[#888] hover:border-[#333]'
              )}
            >
              <TrendingDown className="w-3 h-3 inline mr-1" />
              空
            </button>
          </div>
        </div>
      </div>

      {liqData && (
        <div className="rounded-xl border border-[#1890ff]/20 bg-[#1890ff]/5 p-3">
          <div className="flex items-center justify-between">
            <span className="text-xs text-[#888]">预估强平价</span>
            <span className="text-lg font-bold text-[#1890ff]">
              {isFetching ? '...' : formatCurrency(liqData.liquidation_price)}
            </span>
          </div>
        </div>
      )}
    </div>
  )
}

/* ── Main Panel ── */
export const ContractPanel: React.FC = () => {
  const queryClient = useQueryClient()

  const { data: savedParams } = useQuery({
    queryKey: ['contract', 'params'],
    queryFn: () => contractApi.getParams().then((r) => r.data),
  })

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<ContractFormValues>({
    defaultValues: DEFAULT_VALUES,
  })

  useEffect(() => {
    if (savedParams) {
      reset({
        ...DEFAULT_VALUES,
        ...savedParams,
        symbol: savedParams.symbol || DEFAULT_VALUES.symbol,
      })
    }
  }, [savedParams, reset])

  const leverage = watch('leverage')
  const direction = watch('direction')
  const marginMode = watch('margin_mode')
  const trendFollowing = watch('enable_trend_following')

  const saveMutation = useMutation({
    mutationFn: (data: ContractParams) => contractApi.saveParams(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['contract', 'params'] })
    },
  })

  const leverageMutation = useMutation({
    mutationFn: (lev: number) => contractApi.setLeverage(lev),
  })

  const handleFormSubmit = (values: ContractFormValues) => {
    const params: ContractParams = {
      leverage: values.leverage,
      direction: values.direction,
      margin_mode: values.margin_mode,
      open_indicator: values.open_indicator,
      indicator_timeframe: values.indicator_timeframe,
      enable_trend_following: values.enable_trend_following,
      max_positions: values.max_positions,
      symbol: values.symbol,
    }
    saveMutation.mutate(params)
    leverageMutation.mutate(values.leverage)
  }

  const { data: marginInfo, isLoading: marginLoading } = useQuery({
    queryKey: ['contract', 'margin'],
    queryFn: () => contractApi.getMarginInfo().then((r) => r.data),
    refetchInterval: 10000,
  })

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-5">
      {/* Margin Info */}
      <SectionCard
        title="保证金信息"
        headerAction={
          marginInfo && (
            <div className="flex items-center gap-2">
              <Badge variant="info">{marginInfo.leverage}x</Badge>
              <Badge variant={marginInfo.margin_mode === 'cross' ? 'success' : 'warning'}>
                {marginInfo.margin_mode === 'cross' ? '全仓' : '逐仓'}
              </Badge>
            </div>
          )
        }
      >
        <MarginInfoCard data={marginInfo} isLoading={marginLoading} />
      </SectionCard>

      {/* Contract Params Form */}
      <SectionCard title="合约参数" headerAction={<Settings2 className="w-4 h-4 text-[#555]" />}>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Input
            label="交易对"
            placeholder="如: BTCUSDT"
            {...register('symbol')}
          />
          <div>
            <Slider
              label="杠杆倍数"
              min={1}
              max={100}
              step={1}
              value={leverage}
              onChange={(v) => setValue('leverage', v)}
              valueFormatter={(v) => `${v}x`}
            />
            <input type="hidden" {...register('leverage', { valueAsNumber: true })} />
          </div>

          <Select
            label="交易方向"
            options={DIRECTION_OPTIONS}
            {...register('direction')}
          />
          <Select
            label="保证金模式"
            options={MARGIN_MODE_OPTIONS}
            {...register('margin_mode')}
          />

          <Select
            label="开仓指标"
            options={INDICATOR_OPTIONS}
            {...register('open_indicator')}
          />
          <Select
            label="指标周期"
            options={TIMEFRAME_OPTIONS}
            {...register('indicator_timeframe')}
          />

          <Input
            label="最大持仓数"
            type="number"
            min={1}
            max={50}
            {...register('max_positions', {
              required: true,
              valueAsNumber: true,
              min: { value: 1, message: '最小1' },
              max: { value: 50, message: '最大50' },
            })}
            error={errors.max_positions?.message}
          />

          <div className="flex items-end">
            <Switch
              label="顺势而为"
              checked={trendFollowing}
              onChange={(e) => setValue('enable_trend_following', e.target.checked)}
            />
          </div>
        </div>

        {/* Direction Badges */}
        <div className="flex flex-wrap gap-2 mt-4">
          <Badge variant={direction === 'long' || direction === 'both' ? 'success' : 'neutral'}>
            <TrendingUp className="w-3 h-3 inline mr-1" />
            做多
          </Badge>
          <Badge variant={direction === 'short' || direction === 'both' ? 'error' : 'neutral'}>
            <TrendingDown className="w-3 h-3 inline mr-1" />
            做空
          </Badge>
          <Badge variant={marginMode === 'cross' ? 'info' : 'warning'}>
            <ArrowLeftRight className="w-3 h-3 inline mr-1" />
            {marginMode === 'cross' ? '全仓' : '逐仓'}
          </Badge>
          {trendFollowing && (
            <Badge variant="success">
              <BarChart3 className="w-3 h-3 inline mr-1" />
              顺势而为
            </Badge>
          )}
        </div>
      </SectionCard>

      {/* Liquidation Calculator */}
      <SectionCard title="强平价计算器" headerAction={<Gauge className="w-4 h-4 text-[#555]" />}>
        <LiquidationCalc />
      </SectionCard>

      {/* Submit */}
      <div className="flex items-center gap-3 pt-2">
        <Button
          type="submit"
          variant="primary"
          isLoading={saveMutation.isPending || leverageMutation.isPending}
          leftIcon={<Save className="w-4 h-4" />}
        >
          保存合约配置
        </Button>
        {saveMutation.isSuccess && (
          <Badge variant="success">保存成功</Badge>
        )}
      </div>
    </form>
  )
}

export default ContractPanel
