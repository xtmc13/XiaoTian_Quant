import { forwardRef, useImperativeHandle, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, formatCurrency } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import { strategyApi, configApi } from '@/lib/api'
import { SectionCard } from '@/components/ui/SectionCard'
import { ExchangeSelectModal } from './ExchangeSelectModal'
import { IndicatorPicker, type IndicatorSelection } from './IndicatorPicker'
import { buildOpenIndicatorConfig } from './indicatorPresets'
import { Activity, AlertTriangle, CheckCircle2, Globe } from 'lucide-react'

/**
 * 现货策略专用创建表单（壳—指标—运行 收官：现货/合约两套独立参数表单）。
 * 与合约表单（StrategyCreateForm/CRAParamForm）完全独立，参数模型 =
 * 价格区间 + 格数 + 每格金额 + 循环模式 + 手续费率 + 开仓指标门控（现货版
 * IndicatorPicker，无顺势多/空）+ 执行设置（同样走 live_enabled 总闸）。
 *
 * config_json 双键设计（回填时自定义键优先）：
 * - 自定义键：price_lower / price_upper / grid_count / per_grid_amount /
 *   loop_mode / fee_rate（UI 显示与回填的数据源）；
 * - CRA 引擎映射键：first_order_amount=每格金额、order_count=格数、
 *   add_positions=由区间/格数生成的等差下跌补仓 ladder（spread_i = i×单格跌幅），
 *   take_profit_ratio=单格跌幅（一格利润即止盈）、direction 固定 long ——
 *   保证 cra_spot 引擎无需改动即可真实跑起来（区间下沿即 ladder 末端，作
 *   止损参考语义）。
 */

export interface SpotStrategyInitial {
  name?: string
  symbol?: string
  /** 策略类型预设（编辑回填；不在预设列表则回退 cra_spot）。 */
  strategyType?: string
  initialCapital?: number
  selectedExchanges?: string[]
  executionMode?: 'paper' | 'live'
  notifyChannels?: string[]
  // 自定义键优先；缺省时从 CRA 映射键回退推导。
  priceLower?: number
  priceUpper?: number
  gridCount?: number
  perGridAmount?: number
  loopMode?: 'single' | 'cycle'
  feeRate?: number
  indicator?: IndicatorSelection['indicator']
  indicatorParams?: Record<string, number | string>
  indicatorCustom?: { code_id: number; name: string } | null
}

export interface SpotStrategyFormRef {
  submit: () => void
}

interface SpotStrategyFormProps {
  onSaved: () => void
  editId?: string
  initial?: SpotStrategyInitial
}

const boxCls = 'bg-quant-bg border border-quant-border rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0'

/** 现货策略类型预设（STRAT_TYPES.spot 子集）：选中即套用一份保守的参数档案。 */
type SpotStrategyTypeKey = 'cra_spot' | 'martin_trend' | 'wallstreet' | 'aggressive'

const SPOT_TYPE_PRESETS: {
  key: SpotStrategyTypeKey
  label: string
  desc: string
  profile: { gridCount: number; perGridAmount: number; loopMode: 'single' | 'cycle'; feeRate: number }
}[] = [
  // 现货网格 = 表单默认档案
  { key: 'cra_spot', label: '现货网格', desc: '等距网格，一格利润止盈', profile: { gridCount: 5, perGridAmount: 50, loopMode: 'cycle', feeRate: 0.001 } },
  // 马丁趋势：首单小、层数多，倍投摊薄成本，必须循环
  { key: 'martin_trend', label: '马丁趋势', desc: '首单小层数多，浮亏摊薄', profile: { gridCount: 7, perGridAmount: 20, loopMode: 'cycle', feeRate: 0.001 } },
  // 华尔街：等比档位更密
  { key: 'wallstreet', label: '华尔街', desc: '档位更密，等比推进', profile: { gridCount: 8, perGridAmount: 30, loopMode: 'cycle', feeRate: 0.0008 } },
  // 激进：格数更大、每格更小
  { key: 'aggressive', label: '激进', desc: '高密度小网格，高频止盈', profile: { gridCount: 10, perGridAmount: 10, loopMode: 'cycle', feeRate: 0.001 } },
]

function normalizeSpotType(t?: string): SpotStrategyTypeKey {
  return SPOT_TYPE_PRESETS.some((p) => p.key === t) ? (t as SpotStrategyTypeKey) : 'cra_spot'
}
const boxHighlight = 'bg-quant-bg border border-quant-gold/50 rounded-lg px-3 py-2 text-xs flex items-center gap-2 min-w-0'

export const SpotStrategyForm = forwardRef<SpotStrategyFormRef, SpotStrategyFormProps>(function SpotStrategyForm(
  { onSaved, editId, initial },
  ref
) {
  const [isSubmitting, setIsSubmitting] = useState(false)

  const { data: configuredExchanges } = useQuery({
    queryKey: ['configured-exchanges'],
    queryFn: () => configApi.exchangesConfigured(),
    staleTime: 30000,
  })

  // 基础信息
  const [name, setName] = useState(initial?.name ?? '')
  const [symbol, setSymbol] = useState(initial?.symbol ?? 'BTCUSDT')
  const [initialCapital, setInitialCapital] = useState(initial?.initialCapital ?? 1000)
  const [selectedExchanges, setSelectedExchanges] = useState<string[]>(initial?.selectedExchanges ?? [])
  const [showExchangeModal, setShowExchangeModal] = useState(false)

  // 策略类型预设（提交 payload.strategy_type；cra 兼容别名均可由映射 CRA 键跑引擎）
  const [strategyType, setStrategyType] = useState<SpotStrategyTypeKey>(normalizeSpotType(initial?.strategyType))

  // 现货参数
  const [priceLower, setPriceLower] = useState<string>(initial?.priceLower != null ? String(initial.priceLower) : '')
  const [priceUpper, setPriceUpper] = useState<string>(initial?.priceUpper != null ? String(initial.priceUpper) : '')
  const [gridCount, setGridCount] = useState<number>(initial?.gridCount ?? 5)
  const [perGridAmount, setPerGridAmount] = useState<number>(initial?.perGridAmount ?? 50)
  const [loopMode, setLoopMode] = useState<'single' | 'cycle'>(initial?.loopMode ?? 'cycle')
  const [feeRate, setFeeRate] = useState<number>(initial?.feeRate ?? 0.001)

  // 开仓指标（现货版：隐藏顺势多/空，无 direction 概念）
  const [indicator, setIndicator] = useState<IndicatorSelection['indicator']>(initial?.indicator ?? 'none')
  const [indicatorParams, setIndicatorParams] = useState<Record<string, number | string>>(
    initial?.indicatorParams ?? {}
  )
  const [indicatorCustom, setIndicatorCustom] = useState<{ code_id: number; name: string } | null>(
    initial?.indicatorCustom ?? null
  )

  // 执行设置
  const [executionMode, setExecutionMode] = useState<'paper' | 'live'>(initial?.executionMode ?? 'paper')
  const [notifyChannels, setNotifyChannels] = useState<string[]>(initial?.notifyChannels ?? ['browser'])

  const totalInvestment = perGridAmount * gridCount

  const handleIndicatorChange = (sel: IndicatorSelection) => {
    setIndicator(sel.indicator)
    setIndicatorParams(sel.params)
    setIndicatorCustom(sel.custom)
  }

  const handleSubmit = async () => {
    if (!name.trim()) return toast('error', '请输入策略名称')
    if (!symbol.trim()) return toast('error', '请输入交易对')
    if (selectedExchanges.length === 0) return toast('error', '请至少选择一个交易所')
    const lower = parseFloat(priceLower)
    const upper = parseFloat(priceUpper)
    if (!isFinite(lower) || lower <= 0) return toast('error', '请输入有效的价格区间下限')
    if (!isFinite(upper) || upper <= lower) return toast('error', '价格区间上限必须大于下限')
    if (!Number.isInteger(gridCount) || gridCount < 2 || gridCount > 200) return toast('error', '格数必须在 2-200 之间')
    if (!perGridAmount || perGridAmount < 1) return toast('error', '每格金额必须≥1 USDT')
    if (feeRate < 0) return toast('error', '手续费率不能为负数')

    setIsSubmitting(true)
    try {
      // 单格跌幅（小数）：价格从上限跌到下限均分为 gridCount 格。
      const stepPct = (upper - lower) / upper / gridCount
      const ladder = Array.from({ length: gridCount - 1 }, (_, i) => ({
        order: i + 1,
        multiplier: 1,
        spread: stepPct * (i + 1),
        callback: 0.003,
        ema_enabled: false,
      }))
      const indicatorCfg = buildOpenIndicatorConfig(indicator, indicatorParams, indicatorCustom)
      const config: Record<string, unknown> = {
        // ── 现货自定义键（UI 显示/回填优先）──
        price_lower: lower,
        price_upper: upper,
        grid_count: gridCount,
        per_grid_amount: perGridAmount,
        loop_mode: loopMode,
        fee_rate: feeRate,
        // ── CRA 引擎映射键（cra_spot 直接可跑）──
        first_order_amount: perGridAmount, // 每格金额 = 首单金额
        first_order_multiplier: 1,
        trade_count_mode: loopMode,
        loop_count: 100,
        enable_add_position: gridCount > 1,
        order_count: gridCount,
        add_positions: ladder,
        take_profit_method: 'full',
        tp_mode: 'static',
        take_profit_ratio: stepPct, // 一格利润即止盈
        profit_callback: 0.003,
        direction: 'long',
        market_type: 'spot',
        margin_mode: 'cross',
        ...indicatorCfg,
        selected_exchanges: selectedExchanges,
      }
      const payload: Record<string, unknown> = {
        name: name.trim(),
        symbol: symbol.trim().toUpperCase(),
        leverage: 1,
        trade_direction: 'long',
        market_type: 'spot',
        execution_mode: executionMode,
        notification_config: { channels: notifyChannels },
        strategy_type: strategyType,
        status: 'stopped',
        config_json: JSON.stringify(config),
        category: 'spot',
        coin: symbol.trim().toUpperCase().replace('USDT', '').replace('USD', ''),
        direction: 'long',
        mode: 'signal',
        initial_capital: initialCapital,
      }
      let res: { forced_paper?: boolean } | undefined
      if (editId) {
        res = await strategyApi.update(editId, payload)
        toast('success', `策略 "${name.trim()}" 已保存`)
      } else {
        res = await strategyApi.create(payload)
        toast('success', `策略 "${name.trim()}" 已创建`)
      }
      if (res?.forced_paper) {
        toast('warning', '已按安全默认设为模拟盘（实盘未在服务端开启）')
      }
      onSaved()
    } catch (e: unknown) {
      toast('error', '保存失败: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      setIsSubmitting(false)
    }
  }

  useImperativeHandle(ref, () => ({ submit: () => void handleSubmit() }), [
    name,
    symbol,
    selectedExchanges,
    priceLower,
    priceUpper,
    gridCount,
    perGridAmount,
    loopMode,
    feeRate,
    indicator,
    indicatorParams,
    indicatorCustom,
    executionMode,
    notifyChannels,
    initialCapital,
    editId,
  ])

  return (
    <div className="space-y-4">
      {/* 1 基础信息（盒式 grid，与合约表单同一观感） */}
      <SectionCard title="基础信息">
        <div id="create-sec-basic" className="scroll-mt-20 -m-1 p-1">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div className={boxCls}>
              <span className="text-muted-foreground shrink-0">策略名称</span>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none text-foreground placeholder:text-muted-foreground/60"
                placeholder="输入策略名称"
              />
            </div>
            <div className={boxHighlight}>
              <span className="text-muted-foreground shrink-0">交易对</span>
              <input
                value={symbol}
                onChange={(e) => setSymbol(e.target.value.toUpperCase())}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
                placeholder="BTCUSDT"
              />
            </div>
            <button
              type="button"
              onClick={() => setShowExchangeModal(true)}
              className={cn(
                boxCls,
                'text-left transition-colors',
                selectedExchanges.length > 0 ? 'border-quant-gold/30' : ''
              )}
            >
              <Globe className="w-3.5 h-3.5 shrink-0 text-muted-foreground" />
              <span className="text-muted-foreground shrink-0">交易所</span>
              <span className="flex-1 min-w-0 truncate text-foreground">
                {selectedExchanges.length > 0 ? selectedExchanges.join(', ') : '点击选择'}
              </span>
              {selectedExchanges.length > 0 && <span className="text-quant-gold shrink-0">✓</span>}
            </button>
            <div className={boxCls}>
              <span className="text-muted-foreground shrink-0">初始资金</span>
              <input
                type="number"
                min={0}
                value={initialCapital}
                onChange={(e) => setInitialCapital(Number(e.target.value) || 0)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
                placeholder="1000"
              />
              <span className="text-muted-foreground shrink-0">USDT</span>
            </div>
          </div>
        </div>
      </SectionCard>

      <ExchangeSelectModal
        open={showExchangeModal}
        onClose={() => setShowExchangeModal(false)}
        value={selectedExchanges}
        onChange={setSelectedExchanges}
        configuredExchanges={configuredExchanges}
      />

      {/* 2 现货策略：类型预设（选中套用参数档案）+ 现货参数（区间逐格补仓） */}
      <SectionCard title="现货策略">
        <div className="space-y-2">
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
            {SPOT_TYPE_PRESETS.map((t) => {
              const active = strategyType === t.key
              return (
                <button
                  key={t.key}
                  type="button"
                  onClick={() => {
                    setStrategyType(t.key)
                    setGridCount(t.profile.gridCount)
                    setPerGridAmount(t.profile.perGridAmount)
                    setLoopMode(t.profile.loopMode)
                    setFeeRate(t.profile.feeRate)
                  }}
                  className={cn(
                    'p-3 rounded-xl border text-left transition-all',
                    active
                      ? 'border-quant-gold bg-quant-gold/10'
                      : 'border-quant-border hover:border-quant-gold/30'
                  )}
                >
                  <div className={cn('text-xs font-bold flex items-center gap-1', active ? 'text-quant-gold' : 'text-foreground')}>
                    {t.label}
                    {active && <CheckCircle2 className="w-3 h-3" />}
                  </div>
                  <div className="text-[10px] text-muted-foreground leading-relaxed mt-0.5">{t.desc}</div>
                  <div className="text-[9px] text-muted-foreground/70 mt-1 font-mono">
                    {t.profile.gridCount}格 · {t.profile.perGridAmount}U/格 · {t.profile.loopMode === 'cycle' ? '循环' : '单次'}
                  </div>
                </button>
              )
            })}
          </div>
        </div>
        <div id="create-sec-params" className="scroll-mt-20 -m-1 p-1 space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <div className={boxHighlight}>
              <span className="text-muted-foreground shrink-0">价格下限</span>
              <input
                type="number"
                value={priceLower}
                onChange={(e) => setPriceLower(e.target.value)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
                placeholder="40000"
              />
            </div>
            <div className={boxHighlight}>
              <span className="text-muted-foreground shrink-0">价格上限</span>
              <input
                type="number"
                value={priceUpper}
                onChange={(e) => setPriceUpper(e.target.value)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
                placeholder="50000"
              />
            </div>
            <div className={boxCls}>
              <span className="text-muted-foreground shrink-0">格数</span>
              <input
                type="number"
                min={2}
                max={200}
                value={gridCount}
                onChange={(e) => setGridCount(parseInt(e.target.value, 10) || 2)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
              />
              <span className="text-muted-foreground shrink-0">2-200</span>
            </div>
            <div className={boxCls}>
              <span className="text-muted-foreground shrink-0">每格金额</span>
              <input
                type="number"
                min={1}
                value={perGridAmount}
                onChange={(e) => setPerGridAmount(Number(e.target.value) || 1)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
              />
              <span className="text-muted-foreground shrink-0">USDT</span>
            </div>
            <div className={boxCls}>
              <span className="text-muted-foreground shrink-0">手续费率</span>
              <input
                type="number"
                min={0}
                step={0.0001}
                value={feeRate}
                onChange={(e) => setFeeRate(Number(e.target.value) || 0)}
                className="flex-1 min-w-0 bg-transparent focus:outline-none font-bold text-foreground"
              />
            </div>
            <div className={boxCls}>
              <span className="text-muted-foreground shrink-0">循环模式</span>
              <div className="flex-1 flex gap-1">
                {(
                  [
                    { key: 'single', label: '单次' },
                    { key: 'cycle', label: '循环' },
                  ] as const
                ).map((m) => (
                  <button
                    key={m.key}
                    type="button"
                    onClick={() => setLoopMode(m.key)}
                    className={cn(
                      'flex-1 py-1 rounded text-[11px] border transition-colors',
                      loopMode === m.key
                        ? 'bg-quant-gold/10 border-quant-gold/30 text-quant-gold font-semibold'
                        : 'border-quant-border text-muted-foreground hover:text-foreground'
                    )}
                  >
                    {m.label}
                  </button>
                ))}
              </div>
            </div>
          </div>
          <div className="text-[10px] text-muted-foreground leading-relaxed">
            价格从上限跌到下限均分 {gridCount} 格逐格补仓（等差 ladder），每格利润{' '}
            <span className="text-foreground font-mono">
              {priceLower && priceUpper && parseFloat(priceUpper) > parseFloat(priceLower) && gridCount > 0
                ? `${(((parseFloat(priceUpper) - parseFloat(priceLower)) / parseFloat(priceUpper) / gridCount) * 100).toFixed(2)}%`
                : '--'}
            </span>
            即止盈；预计总投入 <span className="text-foreground font-mono">${formatCurrency(totalInvestment)}</span>
          </div>

          {/* 开仓指标门控（现货版：隐藏顺势多/空，无 direction 行） */}
          <div className="space-y-2 pt-1">
            <div className="text-[11px] text-muted-foreground">开仓指标（可选，不选则无门槛）</div>
            <IndicatorPicker
              indicator={indicator}
              params={indicatorParams}
              custom={indicatorCustom}
              onChange={handleIndicatorChange}
            />
          </div>
        </div>
      </SectionCard>

      {/* 3 执行设置（现货同样走 live_enabled 总闸） */}
      <SectionCard title="执行设置">
        <div id="create-sec-exec" className="space-y-4 scroll-mt-20 -m-1 p-1">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <button
              type="button"
              onClick={() => setExecutionMode('paper')}
              className={cn(
                'flex items-start gap-3 p-4 rounded-xl border transition-all text-left',
                executionMode === 'paper'
                  ? 'border-quant-gold bg-quant-gold/5'
                  : 'border-quant-border hover:border-quant-gold/30'
              )}
            >
              <div
                className={cn(
                  'w-10 h-10 rounded-lg flex items-center justify-center shrink-0',
                  executionMode === 'paper' ? 'bg-quant-gold/10 text-quant-gold' : 'bg-quant-bg text-muted-foreground'
                )}
              >
                <Activity className="w-5 h-5" />
              </div>
              <div>
                <div className="text-xs font-semibold">模拟盘（默认）</div>
                <div className="text-[10px] text-muted-foreground mt-1">
                  撮合引擎模拟成交（paper），不产生真实交易所下单
                </div>
              </div>
              {executionMode === 'paper' && <CheckCircle2 className="w-4 h-4 text-quant-gold ml-auto shrink-0" />}
            </button>
            <button
              type="button"
              onClick={() => {
                if (executionMode !== 'live' && !confirm('实盘将使用真实资金下单，确认开启？')) return
                setExecutionMode('live')
              }}
              className={cn(
                'flex items-start gap-3 p-4 rounded-xl border transition-all text-left',
                executionMode === 'live'
                  ? 'border-quant-red bg-quant-red/10'
                  : 'border-quant-red/30 hover:border-quant-red/60 hover:bg-quant-red/5'
              )}
            >
              <div
                className={cn(
                  'w-10 h-10 rounded-lg flex items-center justify-center shrink-0',
                  executionMode === 'live' ? 'bg-quant-red/15 text-quant-red' : 'bg-quant-bg text-quant-red/70'
                )}
              >
                <AlertTriangle className="w-5 h-5" />
              </div>
              <div>
                <div className="text-xs font-semibold text-quant-red">实盘（真实资金）</div>
                <div className="text-[10px] text-quant-red/80 mt-1">
                  信号直连真实交易所下单；需服务端开启 trading.live_enabled，可能造成本金损失
                </div>
              </div>
              {executionMode === 'live' && <CheckCircle2 className="w-4 h-4 text-quant-red ml-auto shrink-0" />}
            </button>
          </div>
          {executionMode === 'live' && (
            <div className="px-3 py-2 rounded-lg bg-quant-red/10 border border-quant-red/30 text-[11px] text-quant-red leading-relaxed">
              实盘模式将使用真实资金下单。保存时若服务端未开启实盘开关（trading.live_enabled），请求将被拒绝。
            </div>
          )}

          <div>
            <div className="text-xs font-semibold mb-3">通知渠道</div>
            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
              {[
                { key: 'browser', label: '浏览器' },
                { key: 'email', label: '邮件' },
                { key: 'telegram', label: 'Telegram' },
                { key: 'discord', label: 'Discord' },
                { key: 'webhook', label: 'Webhook' },
                { key: 'phone', label: '短信' },
              ].map((ch) => (
                <label key={ch.key} className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                  <input
                    type="checkbox"
                    checked={notifyChannels.includes(ch.key)}
                    onChange={(e) =>
                      setNotifyChannels(
                        e.target.checked ? [...notifyChannels, ch.key] : notifyChannels.filter((c) => c !== ch.key)
                      )
                    }
                    className="rounded border-quant-border"
                  />
                  {ch.label}
                </label>
              ))}
            </div>
          </div>
        </div>
      </SectionCard>
    </div>
  )
})

export default SpotStrategyForm
