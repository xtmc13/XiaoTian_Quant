import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const statusMock = vi.fn()
const pairCodeMock = vi.fn()
const unlinkMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentWecomApi: {
      status: (...args: unknown[]) => statusMock(...args),
      pairCode: (...args: unknown[]) => pairCodeMock(...args),
      unlink: (...args: unknown[]) => unlinkMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { WecomPanel } from '../WecomPanel'

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <WecomPanel onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('WecomPanel 企业微信接入面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('未配置：提示环境变量，状态卡显示未配置/未绑定', async () => {
    statusMock.mockResolvedValue({ success: true, configured: false, linked: false, staff_id: '' })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/WECOM_CORP_ID/)).toBeTruthy())
    expect(screen.getByLabelText('企业微信 配置状态').textContent).toBe('未配置')
    expect(screen.getByLabelText('企业微信 绑定状态').textContent).toBe('未绑定')
  })

  it('已配置未绑定：企业微信应用消息指引 + 生成配对码', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: false, staff_id: '' })
    pairCodeMock.mockResolvedValue({ success: true, code: '112233', expires_in: 600 })
    renderPanel()

    await waitFor(() => expect(screen.getByText(/在企业微信应用消息中把配对码发送给应用/)).toBeTruthy())
    expect(screen.getByRole('button', { name: /生成配对码/ })).toBeTruthy()
  })

  it('已绑定：显示 staff_id', async () => {
    statusMock.mockResolvedValue({ success: true, configured: true, linked: true, staff_id: 'zhangsan' })
    renderPanel()
    await waitFor(() => expect(screen.getByText(/zhangsan/)).toBeTruthy())
    expect(screen.getByLabelText('企业微信 绑定状态').textContent).toBe('已绑定')
  })
})
