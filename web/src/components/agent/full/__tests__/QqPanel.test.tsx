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
    agentQqApi: {
      status: (...args: unknown[]) => statusMock(...args),
      pairCode: (...args: unknown[]) => pairCodeMock(...args),
      unlink: (...args: unknown[]) => unlinkMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { QqPanel } from '../QqPanel'

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <QqPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('QqPanel QQ 接入面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('未配置：展示扫码登录区块（替代 env 提示），状态卡显示未配置/未绑定', async () => {
    statusMock.mockResolvedValue({ success: true, configured: false, linked: false, open_id: '' })
    renderPanel()
    // 有 connectorQr 时未配置态展示扫码登录区块，不再提示手动配置环境变量
    await waitFor(() => expect(screen.getByText(/还没配置 QQ 机器人/)).toBeTruthy())
    expect(screen.getByRole('button', { name: /QQ 扫码登录/ })).toBeTruthy()
    expect(screen.queryByText(/QQ_APP_ID/)).toBeNull()
    expect(screen.getByLabelText('QQ 配置状态').textContent).toBe('未配置')
    expect(screen.getByLabelText('QQ 绑定状态').textContent).toBe('未绑定')
    expect(screen.queryByRole('button', { name: /生成配对码/ })).toBeNull()
  })

  it('已配置未绑定：QQ 私聊指引 + 生成配对码并展示倒计时', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: false, open_id: '' })
    pairCodeMock.mockResolvedValue({ success: true, code: '888999', expires_in: 600 })
    renderPanel()

    await waitFor(() => expect(screen.getByText(/在 QQ 私聊中把配对码发送给/)).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /生成配对码/ }))

    await waitFor(() => expect(screen.getByText('888999')).toBeTruthy())
    expect(screen.getByLabelText('配对码倒计时').textContent).toMatch(/^10:0/)
    expect(screen.getByLabelText('复制配对码')).toBeTruthy()
  })

  it('已绑定：显示 open_id，两步确认后解绑', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: true, open_id: 'qq_open_123' })
    unlinkMock.mockResolvedValue({ success: true })
    renderPanel()

    await waitFor(() => expect(screen.getByText(/qq_open_123/)).toBeTruthy())
    expect(screen.getByLabelText('QQ 绑定状态').textContent).toBe('已绑定')

    fireEvent.click(screen.getByLabelText('解绑 QQ'))
    expect(unlinkMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByLabelText('确认解绑 QQ'))
    await waitFor(() => expect(unlinkMock).toHaveBeenCalledTimes(1))
  })
})
