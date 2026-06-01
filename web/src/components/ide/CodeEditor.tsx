/**
 * CodeMirror 6 React wrapper — Python syntax highlighting with dark theme.
 */
import { useEffect, useRef } from 'react'
import { EditorView, keymap, placeholder as cmPlaceholder } from '@codemirror/view'
import { EditorState } from '@codemirror/state'
import { python } from '@codemirror/lang-python'
import { oneDark } from '@codemirror/theme-one-dark'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { cn } from '@/lib/utils'

interface CodeEditorProps {
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
  className?: string
  placeholder?: string
  theme?: 'dark' | 'light'
}

export function CodeEditor({
  value,
  onChange,
  readOnly = false,
  className,
  placeholder: placeholderText,
  theme = 'dark',
}: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const viewRef = useRef<EditorView | null>(null)

  // Init editor on mount
  useEffect(() => {
    if (!containerRef.current) return

    const extensions = [
      python(),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      EditorView.editable.of(!readOnly),
      EditorView.lineWrapping,
      EditorView.updateListener.of(update => {
        if (update.docChanged && onChange) {
          onChange(update.state.doc.toString())
        }
      }),
      ...(theme === 'dark' ? [oneDark] : []),
      ...(placeholderText ? [cmPlaceholder(placeholderText)] : []),
    ]

    const state = EditorState.create({ doc: value, extensions })
    const view = new EditorView({ state, parent: containerRef.current })
    viewRef.current = view

    return () => {
      view.destroy()
      viewRef.current = null
    }
    // Only init once — value sync handled via dispatch
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Sync value from props → editor
  useEffect(() => {
    if (!viewRef.current) return
    const currentDoc = viewRef.current.state.doc.toString()
    if (value !== currentDoc) {
      viewRef.current.dispatch({
        changes: { from: 0, to: currentDoc.length, insert: value },
      })
    }
  }, [value])

  return (
    <div ref={containerRef} className={cn('h-full overflow-auto', className)} />
  )
}
