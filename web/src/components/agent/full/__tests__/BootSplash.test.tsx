import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import React from 'react'
import { BootSplash } from '../BootSplash'

describe('BootSplash 开屏动画', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('渲染 Logo、欢迎语与进度条', () => {
    render(<BootSplash onDone={() => {}} />)
    expect(screen.getByText('小天量化')).toBeTruthy()
    expect(screen.getByText('欢迎来到专业工作模式')).toBeTruthy()
    expect(screen.getByText(/initializing agent/)).toBeTruthy()
    expect(screen.getByText('点击任意处跳过')).toBeTruthy()
  })

  it('进度跑满后自动回调 onDone', () => {
    const onDone = vi.fn()
    render(<BootSplash onDone={onDone} />)
    act(() => {
      vi.advanceTimersByTime(3000)
    })
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('点击跳过立即触发淡出并回调', () => {
    const onDone = vi.fn()
    const { container } = render(<BootSplash onDone={onDone} />)
    fireEvent.click(container.firstChild as Element)
    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('自定义壁纸与版本号展示', () => {
    render(<BootSplash wallpaper="https://example.com/w.jpg" version="v9.9.9" onDone={() => {}} />)
    expect(screen.getByText('v9.9.9')).toBeTruthy()
    const img = document.querySelector('img[alt=""]')
    expect(img?.getAttribute('src')).toBe('https://example.com/w.jpg')
  })
})
