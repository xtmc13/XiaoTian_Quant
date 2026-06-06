import type { HeatmapType } from './types'

export const HEATMAP_TABS: { key: HeatmapType; label: string }[] = [
  { key: 'us_stocks', label: '美股' },
  { key: 'hk_stocks', label: '港股' },
  { key: 'crypto', label: '加密' },
  { key: 'commodities', label: '商品' },
  { key: 'sectors', label: '板块' },
  { key: 'forex', label: '外汇' },
]

export const COUNTRY_FLAGS: Record<string, string> = {
  US: '🇺🇸', CN: '🇨🇳', EU: '🇪🇺', JP: '🇯🇵', UK: '🇬🇧', DE: '🇩🇪', AU: '🇦🇺', CA: '🇨🇦',
}

export const MARKET_COLORS: Record<string, string> = {
  USStock: 'bg-green-500',
  CNStock: 'bg-blue-500',
  HKStock: 'bg-indigo-500',
  Crypto: 'bg-purple-500',
  Forex: 'bg-yellow-500',
  Futures: 'bg-cyan-500',
}

export const MARKET_NAMES: Record<string, string> = {
  USStock: '美股', CNStock: 'A股', HKStock: '港股', Crypto: '加密', Forex: '外汇', Futures: '期货',
}
