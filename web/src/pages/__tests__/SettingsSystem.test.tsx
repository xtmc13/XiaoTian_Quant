import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Settings } from '../Settings'
import { adminApi, configApi } from '@/lib/api'
import { I18nProvider } from '@/i18n'
import '@/i18n/locales/zh-CN'
import '@/i18n/locales/settings'
import * as useToastModule from '@/lib/useToast'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    configApi: {
      ...actual.configApi,
      get: vi.fn(),
      save: vi.fn(),
      aiModels: vi.fn(),
      exchangesConfigured: vi.fn(),
    },
    notifyRouteApi: { ...actual.notifyRouteApi, list: vi.fn().mockResolvedValue([]), save: vi.fn(), delete: vi.fn(), test: vi.fn() },
    riskApi: { ...actual.riskApi, getConfig: vi.fn(), updateConfig: vi.fn() },
    adminApi: { ...actual.adminApi, reloadConfig: vi.fn() },
    dataApi: { ...actual.dataApi, coverage: vi.fn().mockResolvedValue({}) },
  }
})

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

const appStoreState = {
  language: 'zh-CN',
  setLanguage: vi.fn(),
  theme: 'dark',
  setTheme: vi.fn(),
  uiScale: 1,
  setUiScale: vi.fn(),
  sidebarBehavior: 'fixed',
  setSidebarBehavior: vi.fn(),
}

vi.mock('@/stores/appStore', () => ({
  useAppStore: vi.fn((selector?: (s: unknown) => unknown) =>
    typeof selector === 'function' ? selector(appStoreState) : appStoreState
  ),
}))

// authStore 可变角色：admin 才可见"配置重载"卡片
let authRole: string = 'admin'
vi.mock('@/stores/authStore', () => ({
  useAuthStore: vi.fn((selector?: (s: unknown) => unknown) => {
    const state = { user: { id: 1, username: 'admin', role: authRole } }
    return typeof selector === 'function' ? selector(state) : state
  }),
}))

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    </I18nProvider>
  )
}

async function renderSystemTab() {
  render(<Settings />, { wrapper })
  const tab = await screen.findByText('系统')
  fireEvent.click(tab)
}

describe('Settings 系统区·配置重载（POST /admin/config/reload）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    authRole = 'admin'
    vi.mocked(configApi.get).mockResolvedValue({ default_ai_provider: 'openai', ai: {} } as never)
    vi.mocked(configApi.aiModels).mockResolvedValue({ providers: [] } as never)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({} as never)
    vi.mocked(adminApi.reloadConfig).mockResolvedValue({ status: 'reloaded', log_level: 'debug', timestamp: 1759000000000 })
  })

  it('admin 可见重载按钮，二次确认后调接口并 toast 透出后端 log_level', async () => {
    await renderSystemTab()
    const btn = await screen.findByRole('button', { name: /重载配置/ })
    fireEvent.click(btn)
    // 危险操作二次确认
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/确认从磁盘重载/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '重载配置' }))
    await waitFor(() => expect(adminApi.reloadConfig).toHaveBeenCalledTimes(1))
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('success', expect.stringContaining('debug'))
    )
  })

  it('取消确认则不调用 reload 接口', async () => {
    await renderSystemTab()
    const btn = await screen.findByRole('button', { name: /重载配置/ })
    fireEvent.click(btn)
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(adminApi.reloadConfig).not.toHaveBeenCalled()
  })

  it('接口失败时 toast 报错', async () => {
    vi.mocked(adminApi.reloadConfig).mockRejectedValue(new Error('HTTP 500'))
    await renderSystemTab()
    fireEvent.click(await screen.findByRole('button', { name: /重载配置/ }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '重载配置' }))
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('error', expect.stringContaining('HTTP 500'))
    )
  })

  it('非 admin 不渲染配置重载卡片', async () => {
    authRole = 'user'
    await renderSystemTab()
    // 系统状态卡片正常渲染，重载卡片不存在
    await screen.findByText('系统状态')
    expect(screen.queryByRole('button', { name: /重载配置/ })).toBeNull()
    expect(screen.queryByText('配置重载')).toBeNull()
  })
})
