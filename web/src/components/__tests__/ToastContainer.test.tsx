import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { ToastContainer } from '../ToastContainer'
import { useToastStore } from '@/stores/toastStore'

describe('ToastContainer', () => {
  beforeEach(() => {
    // Clear all toasts before each test
    useToastStore.getState().clearAll()
  })

  it('renders nothing when no toasts', () => {
    const { container } = render(<ToastContainer />)
    expect(container.querySelector('.fixed')).toBeFalsy()
  })

  it('renders a toast when added', () => {
    render(<ToastContainer />)
    act(() => {
      useToastStore.getState().addToast({ type: 'success', message: '操作成功', duration: 5000 })
    })
    expect(screen.getByText('操作成功')).toBeTruthy()
  })

  it('renders multiple toasts', () => {
    render(<ToastContainer />)
    act(() => {
      useToastStore.getState().addToast({ type: 'success', message: '第一条', duration: 5000 })
      useToastStore.getState().addToast({ type: 'error', message: '第二条', duration: 5000 })
    })
    expect(screen.getByText('第一条')).toBeTruthy()
    expect(screen.getByText('第二条')).toBeTruthy()
  })

  it('removes toast when close button clicked', () => {
    render(<ToastContainer />)
    act(() => {
      useToastStore.getState().addToast({ type: 'info', message: '可关闭', duration: 5000 })
    })
    const closeBtn = screen.getByRole('button')
    act(() => {
      fireEvent.click(closeBtn)
    })
    expect(screen.queryByText('可关闭')).toBeFalsy()
  })

  it('renders different toast types with correct styling', () => {
    render(<ToastContainer />)
    act(() => {
      useToastStore.getState().addToast({ type: 'success', message: '成功', duration: 5000 })
      useToastStore.getState().addToast({ type: 'error', message: '错误', duration: 5000 })
      useToastStore.getState().addToast({ type: 'warning', message: '警告', duration: 5000 })
      useToastStore.getState().addToast({ type: 'info', message: '信息', duration: 5000 })
    })
    expect(screen.getByText('成功')).toBeTruthy()
    expect(screen.getByText('错误')).toBeTruthy()
    expect(screen.getByText('警告')).toBeTruthy()
    expect(screen.getByText('信息')).toBeTruthy()
  })

  it('auto-removes toast after duration', async () => {
    vi.useFakeTimers()
    render(<ToastContainer />)
    act(() => {
      useToastStore.getState().addToast({ type: 'success', message: '临时消息', duration: 1000 })
    })
    expect(screen.getByText('临时消息')).toBeTruthy()

    act(() => {
      vi.advanceTimersByTime(1100)
    })
    expect(screen.queryByText('临时消息')).toBeFalsy()
    vi.useRealTimers()
  })
})
