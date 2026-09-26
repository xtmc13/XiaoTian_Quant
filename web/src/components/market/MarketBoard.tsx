import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BadgeCheck, ChevronLeft, ChevronRight, Store, Users } from 'lucide-react'
import { cn, formatCurrency } from '@/lib/utils'
import { marketListingApi, type MarketListing } from '@/lib/api'
import { useI18n } from '@/i18n'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from '@/components/ui/EmptyState'

type SortKey = 'return' | 'drawdown' | 'followers'

/** 标准化统计小格（口径与后端 market_listing_stats 快照一致）。 */
function StatCell({ label, value, tone }: { label: string; value: string; tone?: 'up' | 'down' }) {
  return (
    <div className="text-center p-1.5 bg-quant-bg rounded-lg">
      <div className={cn('text-xs font-bold font-mono', tone === 'up' ? 'text-quant-green' : tone === 'down' ? 'text-quant-red' : 'text-foreground')}>
        {value}
      </div>
      <div className="text-[9px] text-muted-foreground mt-0.5">{label}</div>
    </div>
  )
}

export function MarketListingCard({ listing }: { listing: MarketListing }) {
  const { t } = useI18n()
  const s = listing.stats
  const pct = (v?: number) => `${(v ?? 0) >= 0 ? '+' : ''}${(v ?? 0).toFixed(2)}%`
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3.5 transition-all hover:border-quant-gold/20">
      {/* 行1：名称 + 付费模式 */}
      <div className="flex items-center justify-between gap-2">
        <span className="font-bold text-xs text-foreground truncate">{listing.name}</span>
        {listing.monthly_fee > 0 ? (
          <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px] shrink-0">
            ${formatCurrency(listing.monthly_fee)}/{t('market.unit.month')}
          </span>
        ) : listing.fee_percent > 0 ? (
          <span className="px-1.5 py-0.5 rounded bg-quant-gold/15 text-quant-gold border border-quant-gold/40 text-[10px] shrink-0">
            {t('market.board.profitShare')} {listing.fee_percent}%
          </span>
        ) : (
          <span className="px-1.5 py-0.5 rounded bg-quant-bg text-muted-foreground border border-quant-border text-[10px] shrink-0">
            {t('market.board.free')}
          </span>
        )}
      </div>

      {/* 行2：类型 + 考核通过标识 + 跟踪数 */}
      <div className="mt-1.5 flex items-center gap-1.5 text-[10px] text-muted-foreground min-w-0">
        <span className="px-1 py-0.5 rounded bg-cyan-500/15 text-cyan-400 shrink-0">
          {listing.kind === 'signal' ? t('market.board.kindSignal') : t('market.board.kindRobot')}
        </span>
        {listing.probation_passed && (
          <span className="inline-flex items-center gap-0.5 px-1 py-0.5 rounded bg-quant-green/15 text-quant-green border border-quant-green/40 shrink-0">
            <BadgeCheck className="w-3 h-3" />
            {t('market.status.probationPassed')}
          </span>
        )}
        <span className="ml-auto inline-flex items-center gap-0.5 shrink-0">
          <Users className="w-3 h-3" />
          {s?.followers ?? 0}
        </span>
      </div>

      {/* 行3：标准化透明统计（最新日快照） */}
      {s ? (
        <div className="grid grid-cols-3 gap-1.5 mt-2.5">
          <StatCell label={t('market.stats.monthlyReturn')} value={pct(s.monthly_return_pct)} tone={s.monthly_return_pct >= 0 ? 'up' : 'down'} />
          <StatCell label={t('market.stats.maxDrawdown')} value={`-${Math.abs(s.max_drawdown_pct).toFixed(2)}%`} tone="down" />
          <StatCell label={t('market.stats.winRate')} value={`${s.win_rate.toFixed(1)}%`} />
          <StatCell label={t('market.stats.totalTrades')} value={String(s.total_trades)} />
          <StatCell label={t('market.stats.runningDays')} value={`${s.running_days}${t('market.unit.day')}`} />
          <StatCell label={t('market.stats.totalReturn')} value={pct(s.total_return_pct)} tone={s.total_return_pct >= 0 ? 'up' : 'down'} />
        </div>
      ) : (
        <div className="mt-2.5 text-[10px] text-muted-foreground py-4 text-center">{t('market.stats.noStats')}</div>
      )}
    </div>
  )
}

/** 公开策略市场：listed 条目卡片 + 排序/分页。 */
export function MarketBoard() {
  const { t } = useI18n()
  const [sort, setSort] = useState<SortKey>('return')
  const [order, setOrder] = useState<'desc' | 'asc'>('desc')
  const [page, setPage] = useState(1)
  const pageSize = 12

  const { data, isLoading } = useQuery({
    queryKey: ['market', 'listings', sort, order, page],
    queryFn: () => marketListingApi.list({ sort, order, page, page_size: pageSize }),
    staleTime: 60_000,
  })
  const listings = data?.listings ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const SORTS: { key: SortKey; label: string }[] = [
    { key: 'return', label: t('market.board.sortReturn') },
    { key: 'drawdown', label: t('market.board.sortDrawdown') },
    { key: 'followers', label: t('market.board.sortFollowers') },
  ]

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-xs text-muted-foreground">
          {t('market.board.listedCount')} {total}
        </span>
        <span className="flex-1" />
        {SORTS.map((s) => (
          <button
            key={s.key}
            onClick={() => {
              setPage(1)
              if (sort === s.key) setOrder((o) => (o === 'desc' ? 'asc' : 'desc'))
              else {
                setSort(s.key)
                setOrder(s.key === 'drawdown' ? 'asc' : 'desc')
              }
            }}
            className={cn(
              'px-2.5 py-1 rounded-full text-[11px] border transition-colors',
              sort === s.key
                ? 'bg-quant-gold/10 text-quant-gold border-quant-gold/30'
                : 'border-quant-border text-muted-foreground hover:text-foreground'
            )}
          >
            {s.label}
            {sort === s.key ? (order === 'desc' ? ' ▼' : ' ▲') : ''}
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-40 rounded-xl" />
          ))}
        </div>
      ) : listings.length === 0 ? (
        <EmptyState
          icon={<Store className="w-8 h-8" />}
          title={t('market.board.emptyTitle')}
          description={t('market.board.emptyDesc')}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {listings.map((l) => (
            <MarketListingCard key={l.id} listing={l} />
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-3 text-xs text-muted-foreground">
          <button
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
            className="p-1 rounded border border-quant-border disabled:opacity-40 hover:text-foreground"
            aria-label="prev page"
          >
            <ChevronLeft className="w-3.5 h-3.5" />
          </button>
          <span>
            {page} / {totalPages}
          </span>
          <button
            disabled={page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
            className="p-1 rounded border border-quant-border disabled:opacity-40 hover:text-foreground"
            aria-label="next page"
          >
            <ChevronRight className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
    </div>
  )
}

export default MarketBoard
