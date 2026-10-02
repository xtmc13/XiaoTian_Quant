import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const listMock = vi.fn()
const runMock = vi.fn()
const getRunMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentEvalsApi: {
      list: (...args: unknown[]) => listMock(...args),
      run: (...args: unknown[]) => runMock(...args),
      getRun: (...args: unknown[]) => getRunMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { EvalsPanel } from '../EvalsPanel'

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <EvalsPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

const SUITES = [
  { id: 'tools', name: '工具调用套件', count: 2 },
  { id: 'chat', name: '闲聊套件', count: 5 },
]

describe('EvalsPanel 评测面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染套件列表（名称 + 用例数 + 运行按钮）', async () => {
    listMock.mockResolvedValue({ success: true, suites: SUITES, runs: [] })
    renderPanel()

    await waitFor(() => expect(screen.getByText('工具调用套件')).toBeTruthy())
    expect(screen.getByText('闲聊套件')).toBeTruthy()
    expect(screen.getByText('2 用例')).toBeTruthy()
    expect(screen.getByText('5 用例')).toBeTruthy()
    expect(screen.getByRole('button', { name: '运行 工具调用套件' })).toBeTruthy()
  })

  it('运行 → 轮询 → 展示通过率与逐条用例（✓/✗、工具 chips、缺失关键词）', async () => {
    listMock.mockResolvedValue({ success: true, suites: SUITES, runs: [] })
    runMock.mockResolvedValue({ success: true, run_id: 'r1' })
    getRunMock.mockResolvedValue({
      success: true,
      id: 'r1',
      suite: 'tools',
      status: 'done',
      started_at: Math.floor(Date.now() / 1000) - 10,
      finished_ms: 3456,
      pass: 1,
      total: 2,
      cases: [
        {
          name: '查持仓',
          ok: true,
          expected_tools: ['get_positions'],
          used_tools: ['get_positions'],
          expected_keywords: ['BTC'],
          missing_keywords: [],
          answer_summary: '当前持有 0.5 BTC。',
        },
        {
          name: '下单买入',
          ok: false,
          expected_tools: ['place_order'],
          used_tools: ['get_price'],
          expected_keywords: ['已买入', '成交'],
          missing_keywords: ['成交'],
          answer_summary: '我没有执行买入，因为行情不明确，建议再观望一段时间等待更好的入场时机。',
        },
      ],
    })
    renderPanel()

    await waitFor(() => expect(screen.getByRole('button', { name: '运行 工具调用套件' })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: '运行 工具调用套件' }))
    await waitFor(() => expect(runMock).toHaveBeenCalledWith('tools'))

    // 结果卡：通过率大数字（未全过 → 红）
    await waitFor(() => expect(screen.getByLabelText('评测通过率').textContent).toBe('1/2'))
    // 用例行：✓/✗ + 名称
    await waitFor(() => expect(screen.getByText('查持仓')).toBeTruthy())
    expect(screen.getByLabelText('通过')).toBeTruthy()
    expect(screen.getByLabelText('未通过')).toBeTruthy()
    // 期望工具 vs 实际工具 chips（每个用例各一组）
    expect(screen.getAllByText('期望工具').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('实际工具').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('place_order')).toBeTruthy()
    expect(screen.getByText('get_price')).toBeTruthy()
    // 缺失关键词红色文案 + 回答摘要
    expect(screen.getByText(/缺失关键词：成交/)).toBeTruthy()
    expect(screen.getByText(/我没有执行买入/)).toBeTruthy()
  })

  it('409：toast 已有评测在运行', async () => {
    const { ApiError } = await import('@/lib/api')
    const { toast } = await import('@/lib/useToast')
    listMock.mockResolvedValue({ success: true, suites: SUITES, runs: [] })
    runMock.mockRejectedValue(new ApiError('已有评测在运行', 409))
    renderPanel()

    await waitFor(() => expect(screen.getByRole('button', { name: '运行 工具调用套件' })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: '运行 工具调用套件' }))
    await waitFor(() => expect(toast).toHaveBeenCalledWith('warning', '已有评测在运行'))
  })

  it('列表中已有运行时：面板以 2s 间隔轮询', async () => {
    listMock.mockResolvedValue({
      success: true,
      suites: SUITES,
      runs: [
        {
          id: 'r9',
          suite: 'tools',
          status: 'running',
          started_at: Math.floor(Date.now() / 1000),
          finished_ms: 0,
          pass: 0,
          total: 0,
        },
      ],
    })
    getRunMock.mockResolvedValue({
      success: true,
      id: 'r9',
      suite: 'tools',
      status: 'running',
      started_at: Math.floor(Date.now() / 1000),
      finished_ms: 0,
      pass: 0,
      total: 0,
      cases: [],
    })
    renderPanel()

    await waitFor(() => expect(screen.getByText('运行中…')).toBeTruthy())
    const calls = listMock.mock.calls.length
    await waitFor(() => expect(listMock.mock.calls.length).toBeGreaterThan(calls), { timeout: 4000 })
  })
})
