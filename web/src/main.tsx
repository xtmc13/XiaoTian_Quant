import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import './index.css'

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

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </StrictMode>
)
