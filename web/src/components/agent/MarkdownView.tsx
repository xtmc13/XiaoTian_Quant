import React, { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Check, Copy } from 'lucide-react'
import { copyText } from './types'

// ── 从 pre 的子 code 元素提取语言与文本 ──
function extractCode(children: React.ReactNode): { lang: string; text: string } {
  let lang = ''
  let text = ''
  React.Children.forEach(children, (child) => {
    if (React.isValidElement(child)) {
      const props = child.props as { className?: string; children?: React.ReactNode }
      const match = /language-([\w+-]+)/.exec(props.className || '')
      if (match) lang = match[1]
      text += extractText(props.children)
    }
  })
  return { lang, text: text.replace(/\n$/, '') }
}

function extractText(node: React.ReactNode): string {
  if (node === null || node === undefined) return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(extractText).join('')
  if (React.isValidElement(node)) {
    return extractText((node.props as { children?: React.ReactNode }).children)
  }
  return ''
}

// ── 代码块：语言标签 + 复制按钮 ──
function CodeBlock({ children }: { children?: React.ReactNode }) {
  const [copied, setCopied] = useState(false)
  const { lang, text } = extractCode(children)

  const onCopy = async () => {
    if (await copyText(text)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }

  return (
    <div className="group/code my-2 overflow-hidden rounded-lg border border-quant-border bg-black/30 dark:bg-black/40">
      <div className="flex items-center gap-2 border-b border-quant-border/60 px-2.5 py-1">
        <span className="text-[10px] uppercase tracking-wide text-[#888]">{lang || 'text'}</span>
        <button
          type="button"
          onClick={onCopy}
          title={copied ? '已复制' : '复制代码'}
          aria-label={copied ? '已复制' : '复制代码'}
          className="ml-auto flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] text-[#888] transition-colors hover:bg-quant-hover hover:text-foreground"
        >
          {copied ? <Check size={11} className="text-[#52c41a]" /> : <Copy size={11} />}
          {copied ? '已复制' : '复制'}
        </button>
      </div>
      <pre className="overflow-x-auto p-2.5 text-[11px] leading-relaxed text-[#7dd3fc]">
        <code className={lang ? `language-${lang}` : undefined}>{text}</code>
      </pre>
    </div>
  )
}

// ── 完整 markdown 渲染（GFM：标题/列表/表格/加粗/行内代码/链接） ──
export function MarkdownView({ content }: { content: string }) {
  return (
    <div className="break-words text-[13px] leading-relaxed">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
          code: ({ children }) => (
            <code className="rounded bg-black/20 px-1 py-0.5 text-[11px] text-[#7dd3fc] dark:bg-white/10">
              {children}
            </code>
          ),
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noreferrer" className="text-[#1890ff] underline hover:text-[#40a9ff]">
              {children}
            </a>
          ),
          h1: ({ children }) => <h1 className="mb-1 mt-2 text-base font-semibold">{children}</h1>,
          h2: ({ children }) => <h2 className="mb-1 mt-2 text-[15px] font-semibold">{children}</h2>,
          h3: ({ children }) => <h3 className="mb-1 mt-1.5 text-sm font-semibold">{children}</h3>,
          ul: ({ children }) => <ul className="my-1 list-disc pl-4">{children}</ul>,
          ol: ({ children }) => <ol className="my-1 list-decimal pl-4">{children}</ol>,
          li: ({ children }) => <li className="my-0.5">{children}</li>,
          p: ({ children }) => <p className="my-1 first:mt-0 last:mb-0">{children}</p>,
          table: ({ children }) => (
            <div className="my-2 overflow-x-auto">
              <table className="w-full border-collapse text-[12px]">{children}</table>
            </div>
          ),
          thead: ({ children }) => <thead className="bg-black/10 dark:bg-white/5">{children}</thead>,
          th: ({ children }) => (
            <th className="border border-quant-border px-2 py-1 text-left font-semibold">{children}</th>
          ),
          td: ({ children }) => <td className="border border-quant-border px-2 py-1">{children}</td>,
          hr: () => <hr className="my-2 border-quant-border" />,
          blockquote: ({ children }) => (
            <blockquote className="my-1 border-l-2 border-quant-border pl-2 text-muted-foreground">
              {children}
            </blockquote>
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}

export default MarkdownView
