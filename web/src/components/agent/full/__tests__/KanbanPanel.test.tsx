import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import React from 'react'

const listMock = vi.fn()
const createMock = vi.fn()
const updateMock = vi.fn()
const removeMock = vi.fn()

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    agentKanbanApi: {
      list: (...args: unknown[]) => listMock(...args),
      create: (...args: unknown[]) => createMock(...args),
      update: (...args: unknown[]) => updateMock(...args),
      remove: (...args: unknown[]) => removeMock(...args),
    },
  }
})

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { KanbanPanel } from '../KanbanPanel'

const CARDS = [
  {
    id: 'k1',
    title: '复盘周报',
    description: '汇总本周盈亏与问题',
    column: 'todo',
    assignee: '',
    created_by: 'user',
    comment: '',
    created_at: 0,
    updated_at: 0,
  },
  {
    id: 'k2',
    title: '网格参数调优',
    description: '回测不同步长与区间',
    column: 'doing',
    assignee: '',
    created_by: 'agent',
    comment: '优先处理',
    created_at: 0,
    updated_at: 0,
  },
]

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <KanbanPanel bare onClose={() => {}} />
    </QueryClientProvider>
  )
}

describe('KanbanPanel 看板面板', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listMock.mockResolvedValue({ success: true, cards: CARDS })
    createMock.mockResolvedValue({ success: true, id: 'k3' })
    updateMock.mockResolvedValue({ success: true })
    removeMock.mockResolvedValue({ success: true })
  })

  it('渲染三列与卡片（徽标/评论/列计数）', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('复盘周报')).toBeTruthy())
    expect(screen.getByText('待办')).toBeTruthy()
    expect(screen.getByText('进行中')).toBeTruthy()
    expect(screen.getByText('已完成')).toBeTruthy()
    expect(screen.getByText('网格参数调优')).toBeTruthy()
    // 来源徽标与评论
    expect(screen.getByText('手动')).toBeTruthy()
    expect(screen.getByText('助手')).toBeTruthy()
    expect(screen.getByText('优先处理')).toBeTruthy()
    // 列计数
    expect(screen.getByLabelText('看板列 待办').textContent).toContain('1')
    // 空列提示
    expect(screen.getByText('还没有已完成的卡片')).toBeTruthy()
  })

  it('待办列顶部输入框 Enter 创建卡片', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('复盘周报')).toBeTruthy())
    const input = screen.getByLabelText('新建看板卡片')
    fireEvent.change(input, { target: { value: '巡检机器人' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(createMock).toHaveBeenCalledWith({ title: '巡检机器人' }))
  })

  it('前移/后移调用 update 切换列', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('复盘周报')).toBeTruthy())
    // 待办 → 进行中
    fireEvent.click(screen.getByLabelText('后移 复盘周报'))
    await waitFor(() => expect(updateMock).toHaveBeenCalledWith('k1', { column: 'doing' }))
    // 进行中 → 待办
    fireEvent.click(screen.getByLabelText('前移 网格参数调优'))
    await waitFor(() => expect(updateMock).toHaveBeenCalledWith('k2', { column: 'todo' }))
  })

  it('删除需二次确认', async () => {
    renderPanel()
    await waitFor(() => expect(screen.getByText('复盘周报')).toBeTruthy())
    fireEvent.click(screen.getByLabelText('删除 复盘周报'))
    expect(removeMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByLabelText('确认删除 复盘周报'))
    await waitFor(() => expect(removeMock).toHaveBeenCalledWith('k1'))
  })

  it('加载中显示骨架行', async () => {
    listMock.mockReturnValue(new Promise(() => {}))
    renderPanel()
    await waitFor(() => expect(screen.getAllByLabelText('加载中').length).toBeGreaterThanOrEqual(3))
  })
})
