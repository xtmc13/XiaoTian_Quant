import React, { ErrorInfo, ReactNode } from 'react'

interface ErrorBoundaryProps {
  fallback: ReactNode
  children: ReactNode
}

interface ErrorBoundaryState {
  hasError: boolean
  error?: Error
}

class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('ErrorBoundary caught error:', error, errorInfo)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div>
          {this.props.fallback}
          <details className="mx-auto mt-4 max-w-lg rounded-lg border border-red-500/40 bg-red-500/5 p-3 text-left">
            <summary className="cursor-pointer text-xs text-red-400">错误详情（截图反馈给开发）</summary>
            <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all text-[11px] leading-relaxed text-red-300">
              {String(this.state.error?.message)}
              {'\n'}
              {this.state.error?.stack?.slice(0, 900)}
            </pre>
          </details>
        </div>
      )
    }
    return this.props.children
  }
}

export { ErrorBoundary }