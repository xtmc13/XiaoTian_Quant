import type { StrategyItem } from '@/types'
import { cn, formatCurrency, formatPercent } from '@/lib/utils'
import {
  Play, Pause, Edit3, Trash2, DollarSign, Zap, ArrowRight,
  Clock, BarChart3, Wallet, TrendingUp, TrendingDown, Activity
} from 'lucide-react'
import { StatusBadge } from './StrategyList'

interface StrategyDetailPanelProps {
  strategy: StrategyItem
  onStart: () => void
  onStop: () => void
  onEdit: () => void
  onDelete: () => void
}

/* ─── Extended type for CRA fields not yet in StrategyItem ─── */
type StrategyRow = StrategyItem & {
  add_position_multiple?: number
  moving_take_profit?: { enabled: boolean; tier1_ratio: number; tier1_drawback: number; tier2_drawback: number }
  reverse_take_profit?: boolean
  reverse_stop_loss?: boolean
  amplitude?: { '5m': number; '15m': number; '30m': number; '1h': number }
  custom_reduce?: boolean
  online_order_limit?: number
  profit_protection?: boolean
  follow_trend_max?: number
  stop_loss_ratio?: number
  stop_loss_amount?: number
  stop_loss_price?: number
  first_order_price?: number
}

export function StrategyDetailPanel({ strategy, onStart, onStop, onEdit, onDelete }: StrategyDetailPanelProps) {
  const s = strategy as StrategyRow
  const pnl = s.total_pnl ?? 0
  const pnlPct = s.total_pnl_percent ?? 0
  return (
    <div className="space-y-4 max-w-4xl mx-auto">
      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-3 flex-wrap">
              <h2 className="text-lg font-bold text-foreground">{s.name}</h2>
              <StatusBadge status={s.status} />
              {s.ai_generated && <span className="text-[10px] px-1.5 py-0.5 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">AI 生成</span>}
              {s.mode === 'script' && <span className="text-[10px] px-1.5 py-0.5 rounded bg-green-500/10 text-green-400 border border-green-500/20">脚本</span>}
            </div>
            <div className="flex flex-wrap gap-2 mt-3">
              <Tag icon={DollarSign} label={s.symbol || '-'} />
              <Tag icon={Zap} label={`${s.leverage || 1}x`} />
              <Tag icon={ArrowRight} label={s.trade_direction === 'long' ? '做多' : s.trade_direction === 'short' ? '做空' : '双向'} />
              <Tag icon={Clock} label={s.timeframe || '-'} />
              {s.indicator_name && <Tag icon={BarChart3} label={s.indicator_name} />}
              {s.exchange_id && <Tag icon={Wallet} label={s.exchange_id} />}
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {s.status === 'stopped' ? (
              <button onClick={onStart} className="px-4 py-2 rounded-lg bg-quant-green text-white text-xs font-semibold hover:opacity-90 transition-opacity flex items-center gap-1.5">
                <Play className="w-3.5 h-3.5" /> 启动
              </button>
            ) : (
              <button onClick={onStop} className="px-4 py-2 rounded-lg bg-quant-red text-white text-xs font-semibold hover:opacity-90 transition-opacity flex items-center gap-1.5">
                <Pause className="w-3.5 h-3.5" /> 停止
              </button>
            )}
            <button onClick={onEdit} className="px-3 py-2 rounded-lg bg-quant-bg-tertiary border border-quant-border text-xs hover:bg-quant-hover transition-colors">
              <Edit3 className="w-3.5 h-3.5" />
            </button>
            <button onClick={onDelete} className="px-3 py-2 rounded-lg bg-quant-bg-tertiary border border-quant-border text-xs hover:bg-quant-red/10 hover:text-quant-red hover:border-quant-red/20 transition-colors">
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <div className="grid grid-cols-3 gap-3 mt-5">
          <StatBox icon={Wallet} label="投入资金" value={s.initial_capital != null ? `$${formatCurrency(s.initial_capital)}` : '-'} />
          <StatBox icon={Activity} label="当前净值" value={s.current_equity != null ? `$${formatCurrency(s.current_equity)}` : '-'} />
          <StatBox icon={pnl >= 0 ? TrendingUp : TrendingDown} label="累计盈亏" value={pnl !== 0 ? `${pnl >= 0 ? '+' : ''}$${formatCurrency(pnl)} (${formatPercent(pnlPct)})` : '-'} valueColor={pnl >= 0 ? 'text-quant-green' : pnl < 0 ? 'text-quant-red' : undefined} />
        </div>
      </div>

      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="text-xs font-semibold mb-3">策略详情</div>
        <div className="grid grid-cols-2 gap-4 text-xs">
          <DetailRow label="策略ID" value={s.id} />
          <DetailRow label="状态" value={s.status === 'running' ? '运行中' : s.status === 'error' ? '异常' : '已停止'} />
          <DetailRow label="交易对" value={s.symbol || '-'} />
          <DetailRow label="K线周期" value={s.timeframe || '-'} />
          <DetailRow label="杠杆" value={`${s.leverage || 1}x`} />
          <DetailRow label="方向" value={s.trade_direction === 'long' ? '做多' : s.trade_direction === 'short' ? '做空' : '双向'} />
          <DetailRow label="市场类型" value={s.market_type === 'spot' ? '现货' : '合约'} />
          <DetailRow label="创建时间" value={s.created_at ? new Date(s.created_at).toLocaleString() : '-'} />
        </div>
      </div>

      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="text-xs font-semibold mb-3">CRA 量化参数</div>
        <div className="grid grid-cols-2 gap-4 text-xs">
          <DetailRow label="做单数量" value={`${s.order_count || '-'} 单`} />
          <DetailRow label="首单仓位" value={s.first_order_amount ? `${s.first_order_amount} USDT` : '-'} />
          <DetailRow label="补仓价差" value={s.add_position_spread ? `${s.add_position_spread}%` : '-'} />
          <DetailRow label="补仓回调" value={s.add_position_callback ? `${s.add_position_callback}%` : '-'} />
          <DetailRow label="止盈比例" value={s.take_profit_ratio ? `${s.take_profit_ratio}%` : '-'} />
          <DetailRow label="盈利回调" value={s.profit_callback ? `${s.profit_callback}%` : '-'} />
          <DetailRow label="止盈方式" value={s.take_profit_method === 'full' ? '全仓止盈' : s.take_profit_method === 'tail' ? '尾单止盈' : s.take_profit_method === 'head_tail' ? '首尾止盈' : s.take_profit_method === 'moving' ? '移动止盈' : '-'} />
          <DetailRow label="开仓指标" value={s.open_indicator === 'macd_golden' ? 'MACD金叉开多' : s.open_indicator === 'macd_death' ? 'MACD死叉开空' : s.open_indicator === 'ema' ? 'EMA拐点' : s.open_indicator === 'close' ? '无脑买入' : '-'} />
          <DetailRow label="补仓指标" value={s.add_position_indicator === 'macd' ? 'MACD' : s.add_position_indicator === 'ema' ? 'EMA4' : '仅跌幅'} />
          <DetailRow label="防瀑布" value={s.waterfall_protection ? `${s.waterfall_protection}%` : '-'} />
          <DetailRow label="开仓加倍" value={s.open_double ? '已开启' : '未开启'} />
          <DetailRow label="趋势指标" value={s.trend_indicator ? `EMA4 (${s.trend_timeframe || '-'})` : '未开启'} />
          <DetailRow label="交易次数" value={s.trade_count_mode === 'single' ? '单次循环' : s.trade_count_mode === 'cycle' ? '策略循环' : '-'} />
          <DetailRow label="顺势而为" value={s.follow_trend ? `已开启 (最高${s.follow_trend_max || 5}倍)` : '未开启'} />
          <DetailRow label="斩仓燃烧" value={s.burn_cut && typeof s.burn_cut === 'object' && s.burn_cut.enabled ? `双向${s.burn_cut.dual_burn_start}仓/全局${s.burn_cut.global_burn_start}仓` : '未开启'} />
          <DetailRow label="在线单量限制" value={s.online_order_limit ? `${s.online_order_limit} 单` : '-'} />
          <DetailRow label="盈利保护" value={s.profit_protection ? '已开启' : '未开启'} />
          <DetailRow label="自定义减仓" value={s.custom_reduce ? '已开启' : '未开启'} />
          <DetailRow label="反向止盈" value={s.reverse_take_profit ? '已开启' : '未开启'} />
          <DetailRow label="反向止损" value={s.reverse_stop_loss ? '已开启' : '未开启'} />
          <DetailRow label="关闭补仓" value={s.close_add_position ? '是（仅止盈）' : '否'} />
          <DetailRow label="首单挂单" value={s.first_order_price ? `${s.first_order_price} USDT` : '市价'} />
        </div>
      </div>
    </div>
  )
}

function Tag({ icon: Icon, label }: { icon: React.ComponentType<{ className?: string }>; label: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-[11px] px-2 py-1 rounded-md bg-quant-bg-tertiary border border-quant-border text-muted-foreground">
      <Icon className="w-3 h-3 text-quant-gold" /> {label}
    </span>
  )
}

function StatBox({ icon: Icon, label, value, valueColor }: { icon: React.ComponentType<{ className?: string }>; label: string; value: string; valueColor?: string }) {
  return (
    <div className="flex items-center gap-3 p-3 rounded-lg bg-quant-bg border border-quant-border">
      <div className="w-9 h-9 rounded-lg bg-quant-bg-tertiary flex items-center justify-center text-quant-gold">
        <Icon className="w-4 h-4" />
      </div>
      <div>
        <div className={cn('text-sm font-bold', valueColor || 'text-foreground')}>{value}</div>
        <div className="text-[10px] text-muted-foreground">{label}</div>
      </div>
    </div>
  )
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between py-1.5 border-b border-quant-border/50">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium text-foreground">{value}</span>
    </div>
  )
}
