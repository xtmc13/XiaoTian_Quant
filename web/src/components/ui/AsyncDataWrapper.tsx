import React from 'react'

interface AsyncDataWrapperProps<T> {
  isLoading: boolean
  data: T | undefined | null
  skeleton: React.ReactNode
  empty: React.ReactNode
  children: (data: T) => React.ReactNode
}

export function AsyncDataWrapper<T>({
  isLoading,
  data,
  skeleton,
  empty,
  children,
}: AsyncDataWrapperProps<T>) {
  if (isLoading) {
    return <>{skeleton}</>
  }

  const isEmpty =
    data === undefined ||
    data === null ||
    (Array.isArray(data) && data.length === 0)

  if (isEmpty) {
    return <>{empty}</>
  }

  return <>{children(data as T)}</>
}

export default AsyncDataWrapper
