import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

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

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { AgentChatPanel } from '../../AgentChatPanel'

const CONVERSATIONS = [
  { id: 'c1', title: 'BTC 分析', updated_at: Date.now() - 3_600_000 },
  { id: 'c2', title: '网格机器人', updated_at: Date.now() },
]

function renderFull() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <AgentChatPanel variant="full" open onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('Hermes 全屏助手', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    global.fetch = vi.fn().mockResolvedValue({ ok: true })
    chatMock.mockImplementation(() => ({ abort: vi.fn(), promise: Promise.resolve() }))
    listMock.mockResolvedValue({ success: true, conversations: CONVERSATIONS })
    getMock.mockResolvedValue({ success: true, id: 'c1', title: 'BTC 分析', messages: [] })
    renameMock.mockResolvedValue({ success: true })
    removeMock.mockResolvedValue({ success: true })
  })

  it('空态：大字标 + 标语 + 侧栏分组 + 状态条', async () => {
    renderFull()
    // 大字标
    expect(screen.getByText('小天助手')).toBeTruthy()
    // 侧栏会话（c2 是今天）
    await waitFor(() => expect(screen.getByText('BTC 分析')).toBeTruthy())
    expect(screen.getByText('网格机器人')).toBeTruthy()
    // 日期分组头
    expect(screen.getByText('今天')).toBeTruthy()
    // 状态条
    await waitFor(() => expect(screen.getByText('Gateway 就绪')).toBeTruthy())
    expect(screen.getByLabelText('版本').textContent).toContain('web v')
  })

  it('搜索会话过滤', async () => {
    renderFull()
    await waitFor(() => expect(screen.getByText('BTC 分析')).toBeTruthy())
    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '网格' } })
    expect(screen.queryByText('BTC 分析')).toBeNull()
    expect(screen.getByText('网格机器人')).toBeTruthy()
  })

  it('斜杠面板：/ 打开，Enter 执行 /new', async () => {
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '/' } })
    expect(screen.getByRole('listbox', { name: '命令面板' })).toBeTruthy()
    expect(screen.getByText('/new')).toBeTruthy()

    fireEvent.change(input, { target: { value: '/new' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    // 命令执行而非发送：不产生流式请求，输入被清空
    expect(chatMock).not.toHaveBeenCalled()
    expect((input as HTMLTextAreaElement).value).toBe('')
  })

  it('发送 → 用户全宽气泡 + 助手流式裸文本', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onDelta('你好，')
      handlers.onDelta('世界')
      handlers.onDone({ content: '你好，世界' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '在吗' } })
    fireEvent.click(screen.getByLabelText('发送消息'))

    await waitFor(() => expect(screen.getByText('你好，世界')).toBeTruthy())
    // 用户气泡（xt-human-bubble）
    expect(document.querySelector('.xt-human-bubble')?.textContent).toContain('在吗')
    expect(screen.getByText('在吗')).toBeTruthy()
  })

  it('打断重定向：忙时发送中断当前回合并以新输入续聊', async () => {
    let firstAbort!: () => void
    let resolveFirst!: () => void
    chatMock
      .mockImplementationOnce(() => {
        let resolveIt!: () => void
        const promise = new Promise<void>((resolve) => {
          resolveIt = resolve
          resolveFirst = resolve
        })
        firstAbort = vi.fn(() => resolveIt())
        return { abort: firstAbort, promise }
      })
      .mockImplementationOnce((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
        handlers.onDone({ content: '第二条回复' })
        return { abort: vi.fn(), promise: Promise.resolve() }
      })

    renderFull()
    const input = screen.getByLabelText('消息输入框')

    // 第一条：挂起的流式
    fireEvent.change(input, { target: { value: '第一条' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(1))

    // 忙时发送第二条 → steer：abort 被调用
    fireEvent.change(input, { target: { value: '第二条' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(firstAbort).toHaveBeenCalled())
    resolveFirst()

    // 第一条 promise 结束后自动以第二条开启新回合
    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(2))
    const [params] = chatMock.mock.calls[1] as [Record<string, unknown>]
    expect(params.messages).toEqual([
      { role: 'user', content: '第一条' },
      { role: 'user', content: '第二条' },
    ])
    await waitFor(() => expect(screen.getByText('第二条回复')).toBeTruthy())
  })

  it('忙时空输入显示停止键，Esc 停止', async () => {
    let abortFn: (() => void) | null = null
    chatMock.mockImplementationOnce(() => {
      const promise = new Promise<void>((resolve) => {
        abortFn = vi.fn(() => resolve())
      })
      return { abort: abortFn, promise }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '等很久的回复' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    // 忙 + 空输入 → 停止键出现（发送中输入已被清空）
    await waitFor(() => expect(screen.getByLabelText('停止生成')).toBeTruthy())
    fireEvent.keyDown(input, { key: 'Escape' })
    await waitFor(() => expect(abortFn).toHaveBeenCalled())
  })

  it('重命名与删除在侧栏菜单中可用', async () => {
    renderFull()
    await waitFor(() => expect(screen.getByText('BTC 分析')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('会话操作 BTC 分析'))
    fireEvent.click(screen.getByLabelText('删除会话 BTC 分析'))
    fireEvent.click(screen.getByLabelText('确认删除会话 BTC 分析'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith('c1'))
  })
})
