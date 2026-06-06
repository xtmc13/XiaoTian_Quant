import { useState, useEffect, useRef, useCallback } from 'react'

interface AsyncState<T> {
  data: T | null
  loading: boolean
  error: string | null
}

interface UseAsyncDataOptions<T> {
  initialData?: T
  deps?: unknown[]
  enabled?: boolean
  onError?: (err: string) => void
}

/**
 * Generic async data fetcher with loading/error states.
 * Replaces repetitive useState + useEffect + try/catch patterns.
 */
export function useAsyncData<T>(
  fetcher: () => Promise<T>,
  options: UseAsyncDataOptions<T> = {}
) {
  const { initialData = null, deps = [], enabled = true, onError } = options
  const [state, setState] = useState<AsyncState<T>>({
    data: initialData,
    loading: enabled,
    error: null,
  })

  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher

  const execute = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }))
    try {
      const data = await fetcherRef.current()
      setState({ data, loading: false, error: null })
      return data
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : (err as Record<string, string>)?.error || '加载失败'
      setState((s) => ({ ...s, loading: false, error: msg }))
      onError?.(msg)
      throw err
    }
  }, [onError])

  const executeRef = useRef(execute)
  executeRef.current = execute

  useEffect(() => {
    if (!enabled) {
      setState((s) => ({ ...s, loading: false }))
      return
    }
    executeRef.current().catch(() => {}) // swallow; error is captured in state
  }, [enabled, ...deps])

  return {
    ...state,
    execute,
    setData: (data: T | null) => setState((s) => ({ ...s, data })),
    setError: (error: string | null) => setState((s) => ({ ...s, error })),
    setLoading: (loading: boolean) => setState((s) => ({ ...s, loading })),
  }
}

/**
 * Polling hook for real-time data (e.g. portfolio, positions).
 */
export function usePolling<T>(
  fetcher: () => Promise<T>,
  intervalMs: number,
  options: { enabled?: boolean; onError?: (err: string) => void } = {}
) {
  const { enabled = true, onError } = options
  const { data, loading, error, execute } = useAsyncData(fetcher, {
    enabled,
    onError,
  })

  useEffect(() => {
    if (!enabled || intervalMs <= 0) return
    const id = setInterval(() => {
      execute().catch(() => {}) // swallow errors, onError handles them
    }, intervalMs)
    return () => clearInterval(id)
  }, [enabled, intervalMs, execute])

  return { data, loading, error, refresh: execute }
}

/**
 * Form state manager with dirty tracking.
 */
export function useFormState<T extends Record<string, unknown>>(
  initialValues: T
) {
  const [values, setValues] = useState<T>(initialValues)
  const [dirty, setDirty] = useState<Set<string>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})

  const setField = useCallback(
    (key: keyof T, value: unknown) => {
      setValues((v) => ({ ...v, [key]: value }))
      setDirty((d) => new Set(d).add(String(key)))
      // Clear error when field changes
      setErrors((e) => {
        if (e[String(key)]) {
          const next = { ...e }
          delete next[String(key)]
          return next
        }
        return e
      })
    },
    []
  )

  const setFields = useCallback((partial: Partial<T>) => {
    setValues((v) => ({ ...v, ...partial }))
    setDirty((d) => {
      const next = new Set(d)
      Object.keys(partial).forEach((k) => next.add(k))
      return next
    })
  }, [])

  const reset = useCallback(() => {
    setValues(initialValues)
    setDirty(new Set())
    setErrors({})
  }, [initialValues])

  const setFieldError = useCallback((key: keyof T, msg: string) => {
    setErrors((e) => ({ ...e, [String(key)]: msg }))
  }, [])

  const clearErrors = useCallback(() => setErrors({}), [])

  return {
    values,
    dirty,
    errors,
    isDirty: dirty.size > 0,
    setField,
    setFields,
    reset,
    setFieldError,
    clearErrors,
  }
}

/**
 * Debounced value hook.
 */
export function useDebounce<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(id)
  }, [value, delayMs])
  return debounced
}

/**
 * Previous value hook.
 */
export function usePrevious<T>(value: T): T | undefined {
  const ref = useRef<T | undefined>(undefined)
  useEffect(() => {
    ref.current = value
  }, [value])
  return ref.current
}
