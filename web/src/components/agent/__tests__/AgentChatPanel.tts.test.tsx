import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const chatMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentChatApi: {
      chat: (...args: unknown[]) => chatMock(...args),
      approve: vi.fn().mockResolvedValue({ success: true }),
    },
    agentConversationApi: {
      list: () => Promise.resolve({ success: true, conversations: [] }),
      get: vi.fn(),
      rename: vi.fn(),
      remove: vi.fn(),
      search: vi.fn().mockResolvedValue({ success: true, results: [] }),
      undo: vi.fn(),
      compress: vi.fn(),
      branch: vi.fn(),
    },
    agentPluginApi: { list: () => Promise.resolve({ plugins: [] }) },
    agentSkillApi: { list: () => Promise.resolve({ skills: [] }) },
    configApi: {
      ...actual.configApi,
      getAIModels: () => Promise.resolve({ providers: [] }),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { AgentChatPanel } from '../AgentChatPanel'
import { TTS_KEY } from '../useTts'

const speakMock = vi.fn()
const cancelMock = vi.fn()

class FakeUtterance {
  text: string
  lang = ''
  voice: unknown = null
  onend: (() => void) | null = null
  constructor(text: string) {
    this.text = text
  }
}

function renderFull() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <AgentChatPanel variant="full" open onClose={() => {}} />
    </QueryClientProvider>
  )
}

function sendAndDone(content: string) {
  chatMock.mockImplementation((_params: unknown, handlers: Record<string, (v: unknown) => void>) => {
    handlers.onDone({ content })
    return { abort: vi.fn(), promise: Promise.resolve() }
  })
  renderFull()
  const input = screen.getByLabelText('消息输入框')
  fireEvent.change(input, { target: { value: '在吗' } })
  fireEvent.click(screen.getByLabelText('发送消息'))
}

describe('AgentChatPanel 回复朗读（speechSynthesis）', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    global.fetch = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('speechSynthesis', { speak: speakMock, cancel: cancelMock, getVoices: () => [] })
    vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('开关开启时 onDone 自动朗读（去 Markdown）', async () => {
    localStorage.setItem(TTS_KEY, '1')
    sendAndDone('# 标题\n**你好**，这是[链接](http://x)回复')
    await waitFor(() => expect(speakMock).toHaveBeenCalledTimes(1))
    const u = speakMock.mock.calls[0][0] as FakeUtterance
    expect(u.text).toBe('标题，你好，这是链接回复')
    expect(u.lang).toBe('zh-CN')
  })

  it('开关关闭（默认）时不朗读', async () => {
    sendAndDone('你好，世界')
    await waitFor(() => expect(screen.getByText('你好，世界')).toBeTruthy())
    expect(speakMock).not.toHaveBeenCalled()
  })
})
