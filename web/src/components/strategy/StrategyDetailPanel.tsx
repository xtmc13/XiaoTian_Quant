import type { StrategyItem, AddPositionItem, MovingTPTier } from '@/types'
import { cn, formatCurrency } from '@/lib/utils'
import {
  Play,
  Pause,
  Edit3,
  Trash2,
  DollarSign,
  Zap,
  ArrowRight,
  Clock,
  BarChart3,
  Wallet,
  TrendingUp,
  TrendingDown,
  Activity,
} from 'lucide-react'
import { StatusBadge } from './StrategyList'

interface StrategyDetailPanelProps {
  strategy: StrategyItem
  onStart: () => void
  onStop: () => void
  onEdit: () => void
  onDelete: () => void
}

function safeJsonParse(json?: string | null): Record<string, unknown> {
  if (!json) return {}
  try {
    return JSON.parse(json) as Record<string, unknown>
  } catch {
    return {}
  }
}

function boolField(config: Record<string, unknown>, key: string): string {
  const v = config[key]
  if (v === true) return '已开启'
  if (v === false) return '未开启'
  return '-'
}

function formatAddPositions(addPositions?: AddPositionItem[]): string {
  if (!addPositions || addPositions.length === 0) return '-'
  return addPositions.map((p) => `${p.order}单 ${p.multiplier}x/${p.spread}%/${p.callback}%`).join(' · ')
}

function formatMovingTPTiers(tiers?: MovingTPTier[]): string {
  if (!tiers || tiers.length === 0) return '-'
  return tiers.map((t, i) => `档${i + 1}: ${t.ratio}%/${t.drawback}%`).join(' · ')
}

export function StrategyDetailPanel({ strategy, onStart, onStop, onEdit, onDelete }: StrategyDetailPanelProps) {
  const s = strategy
  const config = safeJsonParse(s.config_json)
  const pnl = s.total_pnl ?? 0
  const pnlPct = s.total_pnl_percent ?? 0
  const isContract = s.market_type !== 'spot' && s.category !== 'spot'

  return (
    <div className="space-y-4 max-w-4xl mx-auto">
      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex items-center gap-3 flex-wrap">
              <h2 className="text-lg font-bold text-foreground">{s.name}</h2>
              <StatusBadge status={s.status} />
              {s.ai_generated && (
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-purple-500/10 text-purple-400 border border-purple-500/20">
                  AI 生成
                </span>
              )}
              {s.mode === 'script' && (
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-green-500/10 text-green-400 border border-green-500/20">
                  脚本
                </span>
              )}
            </div>
            <div className="flex flex-wrap gap-2 mt-3">
              <Tag icon={DollarSign} label={s.symbol || '-'} />
              <Tag icon={Zap} label={`${s.leverage || 1}x`} />
              <Tag
                icon={ArrowRight}
                label={s.trade_direction === 'long' ? '做多' : s.trade_direction === 'short' ? '做空' : '双向'}
              />
              <Tag icon={Clock} label={s.timeframe || '-'} />
              {s.indicator_name && <Tag icon={BarChart3} label={s.indicator_name} />}
              {s.exchange_id && <Tag icon={Wallet} label={s.exchange_id} />}
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {s.status === 'stopped' ? (
              <button
                onClick={onStart}
                className="px-4 py-2 rounded-lg bg-quant-green text-white text-xs font-semibold hover:opacity-90 transition-opacity flex items-center gap-1.5"
              >
                <Play className="w-3.5 h-3.5" /> 启动
              </button>
            ) : (
              <button
                onClick={onStop}
                className="px-4 py-2 rounded-lg bg-quant-red text-white text-xs font-semibold hover:opacity-90 transition-opacity flex items-center gap-1.5"
              >
                <Pause className="w-3.5 h-3.5" /> 停止
              </button>
            )}
            <button
              onClick={onEdit}
              className="px-3 py-2 rounded-lg bg-quant-bg-tertiary border border-quant-border text-xs hover:bg-quant-hover transition-colors"
            >
              <Edit3 className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={onDelete}
              className="px-3 py-2 rounded-lg bg-quant-bg-tertiary border border-quant-border text-xs hover:bg-quant-red/10 hover:text-quant-red hover:border-quant-red/20 transition-colors"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <div className="grid grid-cols-3 gap-3 mt-5">
          <StatBox
            icon={Wallet}
            label="投入资金"
            value={s.initial_capital != null ? `$${formatCurrency(s.initial_capital)}` : '-'}
          />
          <StatBox
            icon={Activity}
            label="当前净值"
            value={s.current_equity != null ? `$${formatCurrency(s.current_equity)}` : '-'}
          />
          <StatBox
            icon={pnl >= 0 ? TrendingUp : TrendingDown}
            label="累计盈亏"
            value={pnl !== 0 ? `${pnl >= 0 ? '+' : ''}$${formatCurrency(pnl)} (${pnlPct.toFixed(2)}%)` : '-'}
            valueColor={pnl >= 0 ? 'text-quant-green' : pnl < 0 ? 'text-quant-red' : undefined}
          />
        </div>
      </div>

      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="text-xs font-semibold mb-3">策略详情</div>
        <div className="grid grid-cols-2 gap-4 text-xs">
          <DetailRow label="策略ID" value={s.id} />
          <DetailRow
            label="状态"
            value={s.status === 'running' ? '运行中' : s.status === 'error' ? '异常' : '已停止'}
          />
          <DetailRow label="交易对" value={s.symbol || '-'} />
          <DetailRow label="K线周期" value={s.timeframe || '-'} />
          <DetailRow label="杠杆" value={`${s.leverage || 1}x`} />
          <DetailRow
            label="方向"
            value={s.trade_direction === 'long' ? '做多' : s.trade_direction === 'short' ? '做空' : '双向'}
          />
          <DetailRow label="市场类型" value={isContract ? '合约' : '现货'} />
          <DetailRow label="创建时间" value={s.created_at ? new Date(s.created_at).toLocaleString() : '-'} />
        </div>
      </div>

      <div className="rounded-xl border border-quant-border bg-quant-card p-5">
        <div className="text-xs font-semibold mb-3">CRA 量化参数</div>
        <div className="grid grid-cols-2 gap-4 text-xs">
          <DetailRow label="挂单价格" value={config.first_order_price ? `${config.first_order_price} USDT` : '市价'} />
          <DetailRow label="首单额度" value={config.first_order_amount ? `${config.first_order_amount} USDT` : '-'} />
          <DetailRow
            label="首单加倍"
            value={config.first_order_multiplier ? `${config.first_order_multiplier}x` : '-'}
          />
          <DetailRow
            label="循环类型"
            value={
              config.trade_count_mode === 'single' ? '单次策略' : config.trade_count_mode === 'cycle' ? '循环策略' : '-'
            }
          />
          <DetailRow label="循环次数" value={config.loop_count ? `${config.loop_count} 次` : '-'} />
          <DetailRow label="是否开启补仓" value={boolField(config, 'enable_add_position')} />
          <DetailRow label="补仓次数" value={config.order_count ? `${config.order_count} 次` : '-'} />
          <DetailRow
            label="止盈方式"
            value={
              config.take_profit_method === 'full'
                ? '全仓止盈'
                : config.take_profit_method === 'tail'
                  ? '尾单止盈'
                  : config.take_profit_method === 'head_tail'
                    ? '首尾止盈'
                    : '-'
            }
          />
          <DetailRow
            label="止盈模式"
            value={config.tp_mode === 'moving' ? '移动止盈' : config.tp_mode === 'static' ? '静态止盈' : '-'}
          />
          <DetailRow label="止盈比例" value={config.take_profit_ratio ? `${config.take_profit_ratio}%` : '-'} />
          <DetailRow label="盈利回调" value={config.profit_callback ? `${config.profit_callback}%` : '-'} />
          <DetailRow label="防瀑布" value={boolField(config, 'waterfall_enabled')} />
          <DetailRow label="防瀑布比例" value={config.waterfall_protection ? `${config.waterfall_protection}%` : '-'} />
          <DetailRow label="开仓加倍" value={boolField(config, 'open_double')} />
          <DetailRow label="顺势而为" value={boolField(config, 'follow_trend')} />
        </div>

        {((config.add_positions as AddPositionItem[] | undefined)?.length ?? 0) > 0 && (
          <div className="mt-4">
            <div className="text-[11px] text-muted-foreground mb-2">补仓计划</div>
            <div className="text-xs font-mono text-foreground">
              {formatAddPositions(config.add_positions as AddPositionItem[])}
            </div>
          </div>
        )}

        {((config.moving_take_profit_tiers as MovingTPTier[] | undefined)?.length ?? 0) > 0 && (
          <div className="mt-4">
            <div className="text-[11px] text-muted-foreground mb-2">移动止盈档位</div>
            <div className="text-xs font-mono text-foreground">
              {formatMovingTPTiers(config.moving_take_profit_tiers as MovingTPTier[])}
            </div>
          </div>
        )}

        {isContract && (
          <>
            <div className="mt-4 text-xs font-semibold text-quant-gold">合约专属参数</div>
            <div className="grid grid-cols-2 gap-4 text-xs mt-3">
              <DetailRow
                label="开仓 MACD"
                value={(config.open_macd_enabled ? `${config.open_macd_period}` : '关闭') as string}
              />
              <DetailRow
                label="逆势 EMA"
                value={(config.open_counter_ema_enabled ? `${config.open_counter_ema_period}` : '关闭') as string}
              />
              <DetailRow
                label="顺势 EMA"
                value={(config.open_trend_ema_enabled ? `${config.open_trend_ema_period}` : '关闭') as string}
              />
              <DetailRow
                label="补仓 MACD"
                value={(config.add_macd_enabled ? `${config.add_macd_period}` : '关闭') as string}
              />
              <DetailRow
                label="补仓 EMA"
                value={(config.add_ema_enabled ? `${config.add_ema_period}` : '关闭') as string}
              />
              <DetailRow
                label="反向止盈"
                value={config.reverse_take_profit_period === 'close' ? '关闭' : `${config.reverse_take_profit_period}`}
              />
              <DetailRow label="反向止损" value={boolField(config, 'reverse_stop_loss')} />
              <DetailRow label="止损开关" value={boolField(config, 'stop_loss_enabled')} />
              <DetailRow
                label="止损类型"
                value={
                  config.stop_loss_type === 'ratio'
                    ? '比例'
                    : config.stop_loss_type === 'amount'
                      ? '金额'
                      : config.stop_loss_type === 'price'
                        ? '价格'
                        : '-'
                }
              />
              <DetailRow label="止损比例" value={config.stop_loss_ratio ? `${config.stop_loss_ratio}%` : '-'} />
              <DetailRow label="止损金额" value={config.stop_loss_amount ? `${config.stop_loss_amount} USDT` : '-'} />
              <DetailRow label="止损价格" value={config.stop_loss_price ? `${config.stop_loss_price} USDT` : '-'} />
              <DetailRow
                label="全局燃烧"
                value={config.burn_global_enabled ? `第 ${config.burn_global_threshold} 次` : '未开启'}
              />
              <DetailRow
                label="对向燃烧"
                value={config.burn_dual_enabled ? `第 ${config.burn_dual_threshold} 次` : '未开启'}
              />
            </div>
          </>
        )}
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

function StatBox({
  icon: Icon,
  label,
  value,
  valueColor,
}: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: string
  valueColor?: string
}) {
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
