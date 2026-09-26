import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AIRobotPanel } from '../AIRobotPanel'
import { aiRobotApi } from '@/lib/api'
import * as useToastModule from '@/lib/useToast'

vi.mock('@/lib/api', () => ({
  aiRobotApi: {
    getConfig: vi.fn(),
    saveConfig: vi.fn(),
    getModels: vi.fn(),
    getStatus: vi.fn(),
    getSignals: vi.fn(),
  },
}))

vi.mock('@/lib/useToast', async () => {
  const actual = await vi.importActual<typeof import('@/lib/useToast')>('@/lib/useToast')
  return { ...actual, toast: vi.fn() }
})

const SAVED_CONFIG = {
  id: 'cfg-1',
  model: 'qwen',
  confidence_threshold: 80,
  scan_interval_seconds: 600,
  market_filters: {
    min_volume_24h: 2000000,
    max_volatility: 8,
    trend_timeframe: '4h',
    require_trend_alignment: false,
    filter_whitelist_only: true,
  },
  enabled: true,
}

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {children}
    </QueryClientProvider>
  )
}

describe('AIRobotPanel AI 机器人面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(aiRobotApi.getConfig).mockResolvedValue(SAVED_CONFIG as never)
    vi.mocked(aiRobotApi.getModels).mockResolvedValue(['deepseek', 'claude', 'qwen'] as never)
    vi.mocked(aiRobotApi.getStatus).mockResolvedValue({
      signals_today: 3,
      avg_confidence: 72,
      filter_rate: 40,
      win_rate: 55,
      model: 'qwen',
      enabled: true,
    } as never)
    vi.mocked(aiRobotApi.getSignals).mockResolvedValue([] as never)
    vi.mocked(aiRobotApi.saveConfig).mockResolvedValue({} as never)
  })

  it('配置加载回填：模型/置信度/扫描间隔/enabled 初始化控件', async () => {
    render(<AIRobotPanel />, { wrapper })
    // Select 回填为已保存模型
    await waitFor(() =>
      expect((screen.getByLabelText('选择模型') as HTMLSelectElement).value).toBe('qwen')
    )
    expect(screen.getByText('置信度门限: 80%')).toBeTruthy()
    expect(screen.getByText('扫描间隔: 10分钟')).toBeTruthy()
    expect((screen.getByLabelText('启用 AI 扫描机器人') as HTMLInputElement).checked).toBe(true)
    // 市场过滤回填 require_trend_alignment=false
    expect((screen.getByLabelText('市场条件过滤') as HTMLInputElement).checked).toBe(false)
  })

  it('空配置时使用默认值渲染', async () => {
    vi.mocked(aiRobotApi.getConfig).mockResolvedValue({} as never)
    render(<AIRobotPanel />, { wrapper })
    await waitFor(() => expect(aiRobotApi.getConfig).toHaveBeenCalled())
    await waitFor(() =>
      expect((screen.getByLabelText('选择模型') as HTMLSelectElement).value).toBe('deepseek')
    )
    expect(screen.getByText('置信度门限: 60%')).toBeTruthy()
    expect(screen.getByText('扫描间隔: 5分钟')).toBeTruthy()
  })

  it('保存配置：调用 saveConfig 并 toast 成功', async () => {
    render(<AIRobotPanel />, { wrapper })
    await waitFor(() =>
      expect((screen.getByLabelText('选择模型') as HTMLSelectElement).value).toBe('qwen')
    )
    fireEvent.click(screen.getByText('保存配置'))
    await waitFor(() => expect(aiRobotApi.saveConfig).toHaveBeenCalledTimes(1))
    const payload = vi.mocked(aiRobotApi.saveConfig).mock.calls[0][0]
    expect(payload.model).toBe('qwen')
    expect(payload.confidence_threshold).toBe(80)
    expect(payload.scan_interval_seconds).toBe(600)
    expect(payload.enabled).toBe(true)
    expect(payload.market_filters?.require_trend_alignment).toBe(false)
    expect(useToastModule.toast).toHaveBeenCalledWith('success', 'AI 机器人配置已保存')
  })

  it('信号流渲染：方向/置信度/理由/过滤器/市场状态', async () => {
    vi.mocked(aiRobotApi.getSignals).mockResolvedValue([
      {
        id: 'sig-1',
        symbol: 'BTCUSDT',
        signal: 'long',
        confidence: 85,
        reason: '放量突破关键阻力位',
        filters: ['trend_up', 'volume_spike'],
        market_condition: 'bullish',
        mode: 'spot',
        provider: 'deepseek',
        created_at: '2026-09-26T01:00:00Z',
      },
      {
        id: 'sig-2',
        symbol: 'ETHUSDT',
        signal: 'short',
        confidence: 45,
        reason: '顶背离',
        filters: [],
        market_condition: 'volatile',
        created_at: '2026-09-26T02:00:00Z',
      },
    ] as never)
    render(<AIRobotPanel />, { wrapper })
    await waitFor(() => expect(screen.getByText('BTCUSDT')).toBeTruthy())
    expect(screen.getByText('做多')).toBeTruthy()
    expect(screen.getByText('做空')).toBeTruthy()
    expect(screen.getByText('放量突破关键阻力位')).toBeTruthy()
    expect(screen.getByText('trend_up')).toBeTruthy()
    expect(screen.getByText('volume_spike')).toBeTruthy()
    expect(screen.getByText('bullish')).toBeTruthy()
    // 置信度进度条
    expect(screen.getAllByRole('progressbar').length).toBe(2)
  })

  it('信号流空态：提示开启扫描', async () => {
    render(<AIRobotPanel />, { wrapper })
    await waitFor(() =>
      expect(
        screen.getByText('暂无信号——开启扫描后这里会显示 AI 实时信号')
      ).toBeTruthy()
    )
  })
})
