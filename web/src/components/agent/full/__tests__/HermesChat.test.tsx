import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const chatMock = vi.fn()
const approveMock = vi.fn()
const listMock = vi.fn()
const getMock = vi.fn()
const renameMock = vi.fn()
const removeMock = vi.fn()
const searchMock = vi.fn()
const undoMock = vi.fn()
const compressMock = vi.fn()
const branchMock = vi.fn()
const subagentsMock = vi.fn()
const userAiConfigGetMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentChatApi: {
      chat: (...args: unknown[]) => chatMock(...args),
      approve: (...args: unknown[]) => approveMock(...args),
    },
    agentConversationApi: {
      list: (...args: unknown[]) => listMock(...args),
      get: (...args: unknown[]) => getMock(...args),
      rename: (...args: unknown[]) => renameMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
      search: (...args: unknown[]) => searchMock(...args),
      undo: (...args: unknown[]) => undoMock(...args),
      compress: (...args: unknown[]) => compressMock(...args),
      branch: (...args: unknown[]) => branchMock(...args),
    },
    agentSubagentsApi: {
      list: (...args: unknown[]) => subagentsMock(...args),
    },
    agentUserAiConfigApi: {
      get: (...args: unknown[]) => userAiConfigGetMock(...args),
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
    approveMock.mockResolvedValue({ success: true })
    listMock.mockResolvedValue({ success: true, conversations: CONVERSATIONS })
    getMock.mockResolvedValue({ success: true, id: 'c1', title: 'BTC 分析', messages: [] })
    renameMock.mockResolvedValue({ success: true })
    removeMock.mockResolvedValue({ success: true })
    searchMock.mockResolvedValue({ success: true, results: [] })
    undoMock.mockResolvedValue({ success: true, remaining: 0 })
    compressMock.mockResolvedValue({ success: true, summary: 's', compressed_count: 7 })
    branchMock.mockResolvedValue({ success: true, id: 'c_new', title: '分叉会话' })
    subagentsMock.mockResolvedValue({ success: true, runs: [] })
    userAiConfigGetMock.mockResolvedValue({ success: true, has_key: false })
  })

  it('空态：鲸鱼标语 + 徽章 + 侧栏分组', async () => {
    renderFull()
    // 空态标语（与登录页一致）
    expect(screen.getByText('AI 驱动的量化交易平台')).toBeTruthy()
    expect(screen.getByText('预览版')).toBeTruthy()
    // 品牌行 + 面包屑药丸各有一处"小天量化"
    expect(screen.getAllByText('小天量化').length).toBeGreaterThanOrEqual(2)
    // 侧栏会话（c2 是今天）
    await waitFor(() => expect(screen.getByText('BTC 分析')).toBeTruthy())
    expect(screen.getByText('网格机器人')).toBeTruthy()
    // 日期分组头
    expect(screen.getByText('今天')).toBeTruthy()
  })

  it('搜索会话走服务端接口并展示摘要', async () => {
    searchMock.mockResolvedValue({
      success: true,
      results: [{ id: 'c2', title: '网格机器人', updated_at: Date.now(), snippet: '…创建一个 BTC 网格机器人…' }],
    })
    renderFull()
    await waitFor(() => expect(screen.getByText('BTC 分析')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('搜索会话开关'))
    fireEvent.change(screen.getByLabelText('搜索会话'), { target: { value: '网格' } })
    // 防抖 300ms 后调用服务端搜索
    await waitFor(() => expect(searchMock).toHaveBeenCalledWith('网格'), { timeout: 2000 })
    // 搜索结果（标题 + 摘要）替换分组列表
    await waitFor(() => expect(screen.getByText('…创建一个 BTC 网格机器人…')).toBeTruthy())
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

  it('指标行：done 携带 usage 时展示真实用量', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onDone({
        content: '你好，世界',
        usage: { prompt_tokens: 1234, completion_tokens: 56, llm_ms: 2345, first_token_ms: 300, tok_per_s: 42.5 },
      })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '在吗' } })
    fireEvent.click(screen.getByLabelText('发送消息'))

    await waitFor(() => expect(screen.getByText('你好，世界')).toBeTruthy())
    // 指标行展示真实用量而非字符粗估
    await waitFor(() =>
      expect(screen.getByText(/1 轮 · 0 步 \| LLM 2\.3s · 42\.5 tok\/s \| 输入 1234 tok · 输出 56 tok/)).toBeTruthy()
    )
  })

  it('done 携带 reasoning：流式标志立即清除，不残留「思考中」', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onDone({ content: '好的，我在', reasoning: '用户在打招呼，简短回应即可。' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '在吗' } })
    fireEvent.click(screen.getByLabelText('发送消息'))

    await waitFor(() => expect(screen.getByText('好的，我在')).toBeTruthy())
    // reasoning 随 done 到达时 ThinkingDisclosure 直接以非流式态渲染，无 shimmer / 思考中
    await waitFor(() => {
      expect(document.querySelector('.xt-shimmer-text')).toBeNull()
      expect(screen.queryByText(/思考中/)).toBeNull()
    })
  })

  it('撤销这一轮：调用 undo 接口并移除最后一轮消息', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onConversation({ id: 'c9', title: '在吗' })
      handlers.onDone({ content: '你好，世界', conversation_id: 'c9' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '在吗' } })
    fireEvent.click(screen.getByLabelText('发送消息'))

    await waitFor(() => expect(screen.getByText('你好，世界')).toBeTruthy())
    // 仅最后一条助手消息上有撤销入口
    await waitFor(() => expect(screen.getByLabelText('撤销这一轮')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('撤销这一轮'))
    await waitFor(() => expect(undoMock).toHaveBeenCalledWith('c9'))
    // 最后一轮（用户 + 助手）被移除
    await waitFor(() => expect(screen.queryByText('你好，世界')).toBeNull())
  })

  it('打断重定向：忙时点击发送键中断当前回合并以新输入续聊', async () => {
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

    // 忙时点击发送键 → steer：abort 被调用（Enter 则是排队，不打断）
    //（发送后布局从空态簇切到聊天视图，输入框重新挂载，需重新查询）
    const input2 = screen.getByLabelText('消息输入框')
    fireEvent.change(input2, { target: { value: '第二条' } })
    fireEvent.click(screen.getByLabelText('发送消息'))
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

    // 忙 + 空输入 → 停止键出现（composer 停止键 + 流式状态条停止键，发送后布局切换重新查询）
    await waitFor(() => expect(screen.getAllByLabelText('停止生成').length).toBeGreaterThanOrEqual(1))
    fireEvent.keyDown(screen.getByLabelText('消息输入框'), { key: 'Escape' })
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

  it('审批流：approval_request 渲染审批卡，批准后调用 approve 并清除卡片', async () => {
    let resolveStream!: () => void
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onApprovalRequest({ id: 'ap_x', tool: 'place_paper_order', args_summary: '买入 0.1 BTC' })
      // 回合暂停：流保持挂起
      const promise = new Promise<void>((resolve) => {
        resolveStream = resolve
      })
      return { abort: vi.fn(() => resolveStream()), promise }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '帮我买入' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    // 审批卡出现：工具名 + 参数摘要 + 批准/拒绝
    await waitFor(() => expect(screen.getByRole('alertdialog', { name: '工具调用审批' })).toBeTruthy())
    expect(screen.getByText('等待批准：place_paper_order')).toBeTruthy()
    expect(screen.getByText('买入 0.1 BTC')).toBeTruthy()
    // 流式状态仍在（生成状态条可见）
    expect(screen.getByText('正在生成…')).toBeTruthy()

    fireEvent.click(screen.getByLabelText('批准工具调用'))
    await waitFor(() => expect(approveMock).toHaveBeenCalledWith('ap_x', true))
    // 卡片清除
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: '工具调用审批' })).toBeNull())
    resolveStream()
  })

  it('/deny 拒绝待审批的工具调用', async () => {
    let resolveStream!: () => void
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onApprovalRequest({ id: 'ap_y', tool: 'place_paper_order', args_summary: '卖出 0.5 ETH' })
      const promise = new Promise<void>((resolve) => {
        resolveStream = resolve
      })
      return { abort: vi.fn(() => resolveStream()), promise }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '帮我卖出' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(screen.getByRole('alertdialog', { name: '工具调用审批' })).toBeTruthy())

    // 斜杠命令拒绝
    const input2 = screen.getByLabelText('消息输入框')
    fireEvent.change(input2, { target: { value: '/deny' } })
    fireEvent.keyDown(input2, { key: 'Enter' })
    await waitFor(() => expect(approveMock).toHaveBeenCalledWith('ap_y', false))
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: '工具调用审批' })).toBeNull())
    resolveStream()
  })

  it('排队：忙时 Enter 进入队列，当前回合结束后自动发送', async () => {
    let resolveFirst!: () => void
    let firstAbort!: () => void
    chatMock
      .mockImplementationOnce(() => {
        const promise = new Promise<void>((resolve) => {
          resolveFirst = resolve
        })
        firstAbort = vi.fn()
        return { abort: firstAbort, promise }
      })
      .mockImplementationOnce((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
        handlers.onDone({ content: '排队后的回复' })
        return { abort: vi.fn(), promise: Promise.resolve() }
      })

    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '第一条' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(1))

    // 忙时 Enter → 排队（不打断当前回合）
    const input2 = screen.getByLabelText('消息输入框')
    fireEvent.change(input2, { target: { value: '第二条' } })
    fireEvent.keyDown(input2, { key: 'Enter' })
    await waitFor(() => expect(screen.getByLabelText('排队消息')).toBeTruthy())
    expect(firstAbort).not.toHaveBeenCalled()
    expect(chatMock).toHaveBeenCalledTimes(1)

    // 当前回合结束 → 自动发送排队消息
    resolveFirst()
    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(2))
    const [params] = chatMock.mock.calls[1] as [Record<string, unknown>]
    expect(params.messages).toEqual([
      { role: 'user', content: '第一条' },
      { role: 'user', content: '第二条' },
    ])
    await waitFor(() => expect(screen.getByText('排队后的回复')).toBeTruthy())
    // 队列已清空
    expect(screen.queryByLabelText('排队消息')).toBeNull()
  })

  it('旁问：/btw 打开输入框，ephemeral 请求且回答不进入消息流', async () => {
    chatMock.mockImplementation((params: Record<string, unknown>, handlers: Record<string, (v: unknown) => void>) => {
      if (params.ephemeral) {
        handlers.onDelta('这是旁问回答')
        handlers.onDone({ content: '这是旁问回答' })
      }
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '/btw' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    // 旁问输入框出现
    const btwInput = await waitFor(() => screen.getByLabelText('旁问输入框'))
    fireEvent.change(btwInput, { target: { value: 'BTC 现在多少' } })
    fireEvent.keyDown(btwInput, { key: 'Enter' })

    await waitFor(() => expect(chatMock).toHaveBeenCalledTimes(1))
    const [params] = chatMock.mock.calls[0] as [Record<string, unknown>]
    expect(params.ephemeral).toBe(true)
    expect(params.messages).toEqual([{ role: 'user', content: 'BTC 现在多少' }])
    // 瞬态卡片渲染回答，且不进入消息流（空态仍在）
    await waitFor(() => expect(screen.getByText('这是旁问回答')).toBeTruthy())
    expect(screen.getByText('AI 驱动的量化交易平台')).toBeTruthy()
    // 关闭旁问
    fireEvent.click(screen.getByLabelText('关闭旁问'))
    await waitFor(() => expect(screen.queryByText('这是旁问回答')).toBeNull())
  })

  it('压缩：SSE compressed 事件展示一次性提示', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onCompressed({ compressed_count: 5 })
      handlers.onDelta('好的')
      handlers.onDone({ content: '好的' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '继续' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('（已自动压缩 5 条早期消息）')).toBeTruthy())
  })

  it('/compress 调用压缩接口并提示条数', async () => {
    chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
      handlers.onConversation({ id: 'c9', title: '在吗' })
      handlers.onDone({ content: '你好', conversation_id: 'c9' })
      return { abort: vi.fn(), promise: Promise.resolve() }
    })
    const { toast } = await import('@/lib/useToast')
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '在吗' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('你好')).toBeTruthy())

    const input2 = screen.getByLabelText('消息输入框')
    fireEvent.change(input2, { target: { value: '/compress' } })
    fireEvent.keyDown(input2, { key: 'Enter' })
    await waitFor(() => expect(compressMock).toHaveBeenCalledWith('c9'))
    await waitFor(() => expect(toast).toHaveBeenCalledWith('success', '已压缩 7 条早期消息'))
  })

  it('分叉：侧栏菜单「从此处分叉」调用 branch 并切换到新会话', async () => {
    getMock.mockImplementation((id: string) =>
      Promise.resolve({ success: true, id, title: id === 'c_new' ? '分叉会话' : 'BTC 分析', messages: [] })
    )
    renderFull()
    await waitFor(() => expect(screen.getByText('BTC 分析')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('会话操作 BTC 分析'))
    fireEvent.click(screen.getByLabelText('分叉会话 BTC 分析'))
    // 空会话 → message_index = 0（末尾分叉）
    await waitFor(() => expect(branchMock).toHaveBeenCalledWith('c1', 0))
    // 切换到新会话
    await waitFor(() => expect(getMock).toHaveBeenCalledWith('c_new'))
  })

  it('子代理面板：/agents 打开并渲染运行状态', async () => {
    subagentsMock.mockResolvedValue({
      success: true,
      runs: [
        {
          id: 'r1',
          task: '调研 BTC 舆情',
          status: 'running',
          started_at: Math.floor(Date.now() / 1000),
          finished_ms: 0,
          result_summary: '',
        },
        {
          id: 'r2',
          task: '汇总季度财报',
          status: 'done',
          started_at: Math.floor(Date.now() / 1000) - 60,
          finished_ms: 1234,
          result_summary: '财报要点：营收增长',
        },
      ],
    })
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '/agents' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(screen.getByRole('dialog', { name: '子代理' })).toBeTruthy())
    await waitFor(() => expect(screen.getByText('调研 BTC 舆情')).toBeTruthy())
    expect(screen.getByText('运行中')).toBeTruthy()
    expect(screen.getByText('汇总季度财报')).toBeTruthy()
    expect(screen.getByText('完成')).toBeTruthy()
    expect(screen.getByText('财报要点：营收增长')).toBeTruthy()
  })

  it('子代理面板：接口未就绪时降级为提示空态', async () => {
    subagentsMock.mockRejectedValue(new Error('404'))
    renderFull()
    const input = screen.getByLabelText('消息输入框')
    fireEvent.change(input, { target: { value: '/agents' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(screen.getByText(/子代理服务暂不可用/)).toBeTruthy())
  })
})
