import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const chatMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentApi: {
      ...actual.agentApi,
      aiConfig: () => Promise.resolve({ model: 'test-model', temperature: 0.7, max_tokens: 1024 }),
    },
    agentChatApi: {
      chat: (...args: unknown[]) => chatMock(...args),
    },
  }
})

import { AgentChatPanel, renderMarkdown } from '../AgentChatPanel'

function renderPanel(props?: Partial<React.ComponentProps<typeof AgentChatPanel>>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <AgentChatPanel open onClose={() => {}} {...props} />
    </QueryClientProvider>
  )
}

describe('renderMarkdown', () => {
  it('renders bold text', () => {
    const nodes = renderMarkdown('**BTC** 看涨')
    const html = render(<>{nodes}</>)
    expect(html.container.querySelector('strong')?.textContent).toBe('BTC')
  })

  it('renders fenced code blocks', () => {
    const nodes = renderMarkdown('说明\n```python\nprint(1)\n```\n完')
    const html = render(<>{nodes}</>)
    expect(html.container.querySelector('pre code')?.textContent).toContain('print(1)')
  })

  it('renders inline code', () => {
    const nodes = renderMarkdown('使用 `grid` 策略')
    const html = render(<>{nodes}</>)
    expect(html.container.querySelector('code')?.textContent).toBe('grid')
  })
})

describe('AgentChatPanel', () => {
  beforeEach(() => {
    localStorage.clear()
    chatMock.mockReset()
    chatMock.mockReturnValue({
      abort: vi.fn(),
      promise: new Promise(() => {}), // 永不结束，模拟生成中
    })
  })

  it('renders header, quick chips and input', () => {
    renderPanel()
    expect(screen.getByText('小天助手')).toBeTruthy()
    expect(screen.getByText('分析 BTCUSDT')).toBeTruthy()
    expect(screen.getByText('创建一个 BTC 网格机器人（模拟盘）')).toBeTruthy()
    expect(screen.getByLabelText('消息输入框')).toBeTruthy()
  })

  it('returns null when closed', () => {
    const { container } = renderPanel({ open: false })
    expect(container.firstChild).toBeNull()
  })

  it('sends quick chip message and shows user bubble', async () => {
    renderPanel()
    fireEvent.click(screen.getByText('我的持仓和盈亏'))
    await waitFor(() => {
      // 快捷芯片 + 用户气泡各出现一次
      expect(screen.getAllByText('我的持仓和盈亏').length).toBeGreaterThanOrEqual(2)
    })
    expect(chatMock).toHaveBeenCalledTimes(1)
    const [messages] = chatMock.mock.calls[0]
    expect(messages).toEqual([{ role: 'user', content: '我的持仓和盈亏' }])
  })

  it('persists history to localStorage (agent_chat_history)', async () => {
    renderPanel()
    fireEvent.click(screen.getByText('今日 AI 信号'))
    await waitFor(() => expect(chatMock).toHaveBeenCalled())
    const raw = localStorage.getItem('agent_chat_history')
    expect(raw).toBeTruthy()
    const saved = JSON.parse(raw!)
    expect(saved[0]).toMatchObject({ role: 'user', content: '今日 AI 信号' })
  })

  it('restores history from localStorage on mount', () => {
    localStorage.setItem(
      'agent_chat_history',
      JSON.stringify([{ role: 'user', content: '昨天的提问' }])
    )
    renderPanel()
    expect(screen.getByText('昨天的提问')).toBeTruthy()
  })
})
