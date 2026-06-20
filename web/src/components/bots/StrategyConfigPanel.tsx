import React, { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { Save, RotateCcw, TrendingUp, BarChart3 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SectionCard } from '@/components/ui/SectionCard'
import { Input } from '@/components/ui/Input'
import { Select } from '@/components/ui/Select'
import { Switch } from '@/components/ui/Switch'
import { Slider } from '@/components/ui/Slider'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import type { MartinConfig, WallStreetConfig } from '@/types'

type StrategyType = 'martin' | 'wallstreet'

interface StrategyConfigFormValues {
  name: string
  strategy_type: StrategyType
  symbol: string
  leverage: number
  direction: 'long' | 'short' | 'dual'
  first_order_amount: number
  order_count: number
  add_position_spread: number
  add_position_callback: number
  take_profit_ratio: number
  profit_callback: number
  double_first_order: boolean
  loop_type: 'single' | 'cycle'
  loop_count: number
  enable_add_position: boolean
  flash_crash_protection: number
}

const DEFAULT_VALUES: StrategyConfigFormValues = {
  name: '',
  strategy_type: 'martin',
  symbol: 'BTCUSDT',
  leverage: 10,
  direction: 'long',
  first_order_amount: 100,
  order_count: 7,
  add_position_spread: 3.5,
  add_position_callback: 0.1,
  take_profit_ratio: 1.3,
  profit_callback: 0.1,
  double_first_order: false,
  loop_type: 'cycle',
  loop_count: 999,
  enable_add_position: true,
  flash_crash_protection: 2.0,
}

const ORDER_COUNT_OPTIONS = [
  { value: '5', label: '5 单' },
  { value: '6', label: '6 单' },
  { value: '7', label: '7 单' },
]

const LOOP_TYPE_OPTIONS = [
  { value: 'single', label: '单次' },
  { value: 'cycle', label: '循环' },
]

const DIRECTION_OPTIONS = [
  { value: 'long', label: '做多' },
  { value: 'short', label: '做空' },
  { value: 'dual', label: '双向' },
]

interface StrategyConfigPanelProps {
  initialData?: Partial<StrategyConfigFormValues>
  onSubmit: (data: MartinConfig | WallStreetConfig) => void
  onCancel?: () => void
  isLoading?: boolean
}

export const StrategyConfigPanel: React.FC<StrategyConfigPanelProps> = ({
  initialData,
  onSubmit,
  onCancel,
  isLoading = false,
}) => {
  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<StrategyConfigFormValues>({
    defaultValues: { ...DEFAULT_VALUES, ...initialData },
  })

  const strategyType = watch('strategy_type')
  const enableAddPosition = watch('enable_add_position')
  const doubleFirstOrder = watch('double_first_order')
  const loopType = watch('loop_type')
  const addPositionSpread = watch('add_position_spread')
  const addPositionCallback = watch('add_position_callback')
  const takeProfitRatio = watch('take_profit_ratio')
  const profitCallback = watch('profit_callback')
  const flashCrashProtection = watch('flash_crash_protection')

  useEffect(() => {
    if (initialData) {
      reset({ ...DEFAULT_VALUES, ...initialData })
    }
  }, [initialData, reset])

  const handleFormSubmit = (values: StrategyConfigFormValues) => {
    const base = {
      name: values.name,
      symbol: values.symbol,
      leverage: values.leverage,
      direction: values.direction,
      first_order_amount: values.first_order_amount,
      order_count: values.order_count,
      add_position_spread: values.add_position_spread,
      add_position_callback: values.add_position_callback,
      take_profit_ratio: values.take_profit_ratio,
      profit_callback: values.profit_callback,
      double_first_order: values.double_first_order,
      loop_type: values.loop_type,
      loop_count: values.loop_count,
      enable_add_position: values.enable_add_position,
      flash_crash_protection: values.flash_crash_protection,
    }

    if (values.strategy_type === 'martin') {
      onSubmit({ ...base, strategy_type: 'martin' } as MartinConfig)
    } else {
      onSubmit({ ...base, strategy_type: 'wallstreet' } as WallStreetConfig)
    }
  }

  const handleReset = () => {
    reset(DEFAULT_VALUES)
  }

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-5">
      {/* Strategy Type Selector */}
      <div className="flex gap-3">
        <button
          type="button"
          onClick={() => setValue('strategy_type', 'martin')}
          className={cn(
            'flex-1 flex items-center gap-2 rounded-xl border p-4 transition-all',
            strategyType === 'martin'
              ? 'border-[#f5222d]/40 bg-[#f5222d]/5'
              : 'border-[#2a2a2a] bg-[#111] hover:border-[#333]'
          )}
        >
          <TrendingUp className={cn('w-5 h-5', strategyType === 'martin' ? 'text-[#f5222d]' : 'text-[#555]')} />
          <div className="text-left">
            <div className={cn('text-sm font-medium', strategyType === 'martin' ? 'text-[#e0e0e0]' : 'text-[#888]')}>
              马丁策略
            </div>
            <div className="text-xs text-[#666]">倍投补仓 2,4,8,16,32,64</div>
          </div>
        </button>

        <button
          type="button"
          onClick={() => setValue('strategy_type', 'wallstreet')}
          className={cn(
            'flex-1 flex items-center gap-2 rounded-xl border p-4 transition-all',
            strategyType === 'wallstreet'
              ? 'border-[#faad14]/40 bg-[#faad14]/5'
              : 'border-[#2a2a2a] bg-[#111] hover:border-[#333]'
          )}
        >
          <BarChart3 className={cn('w-5 h-5', strategyType === 'wallstreet' ? 'text-[#faad14]' : 'text-[#555]')} />
          <div className="text-left">
            <div className={cn('text-sm font-medium', strategyType === 'wallstreet' ? 'text-[#e0e0e0]' : 'text-[#888]')}>
              华尔街策略
            </div>
            <div className="text-xs text-[#666]">等比补仓 1,2,3,5,8,13,21,34,55</div>
          </div>
        </button>
      </div>

      {/* Basic Info */}
      <SectionCard title="基本信息">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Input
            label="策略名称"
            placeholder="输入策略名称"
            error={errors.name?.message}
            {...register('name', { required: '请输入策略名称' })}
          />
          <Input
            label="交易对"
            placeholder="如: BTCUSDT"
            {...register('symbol', { required: '请输入交易对' })}
          />
          <Input
            label="杠杆倍数"
            type="number"
            min={1}
            max={125}
            {...register('leverage', {
              required: true,
              valueAsNumber: true,
              min: { value: 1, message: '最小1x' },
              max: { value: 125, message: '最大125x' },
            })}
            error={errors.leverage?.message}
          />
          <Select
            label="交易方向"
            options={DIRECTION_OPTIONS}
            {...register('direction')}
          />
        </div>
      </SectionCard>

      {/* Order Parameters */}
      <SectionCard title="做单参数">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Input
            label="首单金额 (USDT)"
            type="number"
            min={10}
            max={10000}
            helperText="范围: 10 - 10000 USDT"
            {...register('first_order_amount', {
              required: true,
              valueAsNumber: true,
              min: { value: 10, message: '最小10 USDT' },
              max: { value: 10000, message: '最大10000 USDT' },
            })}
            error={errors.first_order_amount?.message}
          />

          <Select
            label="做单数量"
            options={ORDER_COUNT_OPTIONS}
            {...register('order_count', {
              required: true,
              valueAsNumber: true,
            })}
            helperText="5-7 单可选"
          />

          <div className="sm:col-span-2">
            <div className="flex items-center gap-3 mb-4">
              <Switch
                label="开启补仓"
                checked={enableAddPosition}
                onChange={(e) => setValue('enable_add_position', e.target.checked)}
              />
              <Switch
                label="首单加倍"
                checked={doubleFirstOrder}
                onChange={(e) => setValue('double_first_order', e.target.checked)}
              />
            </div>
          </div>

          {enableAddPosition && (
            <>
              <div className="sm:col-span-2 space-y-4">
                <Slider
                  label="补仓价差"
                  min={0.5}
                  max={50}
                  step={0.1}
                  value={addPositionSpread}
                  onChange={(v) => setValue('add_position_spread', v)}
                  valueFormatter={(v) => `${v}%`}
                />
                <Slider
                  label="补仓回调"
                  min={0.01}
                  max={0.5}
                  step={0.01}
                  value={addPositionCallback}
                  onChange={(v) => setValue('add_position_callback', v)}
                  valueFormatter={(v) => `${v}%`}
                />
              </div>
            </>
          )}
        </div>
      </SectionCard>

      {/* Take Profit */}
      <SectionCard title="止盈参数">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Input
            label="止盈比例"
            type="number"
            step={0.1}
            min={0.1}
            max={10}
            helperText="推荐值: 1.3%"
            {...register('take_profit_ratio', {
              required: true,
              valueAsNumber: true,
              min: { value: 0.1, message: '最小0.1%' },
              max: { value: 10, message: '最大10%' },
            })}
            error={errors.take_profit_ratio?.message}
          />
          <div>
            <Slider
              label="盈利回调"
              min={0.01}
              max={0.5}
              step={0.01}
              value={profitCallback}
              onChange={(v) => setValue('profit_callback', v)}
              valueFormatter={(v) => `${v}%`}
            />
          </div>
        </div>
      </SectionCard>

      {/* Loop Settings */}
      <SectionCard title="循环设置">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Select
            label="循环类型"
            options={LOOP_TYPE_OPTIONS}
            {...register('loop_type')}
          />
          {loopType === 'cycle' && (
            <Input
              label="循环次数"
              type="number"
              min={1}
              max={9999}
              helperText={loopType === 'cycle' ? '0 = 无限循环' : undefined}
              {...register('loop_count', {
                required: true,
                valueAsNumber: true,
                min: { value: 1, message: '最小1次' },
              })}
              error={errors.loop_count?.message}
            />
          )}
        </div>
      </SectionCard>

      {/* Protection */}
      <SectionCard title="风控设置">
        <div className="space-y-4">
          <Input
            label="防瀑布比例"
            type="number"
            step={0.1}
            min={0}
            max={10}
            helperText="价格急跌超过此比例暂停开仓 (0 = 关闭)"
            {...register('flash_crash_protection', {
              required: true,
              valueAsNumber: true,
              min: { value: 0, message: '最小0%' },
              max: { value: 10, message: '最大10%' },
            })}
            error={errors.flash_crash_protection?.message}
          />
          <div className="flex flex-wrap gap-2">
            {flashCrashProtection > 0 && (
              <Badge variant="info" dot>防瀑布保护已启用: {flashCrashProtection}%</Badge>
            )}
            {enableAddPosition && (
              <Badge variant="success" dot>补仓已启用</Badge>
            )}
            {doubleFirstOrder && (
              <Badge variant="warning" dot>首单加倍</Badge>
            )}
          </div>
        </div>
      </SectionCard>

      {/* Actions */}
      <div className="flex items-center gap-3 pt-2">
        <Button
          type="submit"
          variant="primary"
          isLoading={isLoading}
          leftIcon={<Save className="w-4 h-4" />}
        >
          保存配置
        </Button>
        <Button
          type="button"
          variant="ghost"
          onClick={handleReset}
          leftIcon={<RotateCcw className="w-4 h-4" />}
        >
          重置
        </Button>
        {onCancel && (
          <Button type="button" variant="ghost" onClick={onCancel}>
            取消
          </Button>
        )}
      </div>
    </form>
  )
}

export default StrategyConfigPanel
