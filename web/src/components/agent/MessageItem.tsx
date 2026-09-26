import React, { useState } from 'react'
import { Check, ChevronRight, CircleAlert, Clipboard, Loader2, Pencil, RotateCcw, Wrench } from 'lucide-react'
import type { AgentChatMsg } from './types'
import { MarkdownView } from './MarkdownView'
import { copyText, toolLabel } from './types'
import { cn } from '@/lib/utils'

// ── 思考过程（reasoning）：默认折叠，灰字斜体 ──
function ReasoningBlock({ reasoning }: { reasoning: string }) {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="mb-1.5">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        className="flex items-center gap-1 rounded px-1 py-0.5 text-[11px] italic text-[#888] transition-colors hover:bg-quant-hover hover:text-[#aaa]"
      >
        <ChevronRight size={12} className={cn('shrink-0 transition-transform duration-150', expanded && 'rotate-90')} />
        思考过程
      </button>
      {expanded && (
        <div className="mt-1 whitespace-pre-wrap break-words rounded-md bg-black/10 px-2 py-1.5 text-[11px] italic leading-relaxed text-[#888] dark:bg-white/5">
          {reasoning}
        </div>
      )}
    </div>
  )
}

// ── 工具调用卡片 ──
export function ToolCallList({ toolCalls }: { toolCalls: NonNullable<AgentChatMsg['toolCalls']> }) {
  if (toolCalls.length === 0) return null
  return (
    <div className="mb-1.5 space-y-1">
      {toolCalls.map((t, ti) => (
        <div
          key={`${t.name}-${ti}`}
          className="flex items-center gap-1.5 rounded-md bg-black/10 px-2 py-1 text-[11px] text-[#999] dark:bg-white/5"
        >
          {t.status === 'running' ? (
            <Loader2 size={12} className="shrink-0 animate-spin text-[#1890ff]" />
          ) : (
            <Check size={12} className="shrink-0 text-[#52c41a]" />
          )}
          <Wrench size={11} className="shrink-0 opacity-60" />
          <span className="shrink-0 text-foreground/80">{toolLabel(t.name)}</span>
          {t.args_summary && <span className="truncate opacity-70">{t.args_summary}</span>}
          {t.status === 'running' ? (
            <span className="ml-auto shrink-0 animate-pulse">执行中…</span>
          ) : (
            <span className="ml-auto shrink-0 text-[#52c41a]">{t.result_summary || '完成'}</span>
          )}
        </div>
      ))}
    </div>
  )
}

export interface MessageItemProps {
  msg: AgentChatMsg
  index: number
  isStreaming: boolean
  onRegenerate: () => void
  onEdit: (index: number, content: string) => void
}

// ── 单条消息：用户右侧气泡 / 助手左侧全宽，hover 操作条 ──
export function MessageItem({ msg, index, isStreaming, onRegenerate, onEdit }: MessageItemProps) {
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState(msg.content)

  if (msg.role === 'user') {
    return (
      <div className="group flex justify-end">
        <div className="max-w-[85%]">
          {editing ? (
            <div className="flex flex-col gap-1.5">
              <textarea
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                rows={3}
                aria-label="编辑消息"
                className="w-56 resize-none rounded-xl border border-[#1890ff]/60 bg-quant-bg-secondary px-3 py-2 text-[13px] text-foreground focus:outline-none focus:ring-1 focus:ring-[#1890ff]/30 md:w-64"
              />
              <div className="flex justify-end gap-1.5">
                <button
                  type="button"
                  onClick={() => {
                    setEditing(false)
                    setEditValue(msg.content)
                  }}
                  className="rounded-md border border-quant-border px-2 py-1 text-[11px] text-[#aaa] hover:bg-quant-hover"
                >
                  取消
                </button>
                <button
                  type="button"
                  disabled={!editValue.trim() || isStreaming}
                  onClick={() => {
                    setEditing(false)
                    onEdit(index, editValue.trim())
                  }}
                  className="rounded-md bg-[#1890ff] px-2 py-1 text-[11px] text-white hover:bg-[#40a9ff] disabled:opacity-40"
                >
                  发送
                </button>
              </div>
            </div>
          ) : (
            <>
              <div className="whitespace-pre-wrap break-words rounded-2xl rounded-br-sm bg-[#1890ff] px-3 py-2 text-[13px] leading-relaxed text-white">
                {msg.content}
              </div>
              {/* hover 操作条：编辑 */}
              <div className="mt-0.5 flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                <button
                  type="button"
                  title="编辑并重新发送"
                  aria-label="编辑并重新发送"
                  disabled={isStreaming}
                  onClick={() => {
                    setEditValue(msg.content)
                    setEditing(true)
                  }}
                  className="rounded p-1 text-[#888] hover:bg-quant-hover hover:text-foreground disabled:opacity-40"
                >
                  <Pencil size={12} />
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    )
  }

  // 助手消息
  const doCopy = async () => {
    if (await copyText(msg.content)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
  }

  return (
    <div className="group flex justify-start">
      <div className="w-full min-w-0 rounded-2xl rounded-bl-sm border border-quant-border bg-quant-bg-secondary px-3 py-2">
        {msg.reasoning && <ReasoningBlock reasoning={msg.reasoning} />}
        <ToolCallList toolCalls={msg.toolCalls || []} />
        {msg.content ? (
          <MarkdownView content={msg.content} />
        ) : (
          msg.streaming && <Loader2 size={14} className="animate-spin text-[#1890ff]" />
        )}
        {msg.error && (
          <div className="mt-1 flex items-center gap-1 text-[11px] text-[#f5222d]">
            <CircleAlert size={12} />
            连接中断，请重试
          </div>
        )}
        {/* hover 操作条：复制 / 重新生成 */}
        {(msg.content || msg.toolCalls?.length) && !msg.streaming && (
          <div className="mt-1 flex gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
            <button
              type="button"
              title={copied ? '已复制' : '复制'}
              aria-label={copied ? '已复制' : '复制回复'}
              onClick={doCopy}
              className="rounded p-1 text-[#888] hover:bg-quant-hover hover:text-foreground"
            >
              {copied ? <Check size={12} className="text-[#52c41a]" /> : <Clipboard size={12} />}
            </button>
            <button
              type="button"
              title="重新生成"
              aria-label="重新生成回复"
              disabled={isStreaming}
              onClick={onRegenerate}
              className="rounded p-1 text-[#888] hover:bg-quant-hover hover:text-foreground disabled:opacity-40"
            >
              <RotateCcw size={12} />
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

export default MessageItem
