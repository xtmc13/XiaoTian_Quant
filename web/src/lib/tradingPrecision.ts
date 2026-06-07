export interface TradingPrecision {
  price: number
  quantity: number
}

export const TRADING_PRECISION: Record<string, TradingPrecision> = {
  BTCUSDT: { price: 2, quantity: 4 },
  ETHUSDT: { price: 2, quantity: 3 },
  SOLUSDT: { price: 2, quantity: 2 },
  BNBUSDT: { price: 2, quantity: 3 },
  DOGEUSDT: { price: 4, quantity: 0 },
  ADAUSDT: { price: 4, quantity: 2 },
  XRPUSDT: { price: 4, quantity: 1 },
  AVAXUSDT: { price: 2, quantity: 3 },
  DOTUSDT: { price: 2, quantity: 3 },
  MATICUSDT: { price: 4, quantity: 2 },
  LINKUSDT: { price: 2, quantity: 3 },
  ATOMUSDT: { price: 2, quantity: 3 },
  LTCUSDT: { price: 2, quantity: 4 },
  TRXUSDT: { price: 4, quantity: 0 },
  BCHUSDT: { price: 2, quantity: 4 },
}

export function getPrecision(symbol: string): TradingPrecision {
  return TRADING_PRECISION[symbol] || { price: 2, quantity: 4 }
}