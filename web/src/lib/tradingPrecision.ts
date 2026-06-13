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

/** Dynamic precision overrides — updated at runtime from exchange info API. */
const dynamicPrecision: Record<string, TradingPrecision> = {}

/** Register precision info for a symbol, typically from exchange API response. */
export function registerPrecision(symbol: string, price: number, quantity: number) {
  dynamicPrecision[symbol] = { price, quantity }
}

/** Register multiple precision entries at once. */
export function registerPrecisionMap(map: Record<string, TradingPrecision>) {
  for (const [sym, prec] of Object.entries(map)) {
    dynamicPrecision[sym] = prec
  }
}

export function getPrecision(symbol: string): TradingPrecision {
  return dynamicPrecision[symbol] || TRADING_PRECISION[symbol] || { price: 2, quantity: 4 }
}