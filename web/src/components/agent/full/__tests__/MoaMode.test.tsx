import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const chatMock = vi.fn()
const listMock = vi.fn()
const getMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentChatApi: {
      chat: (...args: unknown[]) => chatMock(...args),
      approve: vi.fn().mockResolvedValue({ success: true }),
    },
    agentConversationApi: {
      list: (...args: unknown[]) => listMock(...args),
      get: (...args: unknown[]) => getMock(...args),
      rename: vi.fn().mockResolvedValue({ success: true }),
      remove: vi.fn().mockResolvedValue({ success: true }),
      search: vi.fn().mockResolvedValue({ success: true, results: [] }),
      undo: vi.fn().mockResolvedValue({ success: true, remaining: 0 }),
      compress: vi.fn().mockResolvedValue({ success: true, summary: 's', compressed_count: 1 }),
      branch: vi.fn().mockResolvedValue({ success: true, id: 'c_new', title: '分叉' }),
    },
    agentSubagentsApi: { list: vi.fn().mockResolvedValue({ success: true, runs: [] }) },
    agentUserAiConfigApi: {
      get: vi.fn().mockResolvedValue({ success: true, has_key: false }),
      put: vi.fn().mockResolvedValue({ success: true }),
      remove: vi.fn().mockResolvedValue({ success: true }),
    },
    configApi: {
      ...actual.configApi,
      getAIModels: () => Promise.resolve({ providers: [] }),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { AgentChatPanel } from '../../AgentChatPanel'

function renderFull() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <AgentChatPanel variant="full" open onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('MoA 多模型综合模式', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    global.fetch = vi.fn().mockResolvedValue({ ok: true })
    chatMock.mockImplementation(() => ({ abort: vi.fn(), promise: Promise.resolve() }))
    listMock.mockResolvedValue({ success: true, conversations: [] })
    getMock.mockResolvedValue({ success: true, id: 'c1', title: 't', messages: [] })
  })

  it('/moa 显示 chip，× 取消，不发送请求', async () => {
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '/moa' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(screen.getByLabelText('MoA 模式')).toBeTruthy())
    expect(screen.getByText('MoA 多模型')).toBeTruthy()
    expect(chatMock).not.toHaveBeenCalled()

    fireEvent.click(screen.getByLabelText('取消 MoA 模式'))
    await waitFor(() => expect(screen.queryByLabelText('MoA 模式')).toBeNull())
  })

  it('/moa 后发送：请求携带 moa:true 且 chip 自动清除；再次发送不带 moa', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onDone({ content: '综合回答' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '/moa' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(screen.getByLabelText('MoA 模式')).toBeTruthy())

    const input2 = screen.getByLabelText('消息输入框')
    fireEvent.change(input2, { target: { value: '对比一下 BTC 和 ETH' } })
    fireEvent.keyDown(input2, { key: 'Enter' })

    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(1))
    const [params] = chatMock.mock.calls[0] as [Record<string, unknown>]
    expect(params.moa).toBe(true)
    // chip 发送后自动清除
    await waitFor(() => expect(screen.queryByLabelText('MoA 模式')).toBeNull())

    // 第二次发送不再携带 moa
    const input3 = screen.getByLabelText('消息输入框')
    fireEvent.change(input3, { target: { value: '第二条' } })
    fireEvent.keyDown(input3, { key: 'Enter' })
    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(2))
    const [params2] = chatMock.mock.calls[1] as [Record<string, unknown>]
    expect(params2.moa).toBeUndefined()
  })

  it('moa SSE 事件（≥2 家提案）：流式状态条显示综合提示', async () => {
    let resolveStream!: () => void
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onMoa({ proposers: ['kimi', 'deepseek'] })
      handlers.onDelta('综合中')
      const promise = new Promise<void>((resolve) => {
        resolveStream = resolve
      })
      return { abort: vi.fn(() => resolveStream()), promise }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '你好' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    // 流式状态条内展示综合提示
    await waitFor(() => expect(screen.getByText('综合 2 家模型：kimi、deepseek')).toBeTruthy())
    expect(screen.getByLabelText('生成状态')).toBeTruthy()
    resolveStream()
  })

  it('moa SSE 事件携带 note：toast 提示；空 proposers 不显示综合提示', async () => {
    const { toast } = await import('@/lib/useToast')
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onMoa({ proposers: [], note: 'MoA 不可用，已回退为普通回复' })
      handlers.onDone({ content: '普通回答' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '你好' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(toast).toHaveBeenCalledWith('info', 'MoA 不可用，已回退为普通回复'))
    expect(screen.queryByText(/综合 \d+ 家模型/)).toBeNull()
  })
})
