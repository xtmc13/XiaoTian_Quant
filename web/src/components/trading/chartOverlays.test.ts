import { describe, it, expect } from 'vitest'
import {
  buildOrderPriceLines,
  buildPositionPriceLine,
  computeSpotAvgEntryPrice,
  extractEntryPrice,
  formatLinePrice,
  syncPriceLines,
  MAX_ORDER_LINES,
  ORDER_LINE_COLOR,
  POSITION_LINE_COLOR,
  type ActivePriceLine,
  type ChartApiLike,
  type ChartPriceLine,
  type OrderLike,
} from './chartOverlays'

describe('formatLinePrice', () => {
  it('按精度格式化价格', () => {
    expect(formatLinePrice(65012.3456, 2)).toBe('65012.35')
    expect(formatLinePrice(0.123456, 4)).toBe('0.1235')
  })
  it('非法价格返回占位符', () => {
    expect(formatLinePrice(NaN, 2)).toBe('--')
    expect(formatLinePrice(0, 2)).toBe('--')
    expect(formatLinePrice(-1, 2)).toBe('--')
  })
  it('精度被钳制在 0-8 位', () => {
    expect(formatLinePrice(1.23456789, 20)).toBe('1.23456789')
    expect(formatLinePrice(1.5, -1)).toBe('2')
  })
})

describe('extractEntryPrice', () => {
  it('按字段优先级提取', () => {
    expect(extractEntryPrice({ avg_entry_price: 100, entryPrice: 200 })).toBe(100)
    expect(extractEntryPrice({ entry_price: 110, entryPrice: 200 })).toBe(110)
    expect(extractEntryPrice({ entryPrice: 200 })).toBe(200)
    expect(extractEntryPrice({ avgPrice: 300 })).toBe(300)
    expect(extractEntryPrice({ openPrice: 400 })).toBe(400)
  })
  it('无有效价格返回 null', () => {
    expect(extractEntryPrice(null)).toBeNull()
    expect(extractEntryPrice({})).toBeNull()
    expect(extractEntryPrice({ avg_entry_price: 0 })).toBeNull()
    expect(extractEntryPrice({ avg_entry_price: NaN })).toBeNull()
  })
})

describe('buildPositionPriceLine', () => {
  it('生成绿色实线,带图例与轴标注', () => {
    const [line] = buildPositionPriceLine({ avg_entry_price: 65000.123 }, 2, '持仓均价')
    expect(line.price).toBe(65000.123)
    expect(line.text).toBe('65000.12')
    expect(line.legend).toBe('持仓均价')
    expect(line.color).toBe(POSITION_LINE_COLOR)
    expect(line.dashed).toBe(false)
    expect(line.id).toBe('xt-chart-position')
  })
  it('无持仓返回空数组', () => {
    expect(buildPositionPriceLine(null, 2)).toEqual([])
  })
})

describe('buildOrderPriceLines', () => {
  const orders: OrderLike[] = [
    { id: '1', symbol: 'BTCUSDT', side: 'BUY', type: 'LIMIT', status: 'NEW', price: 60000, quantity: 1 },
    { id: '2', symbol: 'BTCUSDT', side: 'SELL', type: 'LIMIT', status: 'PARTIALLY_FILLED', price: 70000, quantity: 1 },
    { id: '3', symbol: 'BTCUSDT', side: 'BUY', type: 'STOP_LIMIT', status: 'NEW', price: 65000, quantity: 1 },
    // 以下应被过滤
    { id: '4', symbol: 'ETHUSDT', side: 'BUY', type: 'LIMIT', status: 'NEW', price: 3000, quantity: 1 },
    { id: '5', symbol: 'BTCUSDT', side: 'BUY', type: 'MARKET', status: 'NEW', price: 0, quantity: 1 },
    { id: '6', symbol: 'BTCUSDT', side: 'BUY', type: 'LIMIT', status: 'FILLED', price: 61000, quantity: 1 },
    { id: '7', symbol: 'BTCUSDT', side: 'BUY', type: 'LIMIT', status: 'CANCELLED', price: 62000, quantity: 1 },
    { id: '8', symbol: 'BTCUSDT', side: 'BUY', type: 'LIMIT', status: 'NEW', price: -5, quantity: 1 },
  ]

  it('只保留当前 symbol 的未成交限价/条件单', () => {
    const lines = buildOrderPriceLines('BTCUSDT', orders, 2)
    expect(lines.map((l) => l.id).sort()).toEqual(['xt-chart-order-1', 'xt-chart-order-2', 'xt-chart-order-3'])
  })
  it('委托线为黄色虚线,图例区分买卖方向', () => {
    const lines = buildOrderPriceLines('BTCUSDT', orders, 2)
    const buy = lines.find((l) => l.id === 'xt-chart-order-1')!
    const sell = lines.find((l) => l.id === 'xt-chart-order-2')!
    expect(buy.color).toBe(ORDER_LINE_COLOR)
    expect(buy.dashed).toBe(true)
    expect(buy.legend).toBe('买入委托')
    expect(sell.legend).toBe('卖出委托')
    expect(buy.text).toBe('60000.00')
  })
  it('空订单/空 symbol 返回空数组', () => {
    expect(buildOrderPriceLines('BTCUSDT', [], 2)).toEqual([])
    expect(buildOrderPriceLines('BTCUSDT', undefined, 2)).toEqual([])
    expect(buildOrderPriceLines('', orders, 2)).toEqual([])
  })
  it('委托线数量超出上限时截断', () => {
    const many = Array.from({ length: MAX_ORDER_LINES + 5 }, (_, i) => ({
      id: String(i),
      symbol: 'BTCUSDT',
      side: 'BUY',
      type: 'LIMIT',
      status: 'NEW',
      price: 60000 + i,
      quantity: 1,
    }))
    expect(buildOrderPriceLines('BTCUSDT', many, 2)).toHaveLength(MAX_ORDER_LINES)
  })
})

describe('computeSpotAvgEntryPrice', () => {
  it('买入卖出按成交量加权平均', () => {
    const history: OrderLike[] = [
      { id: 'a', symbol: 'BTCUSDT', side: 'BUY', price: 60000, avg_price: 60000, filled_quantity: 1 },
      { id: 'b', symbol: 'BTCUSDT', side: 'BUY', price: 62000, avg_price: 62000, filled_quantity: 1 },
    ]
    expect(computeSpotAvgEntryPrice('BTCUSDT', history)).toBe(61000)
  })
  it('卖出减少净持仓与成本', () => {
    const history: OrderLike[] = [
      { id: 'a', symbol: 'BTCUSDT', side: 'BUY', price: 60000, filled_quantity: 2 },
      { id: 'b', symbol: 'BTCUSDT', side: 'SELL', price: 61000, filled_quantity: 1 },
    ]
    // (2*60000 - 1*61000) / (2-1) = 59000
    expect(computeSpotAvgEntryPrice('BTCUSDT', history)).toBe(59000)
  })
  it('净数量 <= 0 或成本 <= 0 返回 null', () => {
    const soldAll: OrderLike[] = [
      { id: 'a', symbol: 'BTCUSDT', side: 'BUY', price: 60000, filled_quantity: 1 },
      { id: 'b', symbol: 'BTCUSDT', side: 'SELL', price: 61000, filled_quantity: 1 },
    ]
    expect(computeSpotAvgEntryPrice('BTCUSDT', soldAll)).toBeNull()
    expect(computeSpotAvgEntryPrice('BTCUSDT', [])).toBeNull()
    expect(computeSpotAvgEntryPrice('BTCUSDT', undefined)).toBeNull()
  })
  it('忽略其他交易对与无效价格', () => {
    const history: OrderLike[] = [
      { id: 'a', symbol: 'ETHUSDT', side: 'BUY', price: 3000, filled_quantity: 1 },
      { id: 'b', symbol: 'BTCUSDT', side: 'BUY', price: 0, filled_quantity: 1 },
      { id: 'c', symbol: 'BTCUSDT', side: 'BUY', price: 60000, filled_quantity: 1 },
    ]
    expect(computeSpotAvgEntryPrice('BTCUSDT', history)).toBe(60000)
  })
})

describe('syncPriceLines', () => {
  function makeMockApi() {
    const calls = { created: [] as unknown[], overridden: [] as unknown[], removed: [] as string[] }
    const api: ChartApiLike = {
      createOverlay: (v) => {
        calls.created.push(v)
        return 'id'
      },
      overrideOverlay: (v) => {
        calls.overridden.push(v)
      },
      removeOverlay: (id) => {
        calls.removed.push(id)
      },
    }
    return { api, calls }
  }
  const line = (id: string, price: number): ChartPriceLine => ({
    id,
    price,
    text: formatLinePrice(price, 2),
    legend: '委托',
    color: ORDER_LINE_COLOR,
    dashed: true,
  })

  it('首次同步创建 overlay 并记录 active', () => {
    const { api, calls } = makeMockApi()
    const active = new Map<string, ActivePriceLine>()
    syncPriceLines(api, active, [line('a', 60000), line('b', 61000)])
    expect(calls.created).toHaveLength(2)
    expect(calls.removed).toHaveLength(0)
    expect(active.size).toBe(2)
    const createPayload = calls.created[0] as Record<string, unknown>
    expect(createPayload.name).toBe('simpleTag')
    expect(createPayload.lock).toBe(true)
    expect(createPayload.id).toBe('a')
    expect(createPayload.points).toEqual([{ value: 60000 }])
  })

  it('相同数据重复同步不产生任何调用', () => {
    const { api, calls } = makeMockApi()
    const active = new Map<string, ActivePriceLine>()
    const lines = [line('a', 60000), line('b', 61000)]
    syncPriceLines(api, active, lines)
    syncPriceLines(api, active, lines)
    expect(calls.created).toHaveLength(2)
    expect(calls.overridden).toHaveLength(0)
    expect(calls.removed).toHaveLength(0)
  })

  it('价格变化时原位 override 而不是重建', () => {
    const { api, calls } = makeMockApi()
    const active = new Map<string, ActivePriceLine>()
    syncPriceLines(api, active, [line('a', 60000)])
    syncPriceLines(api, active, [line('a', 60500)])
    expect(calls.created).toHaveLength(1)
    expect(calls.removed).toHaveLength(0)
    expect(calls.overridden).toHaveLength(1)
    expect(calls.overridden[0]).toEqual({ id: 'a', points: [{ value: 60500 }], extendData: '60500.00' })
    expect(active.get('a')).toEqual({ price: 60500, text: '60500.00' })
  })

  it('线消失时 removeOverlay 并清理 active', () => {
    const { api, calls } = makeMockApi()
    const active = new Map<string, ActivePriceLine>()
    syncPriceLines(api, active, [line('a', 60000), line('b', 61000)])
    syncPriceLines(api, active, [line('b', 61000)])
    expect(calls.removed).toEqual(['a'])
    expect(active.has('a')).toBe(false)
    expect(active.has('b')).toBe(true)
  })

  it('清空时移除全部', () => {
    const { api, calls } = makeMockApi()
    const active = new Map<string, ActivePriceLine>()
    syncPriceLines(api, active, [line('a', 60000)])
    syncPriceLines(api, active, [])
    expect(calls.removed).toEqual(['a'])
    expect(active.size).toBe(0)
  })

  it('图表 API 抛错时不中断且维持 active 状态', () => {
    const api: ChartApiLike = {
      createOverlay: () => {
        throw new Error('chart gone')
      },
      overrideOverlay: () => {
        throw new Error('chart gone')
      },
      removeOverlay: () => {
        throw new Error('chart gone')
      },
    }
    const active = new Map<string, ActivePriceLine>()
    expect(() => syncPriceLines(api, active, [line('a', 60000)])).not.toThrow()
    // create 失败不记录 active,后续同步会重试
    expect(active.size).toBe(0)
    expect(() => syncPriceLines(api, active, [])).not.toThrow()
  })
})
