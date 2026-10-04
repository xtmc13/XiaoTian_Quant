import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { advancedOrderApi, type BracketCalculateResult } from '@/lib/api'
import { toast } from '@/lib/useToast'
import { useI18n } from '@/i18n'
import { Calculator, Loader2 } from 'lucide-react'

export interface BracketCalcApplied {
  takeProfitPrice: number
  stopLossPrice: number
  quantity: number
}

function fmt(v: number | undefined, d = 4): string {
  if (v == null || !isFinite(v)) return '-'
  return v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: d })
}

/**
 * Bracket 试算器（POST /orders/bracket/calculate）：按入场价 + 止损/止盈幅度 +
 * 余额/单笔风险反推 TP/SL 价格与仓位数量；UI 输入为百分比，接口为小数口径。
 * 「应用到表单」把 TP/SL/数量回填进新建 Bracket 表单。
 */
export function BracketCalculator({
  symbol,
  side: initialSide,
  entryPrice,
  onApply,
}: {
  symbol: string
  side: 'buy' | 'sell'
  entryPrice: number
  onApply: (v: BracketCalcApplied) => void
}) {
  const { t } = useI18n()
  const [side, setSide] = useState<'buy' | 'sell'>(initialSide)
  const [entry, setEntry] = useState(entryPrice || 0)
  const [slPct, setSlPct] = useState(2)
  const [tpPct, setTpPct] = useState(4)
  const [balance, setBalance] = useState(10000)
  const [riskPct, setRiskPct] = useState(1)
  const [result, setResult] = useState<BracketCalculateResult | null>(null)

  const calcMutation = useMutation({
    mutationFn: () =>
      advancedOrderApi.bracket.calculate({
        entry_price: entry,
        side,
        stop_loss_pct: slPct / 100,
        take_profit_pct: tpPct / 100,
        balance,
        risk_pct: riskPct / 100,
      }),
    onSuccess: (d) => setResult(d),
    onError: (err: unknown) => toast('error', err instanceof Error ? err.message : t('bracket.calc.failed')),
  })

  const handleCalc = () => {
    if (!(entry > 0) || !(slPct > 0) || !(tpPct > 0) || !(balance > 0) || !(riskPct > 0)) {
      toast('warning', t('bracket.calc.invalid'))
      return
    }
    calcMutation.mutate()
  }

  const inputCls =
    'w-full px-2 py-1.5 rounded-md bg-quant-bg-secondary border border-quant-border text-sm focus:outline-none focus:border-quant-gold'
  const labelCls = 'text-xs text-muted-foreground'

  return (
    <div className="rounded-lg border border-quant-border bg-quant-bg p-4 space-y-3">
      <div className="flex items-center gap-2">
        <Calculator className="w-4 h-4 text-quant-gold" />
        <span className="text-sm font-medium">{t('bracket.calc.title')}</span>
        <span className="text-[10px] text-muted-foreground font-mono">{symbol}</span>
      </div>
      <p className="text-[11px] text-muted-foreground">{t('bracket.calc.desc')}</p>

      <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
        <div className="space-y-1">
          <label className={labelCls}>{t('bracket.calc.side')}</label>
          <select value={side} onChange={(e) => setSide(e.target.value as 'buy' | 'sell')} className={inputCls} aria-label={t('bracket.calc.side')}>
            <option value="buy">{t('bracket.calc.buy')}</option>
            <option value="sell">{t('bracket.calc.sell')}</option>
          </select>
        </div>
        <div className="space-y-1">
          <label className={labelCls}>{t('bracket.calc.entryPrice')}</label>
          <input type="number" value={entry} onChange={(e) => setEntry(parseFloat(e.target.value))} className={inputCls} aria-label={t('bracket.calc.entryPrice')} />
        </div>
        <div className="space-y-1">
          <label className={labelCls}>{t('bracket.calc.stopLossPct')}</label>
          <input type="number" value={slPct} onChange={(e) => setSlPct(parseFloat(e.target.value))} className={inputCls} aria-label={t('bracket.calc.stopLossPct')} />
        </div>
        <div className="space-y-1">
          <label className={labelCls}>{t('bracket.calc.takeProfitPct')}</label>
          <input type="number" value={tpPct} onChange={(e) => setTpPct(parseFloat(e.target.value))} className={inputCls} aria-label={t('bracket.calc.takeProfitPct')} />
        </div>
        <div className="space-y-1">
          <label className={labelCls}>{t('bracket.calc.balance')}</label>
          <input type="number" value={balance} onChange={(e) => setBalance(parseFloat(e.target.value))} className={inputCls} aria-label={t('bracket.calc.balance')} />
        </div>
        <div className="space-y-1">
          <label className={labelCls}>{t('bracket.calc.riskPct')}</label>
          <input type="number" value={riskPct} onChange={(e) => setRiskPct(parseFloat(e.target.value))} className={inputCls} aria-label={t('bracket.calc.riskPct')} />
        </div>
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={handleCalc}
          disabled={calcMutation.isPending}
          className="flex items-center gap-1.5 px-4 py-2 rounded-md bg-quant-gold/10 text-quant-gold text-sm font-medium hover:bg-quant-gold/20 transition-colors disabled:opacity-50"
        >
          {calcMutation.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Calculator className="w-4 h-4" />}
          {calcMutation.isPending ? t('bracket.calc.calculating') : t('bracket.calc.submit')}
        </button>
      </div>

      {result && (
        <div className="space-y-3">
          <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
            {[
              { label: t('bracket.calc.tp'), value: fmt(result.take_profit) },
              { label: t('bracket.calc.sl'), value: fmt(result.stop_loss) },
              { label: t('bracket.calc.positionSize'), value: fmt(result.position_size, 6) },
              { label: t('bracket.calc.riskAmount'), value: `$${fmt(result.risk_amount)}` },
              { label: t('bracket.calc.rewardAmount'), value: `$${fmt(result.reward_amount)}` },
              { label: t('bracket.calc.riskReward'), value: `1 : ${fmt(result.risk_reward)}` },
            ].map((m) => (
              <div key={m.label} className="rounded-md bg-quant-bg-secondary p-2">
                <div className="text-[10px] text-muted-foreground">{m.label}</div>
                <div className="text-sm font-mono font-medium">{m.value}</div>
              </div>
            ))}
          </div>
          <button
            onClick={() => {
              onApply({
                takeProfitPrice: result.take_profit,
                stopLossPrice: result.stop_loss,
                quantity: result.position_size,
              })
              toast('success', t('bracket.calc.applied'))
            }}
            className="flex items-center gap-1.5 px-4 py-2 rounded-md bg-quant-gold text-white text-sm font-medium hover:bg-quant-gold/90 transition-colors"
          >
            {t('bracket.calc.apply')}
          </button>
        </div>
      )}
    </div>
  )
}
