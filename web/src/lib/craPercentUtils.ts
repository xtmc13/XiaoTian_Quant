/**
 * CRA parameter percentage ↔ decimal conversion utilities.
 *
 * The UI presents percentage values to users (e.g. 2.5 for 2.5%).
 * The backend engines expect decimal ratios (e.g. 0.025).
 */

export const PERCENT_TO_DECIMAL = 1 / 100
export const DECIMAL_TO_PERCENT = 100

// Thresholds for detecting whether an incoming value is already a percentage or still a decimal.
// If a value is <= threshold, we assume it is a decimal from the backend and multiply by 100.
export const PERCENTAGE_FIELD_THRESHOLDS: Record<string, number> = {
  tpRatio: 0.5,
  profitCallback: 0.005,
  waterfall: 0.2,
  stopLossRatio: 1.0,
  addPositionSpread: 0.5,
  addPositionCallback: 0.05,
  movingTPRatio: 0.5,
  movingTPDrawback: 1.0,
}

/** Convert a UI percentage value to the decimal ratio the backend expects. */
export function percentToDecimal(value: number): number {
  return round(value * PERCENT_TO_DECIMAL, 6)
}

/**
 * Convert a backend decimal ratio to a UI-friendly percentage.
 * Values that already look like percentages (greater than the field threshold)
 * are left untouched so legacy percentage-form payloads remain readable.
 */
export function decimalToPercent(value: number, threshold: number): number {
  return value <= threshold ? round(value * DECIMAL_TO_PERCENT, 6) : value
}

function round(value: number, decimals: number): number {
  const factor = 10 ** decimals
  return Math.round(value * factor) / factor
}
