import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Settings } from '../Settings'
import { configApi } from '@/lib/api'
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
      aiTest: vi.fn(),
      aiModels: vi.fn(),
      listProviderModels: vi.fn(),
      exchangesConfigured: vi.fn(),
      saveExchangeCredentials: vi.fn(),
    },
    notifyRouteApi: { ...actual.notifyRouteApi, list: vi.fn().mockResolvedValue([]), save: vi.fn(), delete: vi.fn(), test: vi.fn() },
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
  // 兼容两种用法：useAppStore() 全量返回 / useAppStore(selector)
  useAppStore: vi.fn((selector?: (s: unknown) => unknown) =>
    typeof selector === 'function' ? selector(appStoreState) : appStoreState
  ),
}))

const CATALOG = {
  providers: [
    { key: 'openai', label: 'OpenAI', models: ['gpt-5.5', 'gpt-4.1'], baseUrl: 'https://api.openai.com/v1', default: 'gpt-5.5', configured: true, current_model: 'gpt-5.5' },
    { key: 'claude', label: 'Anthropic Claude', models: ['claude-opus-4-7', 'claude-sonnet-4-6'], baseUrl: 'https://api.anthropic.com', default: 'claude-opus-4-7', configured: false },
    { key: 'deepseek', label: 'DeepSeek', models: ['deepseek-chat'], baseUrl: 'https://api.deepseek.com/v1', default: 'deepseek-chat', configured: false },
    { key: 'kimi', label: 'Kimi（月之暗面）', models: ['kimi-k2.5', 'k3', 'kimi-for-coding'], baseUrl: 'https://api.moonshot.cn/v1', default: 'kimi-k2.5', configured: false },
  ],
}

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <I18nProvider>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        {children}
      </QueryClientProvider>
    </I18nProvider>
  )
}

async function renderAITab() {
  render(<Settings />, { wrapper })
  // 等 config 查询完成、左侧导航渲染后再切 tab
  const tab = await screen.findByText('AI 模型')
  fireEvent.click(tab)
  // provider 名同时出现在默认下拉 option 与卡片标题中，用 getAllByText
  await waitFor(() => expect(screen.getAllByText('Anthropic Claude').length).toBeGreaterThan(0))
}

describe('Settings AI 模型设置', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(configApi.get).mockResolvedValue({ default_ai_provider: 'openai', ai: {} } as never)
    vi.mocked(configApi.aiModels).mockResolvedValue(CATALOG as never)
    vi.mocked(configApi.exchangesConfigured).mockResolvedValue({} as never)
    vi.mocked(configApi.save).mockResolvedValue({} as never)
    vi.mocked(configApi.saveExchangeCredentials).mockResolvedValue({} as never)
    vi.mocked(configApi.aiTest).mockResolvedValue({ success: true, message: '连接成功 · deepseek-chat', latency_ms: 320 } as never)
  })

  it('按后端目录渲染 provider 卡片与已配置标识', async () => {
    await renderAITab()
    expect(screen.getAllByText('OpenAI').length).toBeGreaterThan(0)
    expect(screen.getAllByText('DeepSeek').length).toBeGreaterThan(0)
    expect(screen.getByText('已配置')).toBeTruthy()
    // 默认提供商下拉用 key 存值：select 的 value 应为 key
    const defaultSelect = screen.getByLabelText('默认提供商') as HTMLSelectElement
    expect(defaultSelect.value).toBe('openai')
  })

  it('模型选择器默认取目录 default 且候选完整', async () => {
    await renderAITab()
    const claudeSelect = screen.getByLabelText('Anthropic Claude 默认模型') as HTMLSelectElement
    expect(claudeSelect.value).toBe('claude-opus-4-7')
    // 候选 = 目录模型 + 自定义项
    expect(claudeSelect.options.length).toBe(3)
    expect(claudeSelect.options[2].value).toBe('__custom__')
  })

  it('legacy anthropic 配置迁移到 claude', async () => {
    vi.mocked(configApi.get).mockResolvedValue({
      default_ai_provider: 'anthropic',
      ai: { anthropic: { api_key: 'sk-old', model: 'claude-sonnet-4-6' } },
    } as never)
    await renderAITab()
    // default 迁移：anthropic → claude
    await waitFor(() => {
      const defaultSelect = screen.getByLabelText('默认提供商') as HTMLSelectElement
      expect(defaultSelect.value).toBe('claude')
    })
    const claudeInput = screen.getByLabelText('Anthropic Claude 默认模型') as HTMLInputElement
    expect(claudeInput.value).toBe('claude-sonnet-4-6')
  })

  it('测试按钮携带表单 model/base_url 调 aiTest', async () => {
    vi.mocked(configApi.get).mockResolvedValue({
      default_ai_provider: 'deepseek',
      ai: { deepseek: { api_key: 'sk-ds', model: 'deepseek-chat' } },
    } as never)
    await renderAITab()
    const buttons = screen.getAllByText('测试连接')
    // 第三个卡片是 deepseek（目录顺序 openai/claude/deepseek）
    fireEvent.click(buttons[2])
    await waitFor(() =>
      expect(configApi.aiTest).toHaveBeenCalledWith({
        provider: 'deepseek',
        api_key: 'sk-ds',
        model: 'deepseek-chat',
        base_url: 'https://api.deepseek.com/v1',
      })
    )
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('success', '连接成功 · deepseek-chat')
    )
  })

  it('目录接口失败时回落内置完整目录（含 glm/kimi 等）', async () => {
    vi.mocked(configApi.aiModels).mockRejectedValue(new Error('network') as never)
    render(<Settings />, { wrapper })
    const tab = await screen.findByText('AI 模型')
    fireEvent.click(tab)
    await waitFor(() => expect(screen.getAllByText('智谱 GLM').length).toBeGreaterThan(0))
    expect(screen.getAllByText('Kimi（月之暗面）').length).toBeGreaterThan(0)
  })

  it('拉取模型按钮调 listProviderModels 并填充 datalist 候选', async () => {
    vi.mocked(configApi.get).mockResolvedValue({
      default_ai_provider: 'claude',
      ai: { claude: { api_key: 'sk-ant', model: 'claude-opus-4-7' } },
    } as never)
    vi.mocked(configApi.listProviderModels).mockResolvedValue({
      success: true,
      models: ['claude-opus-4-7', 'claude-sonnet-4-6', 'claude-haiku-4-5'],
    } as never)
    await renderAITab()
    // 每个 provider 卡片各有一个按钮：目录顺序 openai(0)/claude(1)/deepseek(2)
    fireEvent.click(screen.getAllByText('拉取模型')[1])
    await waitFor(() =>
      expect(configApi.listProviderModels).toHaveBeenCalledWith({
        provider: 'claude',
        api_key: 'sk-ant',
        base_url: 'https://api.anthropic.com',
      })
    )
    // 下拉候选被实时列表替换（+ 自定义项）
    await waitFor(() => {
      const sel = screen.getByLabelText('Anthropic Claude 默认模型') as HTMLSelectElement
      expect(sel.options.length).toBe(4)
      expect(sel.options[2].value).toBe('claude-haiku-4-5')
    })
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('success', expect.stringContaining('3'))
    )
  })

  it('拉取失败时 toast 报错且不改 datalist', async () => {
    vi.mocked(configApi.listProviderModels).mockResolvedValue({
      success: false,
      message: 'deepseek 拉取失败: HTTP 401',
    } as never)
    await renderAITab()
    fireEvent.click(screen.getAllByText('拉取模型')[2])
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith('error', 'deepseek 拉取失败: HTTP 401')
    )
    const sel = screen.getByLabelText('DeepSeek 默认模型') as HTMLSelectElement
    expect(sel.options.length).toBe(2) // 目录候选 1 + 自定义项，拉取失败不变
  })

  it('Kimi 卡片"订阅版端点"按钮一键填充 coding 网关地址与模型', async () => {
    await renderAITab()
    fireEvent.click(screen.getByText('订阅版端点'))
    const baseUrlInput = screen.getByLabelText('Kimi（月之暗面） Base URL') as HTMLInputElement
    const modelInput = screen.getByLabelText('Kimi（月之暗面） 默认模型') as HTMLInputElement
    expect(baseUrlInput.value).toBe('https://api.kimi.com/coding/v1')
    expect(modelInput.value).toBe('kimi-for-coding')
    await waitFor(() =>
      expect(useToastModule.toast).toHaveBeenCalledWith(
        'success',
        expect.stringContaining('api.kimi.com/coding')
      )
    )
  })
})
