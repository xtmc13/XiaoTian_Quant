import { describe, it, expect, vi } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { useAsyncData, usePolling, useFormState, useDebounce } from '../useAsyncData'

describe('useAsyncData', () => {
  it('returns loading initially then data', async () => {
    const fetcher = vi.fn().mockResolvedValue('hello')
    const { result } = renderHook(() => useAsyncData(fetcher, { enabled: true }))

    expect(result.current.loading).toBe(true)
    expect(result.current.data).toBeNull()

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toBe('hello')
    expect(result.current.error).toBeNull()
  })

  it('returns error when fetcher fails', async () => {
    const fetcher = vi.fn().mockImplementation(() => Promise.reject(new Error('network error')))
    const onError = vi.fn()
    const { result } = renderHook(() => useAsyncData(fetcher, { enabled: true, onError }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('network error')
    expect(result.current.data).toBeNull()
    expect(onError).toHaveBeenCalledWith('network error')
  })

  it('does not fetch when enabled is false', () => {
    const fetcher = vi.fn().mockResolvedValue('data')
    const { result } = renderHook(() => useAsyncData(fetcher, { enabled: false }))

    expect(result.current.loading).toBe(false)
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('execute triggers manual fetch', async () => {
    const fetcher = vi.fn().mockResolvedValue('manual')
    const { result } = renderHook(() => useAsyncData(fetcher, { enabled: false }))

    act(() => {
      result.current.execute().catch(() => {}) // swallow potential errors
    })

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toBe('manual')
  })

  it('setData updates state directly', async () => {
    const fetcher = vi.fn()
    const { result } = renderHook(() => useAsyncData(fetcher, { enabled: false }))

    act(() => {
      result.current.setData('direct')
    })

    await waitFor(() => expect(result.current.data).toBe('direct'))
  })
})

describe('useDebounce', () => {
  it('delays value update', async () => {
    const { result, rerender } = renderHook(({ value }) => useDebounce(value, 100), {
      initialProps: { value: 'a' },
    })

    expect(result.current).toBe('a')

    rerender({ value: 'b' })
    expect(result.current).toBe('a') // still old value

    await waitFor(() => expect(result.current).toBe('b'), { timeout: 200 })
  })
})

describe('useFormState', () => {
  it('tracks dirty fields', async () => {
    const { result } = renderHook(() => useFormState({ name: '', email: '' }))

    act(() => {
      result.current.setField('name', 'Alice')
    })

    await waitFor(() => expect(result.current.values.name).toBe('Alice'))
    expect(result.current.isDirty).toBe(true)
    expect(result.current.dirty.has('name')).toBe(true)
  })

  it('clears errors on field change', async () => {
    const { result } = renderHook(() => useFormState({ name: '' }))

    act(() => {
      result.current.setFieldError('name', 'Required')
    })
    await waitFor(() => expect(result.current.errors.name).toBe('Required'))

    act(() => {
      result.current.setField('name', 'Alice')
    })
    await waitFor(() => expect(result.current.errors.name).toBeUndefined())
  })

  it('resets to initial values', async () => {
    const { result } = renderHook(() => useFormState({ name: 'Bob' }))

    act(() => {
      result.current.setField('name', 'Alice')
    })
    await waitFor(() => expect(result.current.values.name).toBe('Alice'))

    act(() => {
      result.current.reset()
    })

    await waitFor(() => expect(result.current.values.name).toBe('Bob'))
    expect(result.current.isDirty).toBe(false)
    expect(result.current.errors).toEqual({})
  })
})
