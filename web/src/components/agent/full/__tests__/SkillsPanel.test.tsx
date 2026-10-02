import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const listMock = vi.fn()
const createMock = vi.fn()
const removeMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentSkillApi: {
      list: (...args: unknown[]) => listMock(...args),
      create: (...args: unknown[]) => createMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { SkillsPanel } from '../SkillsPanel'

const SKILL = {
  id: 'sk_1',
  user_id: 2,
  name: '每日复盘',
  description: '收盘复盘流程',
  body: '1. 看持仓 2. 看信号 3. 写总结',
  usage_count: 3,
  source: 'agent',
  created_at: 0,
  updated_at: 0,
}

function renderPanel(onUse = () => {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <SkillsPanel onUse={onUse} onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('SkillsPanel 技能面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ skills: [SKILL] })
    createMock.mockResolvedValue({ skill: SKILL })
    removeMock.mockResolvedValue({ deleted: true })
  })

  it('列出技能：/名、说明、使用次数、对话沉淀标记', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('/每日复盘')).toBeTruthy())
    expect(screen.getByText('收盘复盘流程')).toBeTruthy()
    expect(screen.getByText('×3')).toBeTruthy()
    expect(screen.getByText('对话沉淀')).toBeTruthy()
  })

  it('「使用」回调带出技能正文', async () => {
    const onUse = vi.fn()
    renderPanel(onUse)
    await waitFor(() => expect(screen.getByText('/每日复盘')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('使用技能 每日复盘'))
    expect(onUse).toHaveBeenCalledWith({ name: '每日复盘', body: SKILL.body })
  })

  it('新建技能提交 name/description/body', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('/每日复盘')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('新建技能'))
    fireEvent.change(screen.getByLabelText('技能名'), { target: { value: '机器人巡检' } })
    fireEvent.change(screen.getByLabelText('技能说明'), { target: { value: '巡检流程' } })
    fireEvent.change(screen.getByLabelText('技能正文'), { target: { value: '检查所有机器人' } })
    fireEvent.click(screen.getByText('保存'))

    await waitFor(() => expect(createMock).toHaveBeenCalledTimes(1))
    const [body] = createMock.mock.calls[0] as [Record<string, unknown>]
    expect(body).toEqual({ name: '机器人巡检', description: '巡检流程', body: '检查所有机器人' })
  })

  it('删除技能需二次确认', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('/每日复盘')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('删除技能 每日复盘'))
    fireEvent.click(screen.getByLabelText('确认删除技能 每日复盘'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith('sk_1'))
  })
})
