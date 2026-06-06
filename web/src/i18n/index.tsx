import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'

export type Lang = 'zh-CN' | 'en-US'

export const LANGS = [
  { code: 'zh-CN' as Lang, label: '中文', flag: '🇨🇳' },
  { code: 'en-US' as Lang, label: 'English', flag: '🇺🇸' },
]

interface I18nContextType {
  lang: Lang
  setLang: (l: Lang) => void
  t: (key: string, fallback?: string) => string
}

const I18nContext = createContext<I18nContextType | null>(null)

/* ── Translation dictionaries ────────────────────────────────────── */

const translations: Record<Lang, Record<string, string>> = {
  'zh-CN': {},
  'en-US': {},
}

/** Register a locale dictionary */
export function registerLocale(locale: Lang, dict: Record<string, string>) {
  translations[locale] = { ...translations[locale], ...dict }
}

/** Flatten nested object to dot-notation keys */
export function flatten(obj: Record<string, unknown>, prefix = ''): Record<string, string> {
  const result: Record<string, string> = {}
  for (const key of Object.keys(obj)) {
    const newKey = prefix ? `${prefix}.${key}` : key
    if (typeof obj[key] === 'string') {
      result[newKey] = obj[key]
    } else if (typeof obj[key] === 'object' && obj[key] !== null) {
      Object.assign(result, flatten(obj[key] as Record<string, unknown>, newKey))
    }
  }
  return result
}

/* ── Provider ─────────────────────────────────────────────────────── */

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => {
    const saved = localStorage.getItem('xt-locale') as Lang | null
    return saved || 'zh-CN'
  })

  const setLang = useCallback((l: Lang) => {
    localStorage.setItem('xt-locale', l)
    setLangState(l)
  }, [])

  const t = useCallback(
    (key: string, fallback?: string) => {
      return translations[lang][key] ?? fallback ?? key
    },
    [lang]
  )

  const ctxValue = { lang, setLang, t }
  return (
    <I18nContext.Provider value={ctxValue}>
      {children}
    </I18nContext.Provider>
  )
}

/* ── Hook ────────────────────────────────────────────────────────── */

export function useI18n() {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within I18nProvider')
  return ctx
}
