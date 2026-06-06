import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useToastStore } from '../toastStore'

describe('toastStore', () => {
  beforeEach(() => {
    // Reset store state
    const { result } = renderHook(() => useToastStore())
    act(() => {
      result.current.clearAll()
    })
  })

  it('should add a toast', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Test message', type: 'info' })
    })

    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0].message).toBe('Test message')
    expect(result.current.toasts[0].type).toBe('info')
  })

  it('should auto-generate id', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Test', type: 'success' })
    })

    expect(result.current.toasts[0].id).toBeDefined()
    expect(typeof result.current.toasts[0].id).toBe('string')
  })

  it('should remove a toast by id', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Test 1', type: 'info' })
      result.current.addToast({ message: 'Test 2', type: 'error' })
    })

    const idToRemove = result.current.toasts[0].id

    act(() => {
      result.current.removeToast(idToRemove)
    })

    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0].message).toBe('Test 2')
  })

  it('should support different toast types', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Info', type: 'info' })
      result.current.addToast({ message: 'Success', type: 'success' })
      result.current.addToast({ message: 'Warning', type: 'warning' })
      result.current.addToast({ message: 'Error', type: 'error' })
    })

    expect(result.current.toasts).toHaveLength(4)
    const types = result.current.toasts.map((t: any) => t.type)
    expect(types).toContain('info')
    expect(types).toContain('success')
    expect(types).toContain('warning')
    expect(types).toContain('error')
  })

  it('should clear all toasts', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Test 1', type: 'info' })
      result.current.addToast({ message: 'Test 2', type: 'error' })
      result.current.addToast({ message: 'Test 3', type: 'success' })
    })

    expect(result.current.toasts).toHaveLength(3)

    act(() => {
      result.current.clearAll()
    })

    expect(result.current.toasts).toHaveLength(0)
  })

  it('should not fail when removing non-existent toast', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Test', type: 'info' })
    })

    act(() => {
      result.current.removeToast('non-existent-id')
    })

    expect(result.current.toasts).toHaveLength(1)
  })

  it('should support custom duration', () => {
    const { result } = renderHook(() => useToastStore())

    act(() => {
      result.current.addToast({ message: 'Quick', type: 'info', duration: 1000 })
      result.current.addToast({ message: 'Long', type: 'info', duration: 10000 })
    })

    expect(result.current.toasts[0].duration).toBe(1000)
    expect(result.current.toasts[1].duration).toBe(10000)
  })
})
