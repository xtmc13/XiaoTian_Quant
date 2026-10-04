import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Settings } from '../Settings'
import { configApi, notifyRouteApi } from '@/lib/api'
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
      exchangesConfigured: vi.fn(),
      currencyGet: vi.fn().mockRejectedValue(new Error('skip')),
    },
    notifyRouteApi: {
      ...actual.notifyRouteApi,
      list: vi.fn().mockResolvedValue([]),
      save: vi.fn(),
      delete: vi.fn(),
      test: vi.fn(),
      channels: vi.fn(),
      send: vi.fn(),
    },
    riskApi: { ...actual.riskApi, getConfig: vi.fn(), updateConfig: vi.fn() },
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

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    </I18nProvider>
  )
}

async function renderNotifyTab() {
  render(<Settings />, { wrapper })
  const tab = await screen.findByText('通知', {}, { timeout: 5000 })
  fireEvent.click(tab)
  await waitFor(() => expect(screen.getByText('通知渠道状态')).toBeTruthy())
}

describe('Settings 通知区（渠道状态 + 测试通知）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(configApi.get).mockResolvedValue({} as never)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({} as never)
    vi.mocked(notifyRouteApi.channels).mockResolvedValue([
      { name: 'log', enabled: true, configured: true },
      { name: 'email', enabled: false, configured: false },
      { name: 'lark', enabled: true, configured: true },
    ])
    vi.mocked(notifyRouteApi.send).mockResolvedValue({ status: 'sent' })
  })

  it('渲染渠道状态徽标（已配置/未配置）', async () => {
    await renderNotifyTab()
    // lark 同时出现在状态徽标与渠道勾选按钮中
    await waitFor(() => expect(screen.getAllByText('lark').length).toBeGreaterThan(0))
    expect(screen.getAllByText('已配置').length).toBe(2)
    expect(screen.getByText('未配置')).toBeTruthy()
  })

  it('填写标题内容后发送测试通知', async () => {
    await renderNotifyTab()
    const titleInput = screen.getByPlaceholderText('例如：渠道联调测试')
    const contentInput = screen.getByPlaceholderText('通知正文内容')
    fireEvent.change(titleInput, { target: { value: '联调测试' } })
    fireEvent.change(contentInput, { target: { value: '这是一条测试通知' } })

    fireEvent.click(screen.getByRole('button', { name: '发送测试通知' }))
    await waitFor(() =>
      expect(notifyRouteApi.send).toHaveBeenCalledWith({
        title: '联调测试',
        content: '这是一条测试通知',
        level: 'INFO',
        channels: undefined,
      })
    )
    await waitFor(() =>
      expect(vi.mocked(useToastModule.toast)).toHaveBeenCalledWith('success', '测试通知已发送')
    )
  })

  it('勾选渠道后按选定渠道发送；缺内容拦截', async () => {
    await renderNotifyTab()
    // 等渠道按钮渲染，勾选 lark（状态徽标同名，取按钮角色）
    await waitFor(() => expect(screen.getAllByText('lark').length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole('button', { name: 'lark' }))

    const titleInput = screen.getByPlaceholderText('例如：渠道联调测试')
    const contentInput = screen.getByPlaceholderText('通知正文内容')
    fireEvent.change(titleInput, { target: { value: 't' } })
    fireEvent.change(contentInput, { target: { value: 'c' } })
    fireEvent.click(screen.getByRole('button', { name: '发送测试通知' }))

    await waitFor(() =>
      expect(notifyRouteApi.send).toHaveBeenCalledWith(expect.objectContaining({ channels: ['lark'] }))
    )

    // 清空内容再发 → 拦截
    vi.mocked(notifyRouteApi.send).mockClear()
    fireEvent.change(contentInput, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: '发送测试通知' }))
    await waitFor(() =>
      expect(vi.mocked(useToastModule.toast)).toHaveBeenCalledWith('warning', '请填写标题和内容')
    )
    expect(notifyRouteApi.send).not.toHaveBeenCalled()
  })
})
