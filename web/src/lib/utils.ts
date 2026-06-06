import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatCurrency(val: number): string {
  return new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(val)
}

export function formatPercent(val: number): string {
  return `${val >= 0 ? '+' : ''}${val.toFixed(2)}%`
}

export function classNames(...classes: (string | false | null | undefined)[]) {
  return classes.filter(Boolean).join(' ')
}

/* ── Currency Converter ── */
let conversionRate = 7.25
let preferredCurrency = 'CNY'
const currencySymbols: Record<string, string> = {
  CNY: '¥', USD: '$', EUR: '€', GBP: '£', JPY: '¥',
  KRW: '₩', HKD: 'HK$', TWD: 'NT$', SGD: 'S$', AUD: 'A$',
}
export const getConversionRate = () => conversionRate
export const getPreferredCurrency = () => preferredCurrency
export const getCurrencySymbol = () => currencySymbols[preferredCurrency] || preferredCurrency
export const formatConverted = (usd: number) => {
  const converted = usd * conversionRate
  const sym = currencySymbols[preferredCurrency] || (preferredCurrency + ' ')
  if (preferredCurrency === 'USD') return `$${converted.toFixed(2)}`
  return `${sym}${converted.toFixed(2)}`
}
export const setConversion = (rate: number, currency: string) => {
  conversionRate = rate
  preferredCurrency = currency
}
