import { Calendar, ArrowUp, ArrowDown, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { SkeletonCalItem } from './Skeletons'
import { formatCalendarDate, getImpactClass } from '../utils'
import { COUNTRY_FLAGS } from '../constants'
import type { CalendarEvent } from '../types'

export function EconomicCalendar({
  events,
  loadingCalendar,
}: {
  events: CalendarEvent[]
  loadingCalendar: boolean
}) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3.5 shadow-sm flex-1 flex flex-col min-h-0 overflow-hidden">
      <div className="mb-2 flex items-center gap-1.5 text-xs font-bold text-foreground">
        <Calendar className="w-3.5 h-3.5 text-quant-gold" />
        财经日历
      </div>
      <div className="flex-1 overflow-y-auto min-h-0 scrollbar-thin">
        {loadingCalendar ? (
          Array.from({ length: 5 }).map((_, i) => <SkeletonCalItem key={i} />)
        ) : events.length > 0 ? (
          events.slice(0, 10).map((evt) => (
            <div
              key={evt.id}
              className={cn(
                'flex items-center gap-1.5 py-1.5 border-b border-quant-border/50 text-[10px] last:border-b-0',
                evt.importance === 'high' && 'border-l-[3px] border-l-quant-red pl-2 -ml-1',
                evt.importance === 'medium' && 'border-l-[3px] border-l-yellow-500 pl-2 -ml-1',
                evt.importance === 'low' && 'border-l-[3px] border-l-quant-green pl-2 -ml-1'
              )}
            >
              <span className="text-[9px] text-muted-foreground min-w-[32px] font-medium">{formatCalendarDate(evt.date)}</span>
              <span className="text-muted-foreground min-w-[36px] font-medium">{evt.time || '--:--'}</span>
              <span className="text-xs">{COUNTRY_FLAGS[evt.country] || '🌍'}</span>
              <span className="flex-1 text-foreground truncate">{evt.name}</span>
              <span
                className={cn(
                  'font-semibold text-[10px] flex items-center gap-0.5',
                  getImpactClass(evt) === 'bullish' && 'text-quant-green',
                  getImpactClass(evt) === 'bearish' && 'text-quant-red',
                  getImpactClass(evt) === 'neutral' && 'text-muted-foreground'
                )}
              >
                {getImpactClass(evt) === 'bullish' ? (
                  <ArrowUp className="w-3 h-3" />
                ) : getImpactClass(evt) === 'bearish' ? (
                  <ArrowDown className="w-3 h-3" />
                ) : (
                  <Minus className="w-3 h-3" />
                )}
                {evt.actual ?? evt.forecast ?? '--'}
              </span>
            </div>
          ))
        ) : (
          <div className="text-center py-5 text-xs text-muted-foreground">暂无事件</div>
        )}
      </div>
    </div>
  )
}
