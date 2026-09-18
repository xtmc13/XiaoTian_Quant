import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from 'react'

// ── Cloudflare Turnstile 人机验证组件（A3.2） ──
// 仅在 VITE_TURNSTILE_SITE_KEY 存在时渲染 widget；未配置则整页行为不变。
// 服务端校验由 TURNSTILE_SECRET_KEY 控制，两者需同时配置才生效。

const SITE_KEY = import.meta.env.VITE_TURNSTILE_SITE_KEY as string | undefined
const SCRIPT_URL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'

interface TurnstileRenderOptions {
  sitekey: string
  callback: (token: string) => void
  'expired-callback': () => void
  'error-callback': () => void
  theme?: string
}

interface TurnstileAPI {
  render: (el: HTMLElement, opts: TurnstileRenderOptions) => string
  reset: (widgetId: string) => void
  remove: (widgetId: string) => void
}

declare global {
  interface Window {
    turnstile?: TurnstileAPI
    __turnstileLoadPromise?: Promise<void>
  }
}

export interface TurnstileWidgetHandle {
  /** 每次提交后调用：清空 token 并让 widget 重新出题。 */
  reset: () => void
}

export function turnstileConfigured() {
  return !!SITE_KEY
}

function loadTurnstileScript(): Promise<void> {
  if (window.turnstile) return Promise.resolve()
  if (!window.__turnstileLoadPromise) {
    window.__turnstileLoadPromise = new Promise<void>((resolve, reject) => {
      const el = document.createElement('script')
      el.src = SCRIPT_URL
      el.async = true
      el.onload = () => resolve()
      el.onerror = () => reject(new Error('turnstile script load failed'))
      document.head.appendChild(el)
    })
  }
  return window.__turnstileLoadPromise
}

export const TurnstileWidget = forwardRef<
  TurnstileWidgetHandle,
  { onToken: (token: string | null) => void }
>(function TurnstileWidget({ onToken }, ref) {
  const containerRef = useRef<HTMLDivElement>(null)
  const widgetIdRef = useRef<string | null>(null)
  const [loadError, setLoadError] = useState(false)

  useImperativeHandle(ref, () => ({
    reset: () => {
      onToken(null)
      if (widgetIdRef.current && window.turnstile) {
        try {
          window.turnstile.reset(widgetIdRef.current)
        } catch {
          /* widget may already be removed */
        }
      }
    },
  }))

  useEffect(() => {
    if (!SITE_KEY) return
    let cancelled = false
    loadTurnstileScript()
      .then(() => {
        if (cancelled || !containerRef.current || !window.turnstile || widgetIdRef.current) return
        widgetIdRef.current = window.turnstile.render(containerRef.current, {
          sitekey: SITE_KEY,
          callback: (token) => onToken(token),
          'expired-callback': () => onToken(null),
          'error-callback': () => onToken(null),
          theme: 'dark',
        })
      })
      .catch(() => setLoadError(true))
    return () => {
      cancelled = true
      if (widgetIdRef.current && window.turnstile) {
        try {
          window.turnstile.remove(widgetIdRef.current)
        } catch {
          /* ignore */
        }
      }
      widgetIdRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (!SITE_KEY) return null

  return (
    <div className="flex flex-col items-center gap-1">
      <div ref={containerRef} />
      {loadError && <p className="text-[11px] text-red-400">人机验证组件加载失败，请刷新重试</p>}
    </div>
  )
})
