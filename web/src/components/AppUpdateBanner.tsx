import { useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useI18n } from '@/i18n'

/**
 * 版本更新横幅：App 构建版本（VITE_APP_VERSION，Docker 构建期注入）与服务端
 * /api/health 的构建版本不一致时，底部弹出"点击更新"——根治部署后客户端仍跑
 * 旧缓存包的问题（2026-10-10 实证：手机端整段会话零文档请求，纯本地缓存
 * 启动旧版前端）。index.html 已 no-store + SW 文档 network-first，reload
 * 即可拿到新版本。dev 构建（未注入版本号）不启用。
 */
export function AppUpdateBanner() {
  const { t } = useI18n()
  const [serverVersion, setServerVersion] = useState('')

  useEffect(() => {
    const buildVersion = (import.meta.env.VITE_APP_VERSION as string) || ''
    if (!buildVersion || buildVersion === 'dev') return
    let stopped = false
    const check = async () => {
      try {
        const res = await fetch('/api/health')
        if (!res.ok) return
        const data = await res.json()
        const v = data?.data?.version
        if (!stopped && typeof v === 'string' && v && v !== buildVersion) setServerVersion(v)
      } catch {
        /* 网络失败不提示 */
      }
    }
    check()
    const timer = setInterval(check, 60_000)
    const onVisible = () => {
      if (document.visibilityState === 'visible') check()
    }
    window.addEventListener('focus', check)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      stopped = true
      clearInterval(timer)
      window.removeEventListener('focus', check)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [])

  if (!serverVersion) return null
  return (
    <button
      onClick={() => window.location.reload()}
      data-testid="app-update-banner"
      className="fixed bottom-16 md:bottom-4 left-1/2 -translate-x-1/2 z-[90] flex items-center gap-2 px-4 py-2 rounded-full bg-quant-gold text-white text-xs font-medium shadow-lg hover:opacity-90 transition-opacity"
    >
      <RefreshCw className="w-3.5 h-3.5" />
      {t('app.updateAvailable').replace('{version}', serverVersion)}
    </button>
  )
}
