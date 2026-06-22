import React, { useState, useEffect, useMemo } from 'react'
import {
  ArrowLeft, X, Bot, Radio, Sliders, CheckCircle2,
  ChevronRight, Sparkles, AlertCircle, Shield
} from 'lucide-react'
import { aiBotApi, strategyApi, configApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { Badge } from '@/components/ui/Badge'
import type { AIBotCatalogItem, AIBotCreateRequest, AIBotInstance } from '@/types'

interface AIBotCreateWizardProps {
  open: boolean
  initialCatalog?: AIBotCatalogItem | null
  editInstance?: AIBotInstance
  onClose: () => void
  onCreated: () => void
  onUpdated: () => void
}

const STRATEGY_OPTIONS = [
  { value: 'optimus', label: 'Optimus 稳定现货' },
  { value: 'cyberbot', label: 'CyberBot 熊市防御' },
  { value: 'mono_optimus', label: 'Mono Optimus' },
  { value: 'mono_cyberbot', label: 'Mono CyberBot' },
  { value: 'crypto_future', label: 'Crypto Future 合约' },
  { value: 'ai_alpha', label: 'AI Alpha' },
  { value: 'ai_alpha_futures', label: 'AI Alpha Futures' },
  { value: 'terminator_volatility', label: 'Terminator Volatility' },
  { value: 'alt_volatility', label: 'ALT+ Volatility' },
  { value: 'trade_holder', label: 'Trade Holder 长期' },
  { value: 'noah', label: 'Noah 高流动性' },
]

const MARKET_OPTIONS = [
  { value: 'spot', label: '现货' },
  { value: 'futures', label: '合约' },
]

const EXEC_OPTIONS = [
  { value: 'paper', label: '模拟交易 (Paper)' },
  { value: 'live', label: '实盘交易 (Live)' },
  { value: 'signal', label: '仅信号 (Signal)' },
]

const RISK_OPTIONS = [
  { value: 'low', label: '保守', desc: '低波动、小仓位' },
  { value: 'medium', label: '稳健', desc: '均衡风险收益' },
  { value: 'high', label: '激进', desc: '高波动、大仓位' },
]

export const AIBotCreateWizard: React.FC<AIBotCreateWizardProps> = ({
  open,
  initialCatalog,
  editInstance,
  onClose,
  onCreated,
  onUpdated,
}) => {
  const isEdit = !!editInstance
  const [step, setStep] = useState(0)
  const [form, setForm] = useState<AIBotCreateRequest>({
    name: '',
    strategy_type: 'optimus',
    symbol: 'BTCUSDT',
    market_type: 'spot',
    execution_mode: 'paper',
    config_json: '{}',
    exchange_id: '',
    initial_balance: 10000,
    leverage: 1,
    risk_level: 'medium',
  })
  const [catalog, setCatalog] = useState<AIBotCatalogItem[]>([])
  const [selectedCatalogId, setSelectedCatalogId] = useState<string>('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [jsonError, setJsonError] = useState<string | null>(null)
  const [paramDefs, setParamDefs] = useState<Record<string, any>>({})
  const [exchanges, setExchanges] = useState<Array<{ key: string; label: string }>>([])
  const [configMap, setConfigMap] = useState<Record<string, any>>({})

  const steps = isEdit ? ['参数配置', '确认保存'] : ['选择来源', '交易标的', '参数配置', '确认部署']

  useEffect(() => {
    if (!open) return
    aiBotApi.catalog().then((items) => setCatalog(items))
    strategyApi.paramDefs(form.strategy_type || 'optimus').then((defs) => {
      const map: Record<string, boolean> = {}
      defs.params.forEach((p: any) => { map[p.name] = true })
      setParamDefs(map)
    }).catch(() => setParamDefs({}))
    configApi.getExchanges().then((res) => {
      const list = (res.exchanges || []).filter((e: any) => e.status === 'active' || e.configured)
      setExchanges(list.map((e: any) => ({ key: e.key, label: e.label || e.key })))
    }).catch(() => setExchanges([]))
  }, [open])

  useEffect(() => {
    if (!open) return
    if (editInstance) {
      let cfg: Record<string, any> = {}
      try {
        cfg = JSON.parse(editInstance.config_json || '{}')
      } catch { cfg = {} }
      setConfigMap(cfg)
      setForm({
        name: editInstance.name,
        strategy_type: editInstance.strategy_type,
        symbol: editInstance.symbol,
        market_type: editInstance.market_type,
        execution_mode: editInstance.execution_mode,
        config_json: editInstance.config_json || '{}',
        exchange_id: editInstance.exchange_id || '',
        initial_balance: editInstance.initial_balance || cfg.initial_balance || 10000,
        leverage: cfg.leverage || 1,
        risk_level: cfg.risk_level || 'medium',
      })
      setSelectedCatalogId(editInstance.catalog_id || '')
    } else if (initialCatalog) {
      let cfg: Record<string, any> = {}
      try {
        cfg = JSON.parse(initialCatalog.config_json || '{}')
      } catch { cfg = {} }
      setConfigMap(cfg)
      setSelectedCatalogId(initialCatalog.id)
      setForm((f) => ({
        ...f,
        catalog_id: initialCatalog.id,
        name: initialCatalog.name,
        strategy_type: initialCatalog.strategy_type as any,
        market_type: initialCatalog.market_type,
        risk_level: initialCatalog.risk_level,
        leverage: cfg.leverage || 1,
        initial_balance: cfg.initial_balance || 10000,
      }))
    } else {
      setForm({
        name: '',
        strategy_type: 'optimus',
        symbol: 'BTCUSDT',
        market_type: 'spot',
        execution_mode: 'paper',
        config_json: '{}',
        exchange_id: '',
        initial_balance: 10000,
        leverage: 1,
        risk_level: 'medium',
      })
      setSelectedCatalogId('')
      setConfigMap({})
    }
    setStep(0)
    setJsonError(null)
  }, [initialCatalog, editInstance, open])

  useEffect(() => {
    if (!form.strategy_type) return
    strategyApi.paramDefs(form.strategy_type).then((defs) => {
      const map: Record<string, boolean> = {}
      defs.params.forEach((p: any) => { map[p.name] = true })
      setParamDefs(map)
    }).catch(() => setParamDefs({}))
  }, [form.strategy_type])

  const selectedCatalog = useMemo(() =>
    catalog.find((c) => c.id === selectedCatalogId),
    [catalog, selectedCatalogId]
  )

  const validateJSON = (value: string): boolean => {
    try {
      JSON.parse(value)
      setJsonError(null)
      return true
    } catch (e: any) {
      setJsonError('JSON 格式错误: ' + e.message)
      return false
    }
  }

  const validateConfigKeys = () => {
    const parsed = JSON.parse(form.config_json || '{}')
    const invalid: string[] = []
    Object.keys(parsed).forEach((k) => {
      if (!['initial_balance', 'leverage', 'risk_level', 'take_profit_pct', 'stop_loss_pct', 'max_positions', 'position_size_pct'].includes(k) && !paramDefs[k]) {
        invalid.push(k)
      }
    })
    if (invalid.length > 0) {
      setJsonError(`未知参数: ${invalid.join(', ')}`)
      return false
    }
    return true
  }

  const handleNext = () => {
    if (step === 2 && !isEdit) {
      if (!validateJSON(form.config_json || '{}')) return
      if (!validateConfigKeys()) return
    }
    if (step < steps.length - 1) setStep((s) => s + 1)
  }

  const handleBack = () => {
    if (step > 0) setStep((s) => s - 1)
  }

  const buildConfigJSON = () => {
    const cfg: Record<string, any> = {
      ...configMap,
      initial_balance: form.initial_balance,
      leverage: form.leverage,
      risk_level: form.risk_level,
    }
    try {
      const userCfg = JSON.parse(form.config_json || '{}')
      Object.assign(cfg, userCfg)
    } catch { /* ignore invalid user json on deploy, already validated */ }
    return JSON.stringify(cfg)
  }

  const handleDeploy = async () => {
    if (!validateJSON(form.config_json || '{}')) return
    if (!validateConfigKeys()) return
    setIsSubmitting(true)
    try {
      const payload = {
        ...form,
        config_json: buildConfigJSON(),
      }
      if (isEdit && editInstance) {
        await aiBotApi.update(editInstance.id, payload)
        onUpdated()
      } else {
        await aiBotApi.create(payload)
        onCreated()
      }
      setStep(0)
      setForm({
        name: '',
        strategy_type: 'optimus',
        symbol: 'BTCUSDT',
        market_type: 'spot',
        execution_mode: 'paper',
        config_json: '{}',
        exchange_id: '',
        initial_balance: 10000,
        leverage: 1,
        risk_level: 'medium',
      })
      setSelectedCatalogId('')
      setConfigMap({})
    } finally {
      setIsSubmitting(false)
    }
  }

  const updateForm = (key: keyof AIBotCreateRequest, value: any) => {
    setForm((f) => ({ ...f, [key]: value }))
  }

  const updateConfigJSON = (value: string) => {
    setForm((f) => ({ ...f, config_json: value }))
    validateJSON(value)
  }

  if (!open) return null

  return (
    <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
      <div className="flex w-full max-w-2xl flex-col rounded-2xl border border-[#2a2a2a] bg-[#111111] shadow-2xl max-h-[90vh] sm:max-h-[90vh]">
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
            <h3 className="text-base font-semibold text-white">{isEdit ? '编辑 AI Bot' : '创建 AI Bot'}</h3>
          </div>
          <button
            onClick={onClose}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[#999999] transition-colors hover:bg-[#1c1c1c] hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Step indicator */}
        <div className="px-6 pt-5">
          <div className="flex items-center gap-2 flex-wrap">
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
          {!isEdit && step === 0 && (
            <div className="space-y-4">
              <p className="text-sm text-[#999]">选择机器人来源：从内置市场部署，或创建自定义机器人。</p>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <SourceCard
                  active={!selectedCatalogId}
                  icon={<Sparkles className="w-5 h-5" />}
                  title="自定义机器人"
                  desc="自己选择策略类型、交易对和参数"
                  onClick={() => {
                    setSelectedCatalogId('')
                    setForm((f) => ({ ...f, catalog_id: undefined }))
                  }}
                />
                {catalog.filter((c) => c.strategy_type !== 'signal_provider').map((bot) => (
                  <SourceCard
                    key={bot.id}
                    active={selectedCatalogId === bot.id}
                    icon={<Bot className="w-5 h-5" />}
                    title={bot.name}
                    desc={bot.description || ''}
                    onClick={() => {
                      let cfg: Record<string, any> = {}
                      try { cfg = JSON.parse(bot.config_json || '{}') } catch { cfg = {} }
                      setConfigMap(cfg)
                      setSelectedCatalogId(bot.id)
                      setForm((f) => ({
                        ...f,
                        catalog_id: bot.id,
                        name: bot.name,
                        strategy_type: bot.strategy_type as any,
                        market_type: bot.market_type,
                        risk_level: bot.risk_level,
                        leverage: cfg.leverage || 1,
                        initial_balance: cfg.initial_balance || 10000,
                      }))
                    }}
                  />
                ))}
              </div>
            </div>
          )}

          {(isEdit ? step === 0 : step === 1) && (
            <div className="space-y-4">
              <Field label="机器人名称">
                <Input
                  value={form.name || ''}
                  onChange={(e) => updateForm('name', e.target.value)}
                  placeholder="例如：BTC 网格策略"
                />
              </Field>
              <Field label="交易对">
                <Input
                  value={form.symbol || ''}
                  onChange={(e) => updateForm('symbol', e.target.value.toUpperCase())}
                  placeholder="BTCUSDT"
                />
              </Field>
              <div className="grid grid-cols-2 gap-4">
                <Field label="市场类型">
                  <div className="grid grid-cols-2 gap-2">
                    {MARKET_OPTIONS.map((m) => (
                      <button
                        key={m.value}
                        onClick={() => updateForm('market_type', m.value)}
                        className={cn(
                          'px-3 py-2 rounded-lg text-xs border transition-colors',
                          form.market_type === m.value
                            ? 'bg-[#1890ff]/10 border-[#1890ff]/30 text-[#1890ff]'
                            : 'bg-[#0a0a0a] border-[#1c1c1c] text-[#888] hover:border-[#333]'
                        )}
                      >
                        {m.label}
                      </button>
                    ))}
                  </div>
                </Field>
                <Field label="执行模式">
                  <select
                    value={form.execution_mode}
                    onChange={(e) => updateForm('execution_mode', e.target.value)}
                    className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#1890ff]/40"
                  >
                    {EXEC_OPTIONS.map((o) => (
                      <option key={o.value} value={o.value}>{o.label}</option>
                    ))}
                  </select>
                </Field>
              </div>
            </div>
          )}

          {(isEdit ? step === 0 : step === 2) && (
            <div className="space-y-4">
              <Field label="策略类型">
                <select
                  value={form.strategy_type}
                  onChange={(e) => updateForm('strategy_type', e.target.value)}
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#1890ff]/40"
                >
                  {STRATEGY_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </Field>

              <Field label="风险等级">
                <div className="grid grid-cols-3 gap-2">
                  {RISK_OPTIONS.map((r) => (
                    <button
                      key={r.value}
                      onClick={() => updateForm('risk_level', r.value)}
                      className={cn(
                        'px-3 py-2 rounded-lg text-xs border transition-colors text-left',
                        form.risk_level === r.value
                          ? 'bg-[#1890ff]/10 border-[#1890ff]/30 text-[#1890ff]'
                          : 'bg-[#0a0a0a] border-[#1c1c1c] text-[#888] hover:border-[#333]'
                      )}
                    >
                      <div className="font-medium">{r.label}</div>
                      <div className="text-[10px] opacity-70 mt-0.5">{r.desc}</div>
                    </button>
                  ))}
                </div>
              </Field>

              <div className="grid grid-cols-2 gap-4">
                <Field label="初始余额 (USDT)">
                  <Input
                    type="number"
                    value={form.initial_balance || 10000}
                    onChange={(e) => updateForm('initial_balance', Number(e.target.value))}
                    placeholder="10000"
                  />
                </Field>
                {form.market_type === 'futures' && (
                  <Field label="杠杆倍数">
                    <Input
                      type="number"
                      min={1}
                      max={125}
                      value={form.leverage || 1}
                      onChange={(e) => updateForm('leverage', Math.min(125, Math.max(1, Number(e.target.value))))}
                      placeholder="1"
                    />
                  </Field>
                )}
              </div>

              <Field label="交易所">
                <select
                  value={form.exchange_id || ''}
                  onChange={(e) => updateForm('exchange_id', e.target.value)}
                  className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#1890ff]/40"
                >
                  <option value="">默认 / 未指定</option>
                  {exchanges.map((e) => (
                    <option key={e.key} value={e.key}>{e.label}</option>
                  ))}
                </select>
              </Field>

              <Field label="高级配置 JSON (可选)">
                <textarea
                  value={form.config_json || '{}'}
                  onChange={(e) => updateConfigJSON(e.target.value)}
                  onBlur={() => validateJSON(form.config_json || '{}')}
                  placeholder='{"timeframe":"1h","first_order_amount":100}'
                  className={cn(
                    "h-32 w-full resize-none rounded-lg border bg-[#0a0a0a] p-3 text-sm text-white placeholder-[#444] outline-none focus:border-[#1890ff]/40 font-mono",
                    jsonError ? 'border-[#f5222d]' : 'border-[#1c1c1c]'
                  )}
                />
                {jsonError && (
                  <div className="flex items-center gap-1.5 text-xs text-[#f5222d] mt-1.5">
                    <AlertCircle className="w-3 h-3" />
                    {jsonError}
                  </div>
                )}
              </Field>
            </div>
          )}

          {(isEdit ? step === 1 : step === 3) && (
            <div className="space-y-4">
              <div className="rounded-xl bg-[#0a0a0a] border border-[#1c1c1c] p-4 space-y-2">
                <ConfirmRow label="名称" value={form.name || '未命名'} />
                <ConfirmRow label="策略" value={form.strategy_type} />
                <ConfirmRow label="交易对" value={form.symbol} />
                <ConfirmRow label="市场" value={form.market_type === 'spot' ? '现货' : '合约'} />
                <ConfirmRow
                  label="执行模式"
                  value={form.execution_mode === 'paper' ? '模拟' : form.execution_mode === 'live' ? '实盘' : '仅信号'}
                />
                <ConfirmRow label="风险等级" value={RISK_OPTIONS.find((r) => r.value === form.risk_level)?.label || form.risk_level} />
                <ConfirmRow label="初始余额" value={`$${form.initial_balance}`} />
                {form.market_type === 'futures' && <ConfirmRow label="杠杆" value={`${form.leverage}x`} />}
                {form.exchange_id && <ConfirmRow label="交易所" value={form.exchange_id} />}
                {selectedCatalog && (
                  <ConfirmRow
                    label="费用"
                    value={selectedCatalog.fee_model === 'free' ? '免费' : selectedCatalog.fee_model === 'monthly' ? `$${selectedCatalog.monthly_fee}/月` : `盈利 ${selectedCatalog.fee_percent}%`}
                  />
                )}
              </div>
              <div className="flex items-start gap-2 text-xs text-[#888]">
                <AlertCircle className="w-4 h-4 shrink-0 text-[#faad14]" />
                <span>
                  {isEdit
                    ? '点击"保存"后将更新机器人配置。运行中的机器人需先停止才能修改。'
                    : '点击"部署"后将创建机器人实例。若选择实盘模式，请确保已配置交易所 API 并开启交易权限。'}
                </span>
              </div>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="shrink-0 flex items-center justify-end gap-2 border-t border-[#1c1c1c] px-6 py-4">
          {step < steps.length - 1 ? (
            <Button onClick={handleNext} rightIcon={<ChevronRight className="w-4 h-4" />}>下一步</Button>
          ) : (
            <Button
              onClick={handleDeploy}
              isLoading={isSubmitting}
              leftIcon={<CheckCircle2 className="w-4 h-4" />}
            >
              {isEdit ? '保存修改' : '部署机器人'}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

function SourceCard({
  active,
  icon,
  title,
  desc,
  onClick,
}: {
  active: boolean
  icon: React.ReactNode
  title: string
  desc: string
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'flex flex-col items-start gap-2 rounded-xl border p-4 text-left transition-colors',
        active
          ? 'border-[#1890ff]/50 bg-[#1890ff]/5'
          : 'border-[#1c1c1c] bg-[#0a0a0a] hover:border-[#333]'
      )}
    >
      <div className={cn('w-8 h-8 rounded-lg flex items-center justify-center', active ? 'bg-[#1890ff]/20 text-[#1890ff]' : 'bg-[#1c1c1c] text-[#888]')}>
        {icon}
      </div>
      <div className="text-sm font-medium text-white">{title}</div>
      <div className="text-xs text-[#666] line-clamp-2">{desc}</div>
    </button>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <label className="text-xs font-medium text-[#888]">{label}</label>
      {children}
    </div>
  )
}

function ConfirmRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-[#999]">{label}</span>
      <span className="font-medium text-white">{value}</span>
    </div>
  )
}

export default AIBotCreateWizard
