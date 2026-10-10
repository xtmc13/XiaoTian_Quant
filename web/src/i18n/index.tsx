import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'

export type Lang = 'zh-CN' | 'en-US' | 'ja'

export const LANGS = [
  { code: 'zh-CN' as Lang, label: '中文', flag: '🇨🇳' },
  { code: 'en-US' as Lang, label: 'English', flag: '🇺🇸' },
  { code: 'ja' as Lang, label: '日本語', flag: '🇯🇵' },
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
  ja: {},
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
    document.documentElement.lang = l
    setLangState(l)
  }, [])

  const t = useCallback(
    (key: string, fallback?: string) => {
      // 非中文语言缺词条时回退中文，逐步覆盖期间页面不会露出 key
      return translations[lang][key] ?? translations['zh-CN'][key] ?? fallback ?? key
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

/**
 * useI18nSafe 无 Provider 时不抛错：回退 zh-CN 直查表。用于整体硬编码中文、
 * 只内嵌个别 i18n 词条的组件（如 StrategyCreateForm 的交易所偏好说明 L11），
 * 避免测试/独立渲染必须包 I18nProvider。
 */
export function useI18nSafe(): I18nContextType {
  const ctx = useContext(I18nContext)
  if (ctx) return ctx
  return {
    lang: 'zh-CN',
    setLang: () => {},
    t: (key: string, fallback?: string) => translations['zh-CN'][key] ?? fallback ?? key,
  }
}
