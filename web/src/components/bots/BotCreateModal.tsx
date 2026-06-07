import React, { useState, useCallback } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, X, RefreshCw, Sparkles } from 'lucide-react'
import { strategyApi, aiApi } from '@/lib/api'
import { cn, formatCurrency } from '@/lib/utils'
import type { StrategyItem } from '@/types'
import type { BotItem } from '@/hooks/useBotData'
import { BOT_TYPES, BOT_TYPE_TO_STRATEGY_TYPE } from '@/hooks/useBotData'
import { WizardField, BotParamForm } from './BotParamForm'

export function AiCreateDialog({
  open,
  onClose,
  onApply,
}: {
  open: boolean
  onClose: () => void
  onApply: (preset: { botType: BotItem['bot_type']; description: string; params?: Record<string, unknown> }) => void
}) {
  const [prompt, setPrompt] = useState('')
  const [isGenerating, setIsGenerating] = useState(false)

  const handleSubmit = useCallback(async () => {
    if (!prompt.trim()) return
    setIsGenerating(true)
    try {
      const res = await aiApi.chat(prompt)
      const recommendation = {
        botType: (res?.botType as BotItem['bot_type']) || 'grid',
        description: prompt,
        params: (res?.params as Record<string, unknown>) || {},
      }
      onApply(recommendation)
    } catch {
      onApply({ botType: 'grid', description: prompt })
    } finally {
      setIsGenerating(false)
      setPrompt('')
    }
  }, [prompt, onApply])
  if (!open) return null
  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="w-full max-w-lg rounded-2xl border border-[#2a2a2a] bg-[#111111] p-6 shadow-2xl">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Sparkles className="h-5 w-5 text-[#4f6ed1]" />
            <h3 className="text-base font-semibold text-white">AI 智能创建机器人</h3>
          </div>
          <button
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[#999999] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <p className="mt-2 text-sm text-[#999999]">
          描述你的交易目标、风险偏好和标的，AI 将为你推荐最优策略类型与参数。
        </p>
        <div className="mt-4">
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="例如：我想在 BTC/USDT 上做一个低风险的网格策略，投入 5000 USDT，价格区间 25000-35000"
            className="h-32 w-full resize-none rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-3 text-sm text-white placeholder-[#444444] outline-none transition-colors focus:border-[#4f6ed1]/40"
          />
        </div>
        <div className="mt-4 flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded-lg border border-[#1c1c1c] bg-[#141414] px-4 py-2 text-sm font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={!prompt.trim() || isGenerating}
            className="flex items-center gap-1.5 rounded-lg bg-[#4f6ed1] px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {isGenerating && <RefreshCw className="h-3.5 w-3.5 animate-spin" />}
            {isGenerating ? '生成中...' : '生成策略'}
          </button>
        </div>
      </div>
    </div>
  )
}

function ConfirmRow({ label, value }: { label: string; value?: React.ReactNode }) {
  return (<div className="flex items-center justify-between text-sm"><span className="text-[#999999]">{label}</span><span className="font-medium text-white">{value}</span></div>)
}

export function BotCreateModal({
  open,
  botType,
  aiPreset,
  editBot,
  onCancel,
  onCreated,
  onUpdated,
}: {
  open: boolean
  botType: BotItem['bot_type']
  aiPreset: { botType: BotItem['bot_type']; description: string; params?: Record<string, unknown> } | null
  editBot: BotItem | null
  onCancel: () => void
  onCreated: () => void
  onUpdated: () => void
}) {
  const queryClient = useQueryClient()
  const [step, setStep] = useState(0)
  const [form, setForm] = useState<Record<string, unknown>>({})

  const isEdit = !!editBot
  const effectiveType = aiPreset?.botType || botType || 'grid'

  const createMutation = useMutation({
    mutationFn: (data: Partial<StrategyItem>) => strategyApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      onCreated()
      setStep(0)
      setForm({})
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<StrategyItem> }) => strategyApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['strategies'] })
      onUpdated()
      setStep(0)
      setForm({})
    },
  })

  const steps = ['选择类型', '交易标的', '参数配置', '确认创建']

  const handleNext = () => {
    if (step < steps.length - 1) {
      setStep((s) => s + 1)
    } else {
      const payload: Partial<StrategyItem> & Record<string, unknown> = {
        strategy_name: (form.name as string) || '未命名机器人',
        strategy_type: BOT_TYPE_TO_STRATEGY_TYPE[effectiveType] || effectiveType,
        strategy_mode: 'bot' as const,
        bot_type: effectiveType,
        market_category: (form.market as string) || 'crypto',
        execution_mode: (form.execution as 'live' | 'paper' | 'signal') || 'paper',
        trading_config: {
          symbol: form.symbol,
          initial_capital: Number(form.capital) || 1000,
          timeframe: form.timeframe || '1h',
          bot_type: effectiveType,
          bot_params: { ...aiPreset?.params, ...form },
          order_count: Number(form.order_count) || 7,
          first_order_amount: Number(form.first_order_amount) || 100,
          add_position_spread: Number(form.add_position_spread) || 3,
          add_position_callback: Number(form.add_position_callback) || 0.1,
          take_profit_ratio: Number(form.take_profit_ratio) || 1.3,
          profit_callback: Number(form.profit_callback) || 0.1,
          trade_count_mode: (form.trade_count_mode as string) || 'cycle',
          open_indicator: (form.open_indicator as string) || 'macd_golden',
          add_position_indicator: (form.add_position_indicator as string) || 'macd',
          waterfall_protection: Number(form.waterfall_protection) || 2,
          open_double: !!form.open_double,
          trend_indicator: !!form.trend_indicator,
          trend_timeframe: (form.trend_timeframe as string) || '15m',
          take_profit_method: (form.take_profit_method as string) || 'full',
          reverse_take_profit: !!form.reverse_take_profit,
          reverse_stop_loss: !!form.reverse_stop_loss,
          follow_trend: !!form.follow_trend,
          follow_trend_max: Number(form.follow_trend_max) || 5,
          burn_cut: (form.burn_cut as { enabled: boolean; dual_burn_start: number; global_burn_start: number } | boolean | undefined) || { enabled: false, dual_burn_start: 3, global_burn_start: 5 },
          custom_reduce: !!form.custom_reduce,
          online_order_limit: Number(form.online_order_limit) || 10,
          profit_protection: !!form.profit_protection,
          close_add_position: !!form.close_add_position,
        },
      }
      if (isEdit && editBot) {
        updateMutation.mutate({ id: editBot.id, data: payload })
      } else {
        createMutation.mutate(payload)
      }
    }
  }

  const handleBack = () => {
    if (step > 0) setStep((s) => s - 1)
  }

  if (!open) return null

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="flex w-full max-w-xl flex-col rounded-2xl border border-[#2a2a2a] bg-[#111111] shadow-2xl max-h-[90vh]">
        {/* Header */}
        <div className="shrink-0 flex items-center justify-between border-b border-[#1c1c1c] px-6 py-4">
          <div className="flex items-center gap-3">
            {step > 0 && (
              <button
                onClick={handleBack}
                className="flex h-8 w-8 items-center justify-center rounded-lg text-[#999999] transition-colors hover:bg-[#1c1c1c] hover:text-white"
              >
                <ArrowLeft className="h-4 w-4" />
              </button>
            )}
            <h3 className="text-base font-semibold text-white">
              {isEdit ? '编辑机器人' : '创建机器人'}
            </h3>
          </div>
          <button
            onClick={onCancel}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[#999999] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Step indicator */}
        <div className="px-6 pt-5">
          <div className="flex items-center gap-2">
            {steps.map((s, i) => (
              <React.Fragment key={s}>
                <div
                  className={cn(
                    'flex h-7 items-center rounded-full px-2.5 text-[11px] font-medium transition-colors',
                    i === step
                      ? 'bg-white text-[#0a0a0a]'
                      : i < step
                        ? 'bg-[#1c1c1c] text-[#888888]'
                        : 'bg-[#141414] text-[#8a8a8a]'
                  )}
                >
                  {i + 1}. {s}
                </div>
                {i < steps.length - 1 && (
                  <div className={cn('h-px w-4', i < step ? 'bg-[#888888]' : 'bg-[#1c1c1c]')} />
                )}
              </React.Fragment>
            ))}
          </div>
        </div>

        {/* Body */}
        <div className="overflow-y-auto px-6 py-5">
          {step === 0 && (
            <div className="space-y-3">
              <p className="text-sm text-[#999999]">当前选择的策略类型：</p>
              <div className="flex items-center gap-3 rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4">
                <div
                  className="flex h-10 w-10 items-center justify-center rounded-lg"
                  style={{
                    background: BOT_TYPES.find((b) => b.key === effectiveType)?.bg,
                    color: BOT_TYPES.find((b) => b.key === effectiveType)?.color,
                  }}
                >
                  {BOT_TYPES.find((b) => b.key === effectiveType)?.icon}
                </div>
                <div>
                  <div className="text-sm font-semibold text-white">
                    {BOT_TYPES.find((b) => b.key === effectiveType)?.label}
                  </div>
                  <div className="text-xs text-[#999999]">
                    {BOT_TYPES.find((b) => b.key === effectiveType)?.desc}
                  </div>
                </div>
              </div>
              {aiPreset && (
                <div className="rounded-lg border border-[#4f6ed1]/20 bg-[#4f6ed1]/[0.06] p-3 text-xs text-[#888888]">
                  <span className="font-medium text-[#4f6ed1]">AI 推荐：</span>
                  {aiPreset.description}
                </div>
              )}
            </div>
          )}

          {step === 1 && (
            <div className="space-y-4">
              <WizardField label="交易标的" hint="例如 BTC/USDT">
                <input
                  value={(form.symbol as string) || ''}
                  onChange={(e) => setForm((f) => ({ ...f, symbol: e.target.value }))}
                  placeholder="BTC/USDT"
                  aria-label="交易标的"
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444444] outline-none transition-colors focus:border-[#4f6ed1]/40"
                />
              </WizardField>
              <WizardField label="市场类型">
                <div className="flex gap-2">
                  {['spot', 'futures'].map((m) => (
                    <button
                      key={m}
                      onClick={() => setForm((f) => ({ ...f, market: m }))}
                      className={cn(
                        'rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                        (form.market as string) === m
                          ? 'border-white/20 bg-white/10 text-white'
                          : 'border-[#1c1c1c] bg-[#141414] text-[#999999] hover:text-[#888888]'
                      )}
                    >
                      {m === 'spot' ? '现货' : '合约'}
                    </button>
                  ))}
                </div>
              </WizardField>
            </div>
          )}

          {step === 2 && (
            <div className="space-y-4">
              <WizardField label="初始资金 (USDT)">
                <input
                  type="number"
                  value={(form.capital as number) || ''}
                  onChange={(e) => setForm((f) => ({ ...f, capital: e.target.value }))}
                  placeholder="5000"
                  aria-label="初始资金 USDT"
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444444] outline-none focus:border-[#4f6ed1]/40"
                />
              </WizardField>
              <WizardField label="执行模式">
                <div className="flex gap-2">
                  {[
                    { key: 'paper', label: '模拟盘' },
                    { key: 'signal', label: '信号模式' },
                    { key: 'live', label: '实盘' },
                  ].map((m) => (
                    <button
                      key={m.key}
                      onClick={() => setForm((f) => ({ ...f, execution: m.key }))}
                      className={cn(
                        'rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                        (form.execution as string) === m.key
                          ? 'border-white/20 bg-white/10 text-white'
                          : 'border-[#1c1c1c] bg-[#141414] text-[#999999] hover:text-[#888888]'
                      )}
                    >
                      {m.label}
                    </button>
                  ))}
                </div>
              </WizardField>
              <WizardField label="名称">
                <input
                  value={(form.name as string) || ''}
                  onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                  placeholder="我的策略 #1"
                  aria-label="机器人名称"
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white placeholder-[#444444] outline-none focus:border-[#4f6ed1]/40"
                />
              </WizardField>

              <BotParamForm form={form} setForm={setForm} effectiveType={effectiveType} />
            </div>
          )}

          {step === 3 && (
            <div className="space-y-3">
              <h4 className="text-sm font-semibold text-white">配置确认</h4>
              <div className="rounded-xl border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-2">
                <ConfirmRow label="策略类型" value={BOT_TYPES.find((b) => b.key === effectiveType)?.label} />
                <ConfirmRow label="交易标的" value={(form.symbol as string) || '-'} />
                <ConfirmRow label="初始资金" value={`$${formatCurrency(Number(form.capital) || 0)}`} />
                <ConfirmRow
                  label="执行模式"
                  value={
                    { paper: '模拟盘', signal: '信号模式', live: '实盘' }[(form.execution as string) || 'paper']
                  }
                />
                <ConfirmRow label="名称" value={(form.name as string) || '未命名'} />
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 border-t border-[#1c1c1c] px-6 py-4">
          <button
            onClick={onCancel}
            className="rounded-lg border border-[#1c1c1c] bg-[#141414] px-4 py-2 text-sm font-medium text-[#888888] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            取消
          </button>
          <button
            onClick={handleNext}
            disabled={createMutation.isPending || updateMutation.isPending}
            className="flex items-center gap-1.5 rounded-lg bg-white px-4 py-2 text-sm font-medium text-[#0a0a0a] transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {(createMutation.isPending || updateMutation.isPending) && (
              <RefreshCw className="h-3.5 w-3.5 animate-spin" />
            )}
            {step === steps.length - 1 ? (isEdit ? '保存修改' : '创建机器人') : '下一步'}
          </button>
        </div>
      </div>
    </div>
  )
}
