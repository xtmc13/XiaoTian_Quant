import { cn } from '@/lib/utils'
import { SkeletonCell } from './Skeletons'
import { formatHeatmapPrice, formatNum, getHeatmapName, getHeatmapStyle } from '../utils'
import { HEATMAP_TABS } from '../constants'
import type { HeatmapItem, HeatmapType } from '../types'

export function HeatmapSection({
  heatmapType,
  setHeatmapType,
  currentHeatmap,
  loadingHeatmap,
  isDark,
}: {
  heatmapType: HeatmapType
  setHeatmapType: (t: HeatmapType) => void
  currentHeatmap: HeatmapItem[]
  loadingHeatmap: boolean
  isDark: boolean
}) {
  return (
    <div className="bg-quant-card border border-quant-border rounded-xl p-3.5 shadow-sm">
      <div className="mb-2.5">
        <div className="flex flex-wrap gap-1 bg-quant-bg-secondary rounded-lg p-1">
          {HEATMAP_TABS.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setHeatmapType(tab.key)}
              className={cn(
                'flex-1 min-w-[calc(33.33%-4px)] max-w-[calc(33.33%-2px)] text-[10px] font-semibold h-[26px] rounded-md transition-all whitespace-nowrap overflow-hidden text-ellipsis',
                heatmapType === tab.key
                  ? 'bg-quant-card text-foreground shadow-sm border border-quant-border'
                  : 'text-muted-foreground hover:text-foreground bg-transparent'
              )}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </div>
      <div className="grid grid-cols-3 gap-1">
        {loadingHeatmap ? (
          Array.from({ length: 12 }).map((_, i) => <SkeletonCell key={i} />)
        ) : currentHeatmap.length > 0 ? (
          currentHeatmap.slice(0, 12).map((item, i) => (
            <div
              key={i}
              className="rounded-md p-1.5 text-center text-[9px] transition-transform hover:scale-[1.03] cursor-default"
              style={getHeatmapStyle(item.value, isDark)}
            >
              <span className="block font-semibold truncate">{getHeatmapName(item, heatmapType, true)}</span>
              {item.price != null && <span className="block opacity-80">{formatHeatmapPrice(item.price, heatmapType)}</span>}
              <span className="block font-bold text-[10px]">
                {item.value >= 0 ? '+' : ''}
                {formatNum(item.value)}%
              </span>
            </div>
          ))
        ) : (
          <div className="col-span-3 text-center py-5 text-xs text-muted-foreground">暂无数据</div>
        )}
      </div>
    </div>
  )
}
