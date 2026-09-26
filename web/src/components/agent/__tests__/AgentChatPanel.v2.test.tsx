import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

// ── mock 后端 API ──
const chatMock = vi.fn()
const listMock = vi.fn()
const getMock = vi.fn()
const renameMock = vi.fn()
const removeMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentChatApi: {
      chat: (...args: unknown[]) => chatMock(...args),
    },
    agentConversationApi: {
      list: (...args: unknown[]) => listMock(...args),
      get: (...args: unknown[]) => getMock(...args),
      rename: (...args: unknown[]) => renameMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
    },
    configApi: {
      ...actual.configApi,
      getAIModels: () => Promise.resolve({ providers: [] }),
    },
  }
})

import { AgentChatPanel } from '../AgentChatPanel'

const CONVERSATIONS = [
  { id: 'c1', title: 'BTC 分析', updated_at: Date.now() - 3_600_000 },
  { id: 'c2', title: '网格机器人', updated_at: Date.now() },
]

const C2_MESSAGES = [
  { id: 'm1', role: 'user', content: '帮我看看持仓' },
  { id: 'm2', role: 'assistant', content: '你的持仓如下：BTC 0.5 个', reasoning: '先查持仓接口', tool_calls: [] },
]

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const invalidateSpy = vi.spyOn(client, 'invalidateQueries')
  const view = render(
    <QueryClientProvider client={client}>
      <AgentChatPanel open onClose={() => {}} />
    </QueryClientProvider>
  )
  return { client, invalidateSpy, ...view }
}

describe('AgentChatPanel v2', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    chatMock.mockImplementation(() => ({ abort: vi.fn(), promise: Promise.resolve() }))
    listMock.mockResolvedValue({ success: true, conversations: CONVERSATIONS })
    getMock.mockImplementation((id: string) =>
      Promise.resolve({
        success: true,
        id,
        title: CONVERSATIONS.find((c) => c.id === id)?.title || '',
        messages: id === 'c2' ? C2_MESSAGES : [],
      })
    )
    renameMock.mockResolvedValue({ success: true })
    removeMock.mockResolvedValue({ success: true })
  })

  it('1. 渲染会话列表，点击切换加载该会话消息', async () => {
    renderPanel()
    // 打开历史侧栏
    fireEvent.click(screen.getByLabelText('历史会话'))
    await waitFor(() => {
      expect(screen.getByText('BTC 分析')).toBeTruthy()
      expect(screen.getByText('网格机器人')).toBeTruthy()
    })
    // 点击切换 → 加载消息
    fireEvent.click(screen.getByText('网格机器人'))
    await waitFor(() => expect(getMock).toHaveBeenCalledWith('c2'))
    await waitFor(() => expect(screen.getByText('你的持仓如下：BTC 0.5 个')).toBeTruthy())
    expect(screen.getByText('帮我看看持仓')).toBeTruthy()
  })

  it('2. 发送消息→onDelta 追加→done 后显示并 invalidate 会话列表', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onDelta('你好，')
      handlers.onDelta('世界')
      handlers.onDone({ content: '你好，世界' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    const { invalidateSpy } = renderPanel()
    fireEvent.change(screen.getByLabelText('消息输入框'), { target: { value: '在吗' } })
    fireEvent.click(screen.getByLabelText('发送消息'))
    expect(chatMock).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(screen.getByText('你好，世界')).toBeTruthy())
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ['agent-conversations'] }))
    )
  })

  it('3. reasoning 分片渲染且可折叠（默认折叠）', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onReasoning('让我')
      handlers.onReasoning('想想')
      handlers.onDone({ content: '答案是 42', reasoning: '让我想想' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderPanel()
    fireEvent.change(screen.getByLabelText('消息输入框'), { target: { value: '1+1=?' } })
    fireEvent.keyDown(screen.getByLabelText('消息输入框'), { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('答案是 42')).toBeTruthy())
    const toggle = screen.getByText('思考过程')
    // 默认折叠：reasoning 内容不可见
    expect(screen.queryByText('让我想想')).toBeNull()
    // 展开后可见
    fireEvent.click(toggle)
    expect(screen.getByText('让我想想')).toBeTruthy()
  })

  it('4. 代码块渲染语言标签与复制按钮', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onDone({ content: '```python\nprint(1)\n```' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderPanel()
    fireEvent.change(screen.getByLabelText('消息输入框'), { target: { value: '写段代码' } })
    fireEvent.click(screen.getByLabelText('发送消息'))
    await waitFor(() => expect(screen.getByLabelText('复制代码')).toBeTruthy())
    expect(screen.getByText('python')).toBeTruthy()
    expect(document.querySelector('pre code')?.textContent).toContain('print(1)')
  })

  it('5. 重新生成以 regenerate=true 调用', async () => {
    renderPanel()
    // 加载含用户+助手消息的会话
    fireEvent.click(screen.getByLabelText('历史会话'))
    await waitFor(() => expect(screen.getByText('网格机器人')).toBeTruthy())
    fireEvent.click(screen.getByText('网格机器人'))
    await waitFor(() => expect(screen.getByText('你的持仓如下：BTC 0.5 个')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('重新生成回复'))
    expect(chatMock).toHaveBeenCalledTimes(1)
    const [params] = chatMock.mock.calls[0] as [Record<string, unknown>]
    expect(params.regenerate).toBe(true)
    expect(params.conversation_id).toBe('c2')
    // messages 只含该轮用户消息（不含被重生成的那条助手回复）
    expect(params.messages).toEqual([{ role: 'user', content: '帮我看看持仓' }])
  })

  it('6. 删除会话调用 remove', async () => {
    renderPanel()
    fireEvent.click(screen.getByLabelText('历史会话'))
    await waitFor(() => expect(screen.getByLabelText('删除会话 BTC 分析')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('删除会话 BTC 分析'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith('c1'))
  })
})
