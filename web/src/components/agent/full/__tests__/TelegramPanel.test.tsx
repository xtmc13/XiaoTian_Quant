import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const statusMock = vi.fn()
const pairCodeMock = vi.fn()
const unlinkMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentTelegramApi: {
      status: (...args: unknown[]) => statusMock(...args),
      pairCode: (...args: unknown[]) => pairCodeMock(...args),
      unlink: (...args: unknown[]) => unlinkMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { TelegramPanel } from '../TelegramPanel'

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TelegramPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('TelegramPanel 接入面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('未配置时提示设置环境变量', async () => {
    statusMock.mockResolvedValue({ configured: false, linked: false })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/TELEGRAM_BOT_TOKEN/)).toBeTruthy())
    expect(screen.queryByLabelText('解绑 Telegram')).toBeNull()
  })

  it('已配置未绑定：生成配对码并展示', async () => {
    statusMock.mockResolvedValue({ configured: true, linked: false })
    pairCodeMock.mockResolvedValue({ code: '123456', expires_in: 600 })
    renderPanel()

    await waitFor(() => expect(screen.getByRole('button', { name: /生成配对码/ })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /生成配对码/ }))

    await waitFor(() => expect(screen.getByText('123456')).toBeTruthy())
    expect(screen.getByLabelText('复制配对码')).toBeTruthy()
  })

  it('已绑定：显示用户名并支持解绑', async () => {
    statusMock.mockResolvedValue({ configured: true, linked: true, username: 'trader', chat_id: 55 })
    unlinkMock.mockResolvedValue({ unlinked: true })
    renderPanel()

    await waitFor(() => expect(screen.getByText(/@trader/)).toBeTruthy())
    fireEvent.click(screen.getByLabelText('解绑 Telegram'))
    await waitFor(() => expect(unlinkMock).toHaveBeenCalledTimes(1))
  })
})
