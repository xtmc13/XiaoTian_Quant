import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import './index.css'
import { registerSW, listenInstallPrompt } from './lib/pwa'
import { bootstrapAppearance } from './stores/appStore'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/en-US'
import '@/i18n/locales/nav'
import '@/i18n/locales/settings'
import '@/i18n/locales/dashboard'
import '@/i18n/locales/arb'
import '@/i18n/locales/chrome'
import '@/i18n/locales/marketdata'
import '@/i18n/locales/market'
import '@/i18n/locales/portfolio'
import '@/i18n/locales/trading'

// 渲染前应用持久化的主题/缩放，避免刷新后外观回跳
bootstrapAppearance()

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 数据在 5 分钟内视为新鲜，减少重复请求
      staleTime: 5 * 60 * 1000,
      // 缓存保留 10 分钟（gcTime 替代 v4 的 cacheTime）
      gcTime: 10 * 60 * 1000,
      // 失败时重试 2 次，指数退避
      retry: 2,
      retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 30000),
      // 窗口重新聚焦时不自动刷新（交易场景避免干扰）
      refetchOnWindowFocus: false,
      // 网络恢复时自动刷新
      refetchOnReconnect: true,
      // 组件挂载时若数据已过期则刷新
      refetchOnMount: 'always',
    },
    mutations: {
      // 默认错误时重试 1 次（写操作幂等性需业务层保证）
      retry: 1,
      retryDelay: 1000,
    },
  },
})

// Register PWA Service Worker (disabled in dev to avoid stale cache)
// registerSW()
// listenInstallPrompt()

// 发版后旧标签页引用的 chunk 哈希会失效（动态 import 拉取失败）——检测到后
// 自动刷新一次加载新资源，避免停留在"页面加载异常"。sessionStorage 防止刷新循环。
const CHUNK_RELOAD_KEY = 'xt-chunk-reload'
function isChunkLoadError(msg: string): boolean {
  return /dynamically imported module|Failed to fetch dynamically|error loading dynamically|ChunkLoadError|Loading chunk \d+ failed/i.test(
    msg
  )
}
function reloadOnceOnChunkError(msg: string) {
  if (!isChunkLoadError(msg)) return
  if (sessionStorage.getItem(CHUNK_RELOAD_KEY)) return
  sessionStorage.setItem(CHUNK_RELOAD_KEY, '1')
  window.location.reload()
}
window.addEventListener('unhandledrejection', (e) => {
  reloadOnceOnChunkError(String((e.reason as Error)?.message ?? e.reason ?? ''))
})
window.addEventListener('error', (e) => {
  reloadOnceOnChunkError(String(e.message ?? ''))
  const target = e.target as HTMLScriptElement | null
  if (target && target.tagName === 'SCRIPT' && target.src) reloadOnceOnChunkError('ChunkLoadError')
})
// 正常加载后清除标记，允许下次发版再自动刷新
sessionStorage.removeItem(CHUNK_RELOAD_KEY)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>
)
