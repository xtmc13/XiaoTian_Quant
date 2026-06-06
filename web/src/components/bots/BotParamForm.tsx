import React from 'react'
import { cn } from '@/lib/utils'

export function WizardField({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2">
        <label className="text-xs font-medium text-[#aaaaaa]">{label}</label>
        {hint && <span className="text-[11px] text-[#8a8a8a]">{hint}</span>}
      </div>
      {children}
    </div>
  )
}

interface BotParamFormProps {
  form: Record<string, unknown>
  setForm: React.Dispatch<React.SetStateAction<Record<string, unknown>>>
  effectiveType: string
}

export function BotParamForm({ form, setForm, effectiveType }: BotParamFormProps) {
  const isCraType = ['trend', 'martin_trend', 'wallstreet', 'macd_golden', 'macd_death', 'dual_burn', 'ema_follow', 'ema_counter'].includes(effectiveType)

  return (
    <div className="space-y-4">
      {/* -- 基础策略参数 -- */}
      <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
        <div className="text-xs font-semibold text-white">基础策略参数</div>
        <div className="grid grid-cols-2 gap-3">
          <WizardField label="K线周期">
            <select
              value={(form.timeframe as string) || '1h'}
              onChange={(e) => setForm((f) => ({ ...f, timeframe: e.target.value }))}
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
            >
              <option value="1m">1分钟</option>
              <option value="5m">5分钟</option>
              <option value="15m">15分钟</option>
              <option value="30m">30分钟</option>
              <option value="1h">1小时</option>
              <option value="4h">4小时</option>
              <option value="1d">1天</option>
            </select>
          </WizardField>
          <WizardField label="杠杆">
            <input
              type="number"
              min={1}
              max={125}
              value={(form.leverage as number) || 10}
              onChange={(e) => setForm((f) => ({ ...f, leverage: Number(e.target.value) }))}
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
            />
          </WizardField>
        </div>
        <WizardField label="方向">
          <div className="flex gap-2">
            {[
              { key: 'long', label: '做多' },
              { key: 'short', label: '做空' },
              { key: 'dual', label: '双向' },
            ].map((d) => (
              <button
                key={d.key}
                onClick={() => setForm((f) => ({ ...f, direction: d.key }))}
                className={cn(
                  'flex-1 rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                  (form.direction as string) === d.key
                    ? 'border-white/20 bg-white/10 text-white'
                    : 'border-[#1c1c1c] bg-[#141414] text-[#999999] hover:text-[#888888]'
                )}
              >
                {d.label}
              </button>
            ))}
          </div>
        </WizardField>
      </div>

      {isCraType && (
        // -- CRA 参数配置
        <div className="rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] p-4 space-y-4">
          <div className="text-xs font-semibold text-[#4f6ed1]">CRA 量化参数</div>

          <div className="grid grid-cols-2 gap-3">
            <WizardField label="做单数量 (1-20单)">
              <input
                type="number"
                min={1}
                max={20}
                value={(form.order_count as number) || 7}
                onChange={(e) => setForm((f) => ({ ...f, order_count: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="做单数量"
              />
            </WizardField>
            <WizardField label="首单仓位 (USDT)">
              <input
                type="number"
                min={10}
                max={10000}
                step={10}
                value={(form.first_order_amount as number) || 100}
                onChange={(e) => setForm((f) => ({ ...f, first_order_amount: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="首单仓位 USDT"
              />
            </WizardField>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <WizardField label="补仓价差 (%)">
              <input
                type="number"
                min={0.5}
                max={50}
                step={0.5}
                value={(form.add_position_spread as number) || 3}
                onChange={(e) => setForm((f) => ({ ...f, add_position_spread: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="补仓价差百分比"
              />
            </WizardField>
            <WizardField label="补仓回调 (%)">
              <input
                type="number"
                min={0.01}
                max={0.5}
                step={0.01}
                value={(form.add_position_callback as number) || 0.1}
                onChange={(e) => setForm((f) => ({ ...f, add_position_callback: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="补仓回调百分比"
              />
            </WizardField>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <WizardField label="止盈比例 (%)">
              <input
                type="number"
                min={0.1}
                max={50}
                step={0.1}
                value={(form.take_profit_ratio as number) || 1.3}
                onChange={(e) => setForm((f) => ({ ...f, take_profit_ratio: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="止盈比例百分比"
              />
            </WizardField>
            <WizardField label="盈利回调 (%)">
              <input
                type="number"
                min={0.01}
                max={0.5}
                step={0.01}
                value={(form.profit_callback as number) || 0.1}
                onChange={(e) => setForm((f) => ({ ...f, profit_callback: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="盈利回调百分比"
              />
            </WizardField>
          </div>

          <WizardField label="止盈方式">
            <div className="flex gap-2">
              {[
                { key: 'full', label: '全仓' },
                { key: 'tail', label: '尾单' },
                { key: 'head_tail', label: '首尾' },
                { key: 'moving', label: '移动' },
              ].map((m) => (
                <button
                  key={m.key}
                  onClick={() => setForm((f) => ({ ...f, take_profit_method: m.key }))}
                  className={cn(
                    'rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                    (form.take_profit_method as string) === m.key
                      ? 'border-white/20 bg-white/10 text-white'
                      : 'border-[#1c1c1c] bg-[#141414] text-[#999999] hover:text-[#888888]'
                  )}
                >
                  {m.label}
                </button>
              ))}
            </div>
          </WizardField>

          <WizardField label="开仓指标">
            <select
              value={(form.open_indicator as string) || 'macd_golden'}
              onChange={(e) => setForm((f) => ({ ...f, open_indicator: e.target.value }))}
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
            >
              <option value="macd_golden">MACD金叉开多</option>
              <option value="macd_death">MACD死叉开空</option>
              <option value="ema">EMA拐点开仓</option>
              <option value="close">关闭（无脑买入）</option>
            </select>
          </WizardField>

          <WizardField label="补仓指标">
            <select
              value={(form.add_position_indicator as string) || 'macd'}
              onChange={(e) => setForm((f) => ({ ...f, add_position_indicator: e.target.value }))}
              className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
            >
              <option value="macd">MACD金叉/死叉补仓</option>
              <option value="ema">EMA4上下拐点补仓</option>
              <option value="close">关闭（仅按跌幅补仓）</option>
            </select>
          </WizardField>

          <div className="grid grid-cols-2 gap-3">
            <WizardField label="防瀑布 (%)">
              <input
                type="number"
                min={0.5}
                max={20}
                step={0.5}
                value={(form.waterfall_protection as number) || 2}
                onChange={(e) => setForm((f) => ({ ...f, waterfall_protection: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="防瀑布百分比"
              />
            </WizardField>
            <WizardField label="在线单量限制">
              <input
                type="number"
                min={1}
                max={50}
                value={(form.online_order_limit as number) || 10}
                onChange={(e) => setForm((f) => ({ ...f, online_order_limit: Number(e.target.value) }))}
                className="w-full rounded-lg border border-[#1c1c1c] bg-[#0a0a0a] px-3 py-2 text-sm text-white outline-none focus:border-[#4f6ed1]/40"
                aria-label="在线单量限制"
              />
            </WizardField>
          </div>

          <div className="flex gap-3">
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.open_double}
                onChange={(e) => setForm((f) => ({ ...f, open_double: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
                aria-label="开仓加倍"
              />
              开仓加倍
            </label>
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.trend_indicator}
                onChange={(e) => setForm((f) => ({ ...f, trend_indicator: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
                aria-label="趋势指标EMA4"
              />
              趋势指标(EMA4)
            </label>
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.follow_trend}
                onChange={(e) => setForm((f) => ({ ...f, follow_trend: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
                aria-label="顺势而为"
              />
              顺势而为
            </label>
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.profit_protection}
                onChange={(e) => setForm((f) => ({ ...f, profit_protection: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
                aria-label="盈利保护"
              />
              盈利保护
            </label>
          </div>

          <div className="flex gap-3">
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.reverse_take_profit}
                onChange={(e) => setForm((f) => ({ ...f, reverse_take_profit: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
              />
              反向止盈
            </label>
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.reverse_stop_loss}
                onChange={(e) => setForm((f) => ({ ...f, reverse_stop_loss: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
              />
              反向止损
            </label>
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.custom_reduce}
                onChange={(e) => setForm((f) => ({ ...f, custom_reduce: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
              />
              自定义减仓
            </label>
            <label className="flex items-center gap-2 text-xs text-[#999999]">
              <input
                type="checkbox"
                checked={!!form.close_add_position}
                onChange={(e) => setForm((f) => ({ ...f, close_add_position: e.target.checked }))}
                className="rounded border-[#1c1c1c]"
              />
              关闭补仓
            </label>
          </div>

          <WizardField label="交易次数">
            <div className="flex gap-2">
              {[
                { key: 'single', label: '单次循环' },
                { key: 'cycle', label: '策略循环' },
              ].map((m) => (
                <button
                  key={m.key}
                  onClick={() => setForm((f) => ({ ...f, trade_count_mode: m.key }))}
                  className={cn(
                    'rounded-lg border px-3 py-2 text-xs font-medium transition-colors',
                    (form.trade_count_mode as string) === m.key
                      ? 'border-white/20 bg-white/10 text-white'
                      : 'border-[#1c1c1c] bg-[#141414] text-[#999999] hover:text-[#888888]'
                  )}
                >
                  {m.label}
                </button>
              ))}
            </div>
          </WizardField>
        </div>
      )}
    </div>
  )
}
