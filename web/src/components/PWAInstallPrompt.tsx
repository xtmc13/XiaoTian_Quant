import { useState, useEffect } from 'react'
import { Download, X, Smartphone } from 'lucide-react'
import { isStandalone, canInstall, promptInstall } from '@/lib/pwa'

/**
 * PWAInstallPrompt — shows a banner when the app can be installed.
 * Dismissed state is persisted in localStorage.
 */
export function PWAInstallPrompt() {
  const [visible, setVisible] = useState(false)
  const [installing, setInstalling] = useState(false)

  useEffect(() => {
    // Don't show if already installed
    if (isStandalone()) return

    // Don't show if user previously dismissed
    const dismissed = localStorage.getItem('xt-pwa-dismissed')
    if (dismissed) return

    // Listen for the deferred prompt becoming available
    const check = () => {
      if (canInstall()) setVisible(true)
    }

    // Check immediately and after a delay (prompt may fire after load)
    check()
    const timer = setTimeout(check, 2000)

    // Also listen for the custom event from pwa.ts
    window.addEventListener('beforeinstallprompt', check as EventListener)

    return () => {
      clearTimeout(timer)
      window.removeEventListener('beforeinstallprompt', check as EventListener)
    }
  }, [])

  const handleInstall = async () => {
    setInstalling(true)
    const accepted = await promptInstall()
    setInstalling(false)
    if (accepted) {
      setVisible(false)
    }
  }

  const handleDismiss = () => {
    localStorage.setItem('xt-pwa-dismissed', 'true')
    setVisible(false)
  }

  if (!visible) return null

  return (
    <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[9998] w-[90vw] max-w-md">
      <div className="bg-quant-card border border-quant-border rounded-xl shadow-xl p-4 flex items-center gap-3 animate-in slide-in-from-bottom fade-in duration-300">
        <div className="w-10 h-10 rounded-lg bg-quant-gold/10 flex items-center justify-center shrink-0">
          <Smartphone className="w-5 h-5 text-quant-gold" />
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium text-foreground">安装小天量化</p>
          <p className="text-xs text-muted-foreground truncate">
            添加到主屏幕，离线可用，启动更快
          </p>
        </div>
        <button
          onClick={handleInstall}
          disabled={installing}
          className="shrink-0 inline-flex items-center gap-1 px-3 py-1.5 rounded-lg bg-quant-gold text-white text-xs font-medium hover:opacity-90 transition-opacity disabled:opacity-50"
        >
          <Download className="w-3.5 h-3.5" />
          {installing ? '安装中...' : '安装'}
        </button>
        <button
          onClick={handleDismiss}
          className="shrink-0 p-1 rounded text-muted-foreground hover:text-foreground transition-colors"
          aria-label="关闭"
        >
          <X className="w-4 h-4" />
        </button>
      </div>
    </div>
  )
}
