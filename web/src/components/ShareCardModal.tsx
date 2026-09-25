import { useState } from 'react'
import { X, Copy, Check, Share2, TrendingUp, TrendingDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/useToast'
import type { ShareBacktestCard, ShareTradeCard } from '@/lib/api'

/* ── 收益分享卡弹窗 ──────────────────────────────────────────────
 * 数据源：GET /api/share/backtest/:id/card · GET /api/share/trade/:id/card。
 * 后端只出结构化数据不出图，这里做卡片预览 + 一键复制分享文案。
 * 昵称已按后端口径脱敏（首字 + ***）。
 * ─────────────────────────────────────────────────────────────── */

const pct = (v: number | undefined) => `${(v ?? 0) >= 0 ? '+' : ''}${(v ?? 0).toFixed(2)}%`

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'up' | 'down' }) {
  return (
    <div className="rounded-lg bg-quant-bg p-2.5 text-center">
      <div
        className={cn(
          'text-sm font-bold font-mono',
          tone === 'up' ? 'text-quant-green' : tone === 'down' ? 'text-quant-red' : 'text-foreground'
        )}
      >
        {value}
      </div>
      <div className="mt-0.5 text-[10px] text-muted-foreground">{label}</div>
    </div>
  )
}

function buildShareText(card: ShareBacktestCard | ShareTradeCard): string {
  if (card.kind === 'backtest') {
    return [
      `【小天量化 · 回测收益卡】${card.name}`,
      `总收益 ${pct(card.total_return_pct)} · 最大回撤 ${card.max_drawdown_pct.toFixed(2)}% · 夏普 ${card.sharpe_ratio.toFixed(2)}`,
      `胜率 ${card.win_rate.toFixed(1)}% · ${card.total_trades} 笔交易 · 盈利因子 ${card.profit_factor.toFixed(2)}`,
      `—— ${card.nickname}`,
    ].join('\n')
  }
  return [
    `【小天量化 · 交易收益卡】${card.symbol} ${card.side === 'SELL' ? '空' : '多'}`,
    `盈亏 ${pct(card.pnl_pct)} · 入场 ${card.entry_price} · 出场 ${card.exit_price}`,
    `—— ${card.nickname}`,
  ].join('\n')
}

export function ShareCardModal({
  card,
  onClose,
}: {
  card: ShareBacktestCard | ShareTradeCard | null
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  if (!card) return null

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(buildShareText(card))
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast('error', '复制失败，请手动选择文本复制')
    }
  }

  const isBacktest = card.kind === 'backtest'
  const headline = isBacktest ? card.total_return_pct : (card as ShareTradeCard).pnl_pct

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="收益分享卡"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
      tabIndex={-1}
    >
      <div
        className="w-[420px] max-w-[92vw] rounded-2xl border border-quant-border bg-quant-card p-5 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <span className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <Share2 className="h-4 w-4 text-quant-gold" />
            {isBacktest ? '回测收益分享卡' : '交易收益分享卡'}
          </span>
          <button onClick={onClose} aria-label="关闭" className="text-muted-foreground hover:text-foreground">
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* 卡片预览 */}
        <div className="rounded-xl border border-quant-gold/20 bg-gradient-to-b from-quant-gold/5 to-transparent p-4">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-foreground">
              {isBacktest ? card.name : `${(card as ShareTradeCard).symbol} ${(card as ShareTradeCard).side}`}
            </span>
            <span
              className={cn(
                'flex items-center gap-1 text-lg font-bold font-mono',
                headline >= 0 ? 'text-quant-green' : 'text-quant-red'
              )}
            >
              {headline >= 0 ? <TrendingUp className="h-4 w-4" /> : <TrendingDown className="h-4 w-4" />}
              {pct(headline)}
            </span>
          </div>
          <div className="mt-3 grid grid-cols-3 gap-1.5">
            {isBacktest ? (
              <>
                <Metric label="最大回撤" value={`${card.max_drawdown_pct.toFixed(2)}%`} tone="down" />
                <Metric label="夏普" value={card.sharpe_ratio.toFixed(2)} />
                <Metric label="胜率" value={`${card.win_rate.toFixed(1)}%`} />
                <Metric label="交易数" value={String(card.total_trades)} />
                <Metric label="盈利因子" value={card.profit_factor.toFixed(2)} />
                <Metric label="Sortino" value={card.sortino_ratio.toFixed(2)} />
              </>
            ) : (
              <>
                <Metric label="入场价" value={String((card as ShareTradeCard).entry_price)} />
                <Metric label="出场价" value={String((card as ShareTradeCard).exit_price)} />
                <Metric
                  label="盈亏"
                  value={pct((card as ShareTradeCard).pnl_pct)}
                  tone={(card as ShareTradeCard).pnl_pct >= 0 ? 'up' : 'down'}
                />
              </>
            )}
          </div>
          <div className="mt-3 flex items-center justify-between border-t border-quant-border/50 pt-2 text-[10px] text-muted-foreground">
            <span>{card.nickname}</span>
            <span>小天量化</span>
          </div>
        </div>

        <button
          onClick={handleCopy}
          className="mt-4 flex w-full items-center justify-center gap-1.5 rounded-lg bg-quant-gold py-2 text-xs font-semibold text-white transition-opacity hover:opacity-90"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          {copied ? '已复制' : '复制分享文案'}
        </button>
      </div>
    </div>
  )
}
