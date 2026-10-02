import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent } from '@testing-library/react'
import React, { useState } from 'react'

vi.mock('@/lib/useToast', () => ({ toast: vi.fn() }))

import { AgentComposer } from '../Composer'

// ── 语音识别 mock（实现 start/stop/onresult/onend 协议） ──
interface FakeResult {
  isFinal: boolean
  0: { transcript: string }
}

class FakeRecognition {
  lang = ''
  continuous = true
  interimResults = false
  onresult: ((e: { resultIndex: number; results: FakeResult[] }) => void) | null = null
  onerror: (() => void) | null = null
  onend: (() => void) | null = null
  start = vi.fn()
  abort = vi.fn()
  stop = vi.fn(() => {
    this.onend?.()
  })
  static instances: FakeRecognition[] = []
  constructor() {
    FakeRecognition.instances.push(this)
  }
  emit(results: FakeResult[]) {
    this.onresult?.({ resultIndex: 0, results })
  }
}

function Harness() {
  const [input, setInput] = useState('')
  return (
    <AgentComposer
      input={input}
      onInputChange={setInput}
      onSubmit={() => {}}
      onStop={() => {}}
      isStreaming={false}
      attachments={[]}
      onRemoveAttachment={() => {}}
      onPickFile={() => {}}
      textareaRef={{ current: null }}
      settings={{ system_prompt: '', temperature: 0.7, model: '' }}
      updateSettings={() => {}}
      providers={[]}
      onSlash={() => {}}
      onSuggestion={() => {}}
      showSuggestions={false}
      onUseSkill={() => {}}
    />
  )
}

describe('Composer 语音输入', () => {
  beforeEach(() => {
    FakeRecognition.instances = []
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('支持时渲染麦克风按钮，interim 进输入框、final 落锤', () => {
    vi.stubGlobal('webkitSpeechRecognition', FakeRecognition)
    render(<Harness />)

    const mic = screen.getByLabelText('语音输入')
    fireEvent.click(mic)
    const rec = FakeRecognition.instances[0]
    expect(rec.start).toHaveBeenCalledTimes(1)
    expect(rec.lang).toBe('zh-CN')
    expect(rec.continuous).toBe(false)
    expect(rec.interimResults).toBe(true)
    // 录音中：按钮切换为停止态
    expect(screen.getByLabelText('停止语音输入')).toBeTruthy()

    // interim 结果显示在输入框
    act(() => rec.emit([{ isFinal: false, 0: { transcript: '分析一下' } }]))
    expect((screen.getByLabelText('消息输入框') as HTMLTextAreaElement).value).toBe('分析一下')

    // interim 被修正（同一段的新 interim 覆盖旧的）
    act(() => rec.emit([{ isFinal: false, 0: { transcript: '分析一下行情' } }]))
    expect((screen.getByLabelText('消息输入框') as HTMLTextAreaElement).value).toBe('分析一下行情')

    // final 落锤
    act(() => rec.emit([{ isFinal: true, 0: { transcript: '分析一下行情' } }]))
    expect((screen.getByLabelText('消息输入框') as HTMLTextAreaElement).value).toBe('分析一下行情')

    // 再次点击停止 → stop 触发 onend，回到待机
    fireEvent.click(screen.getByLabelText('停止语音输入'))
    expect(rec.stop).toHaveBeenCalledTimes(1)
    expect(screen.getByLabelText('语音输入')).toBeTruthy()
  })

  it('保留录音前已有文本并追加识别结果', () => {
    vi.stubGlobal('webkitSpeechRecognition', FakeRecognition)
    render(<Harness />)
    fireEvent.change(screen.getByLabelText('消息输入框'), { target: { value: '请先' } })
    fireEvent.click(screen.getByLabelText('语音输入'))
    const rec = FakeRecognition.instances[0]
    act(() => rec.emit([{ isFinal: true, 0: { transcript: '复盘' } }]))
    expect((screen.getByLabelText('消息输入框') as HTMLTextAreaElement).value).toBe('请先复盘')
  })

  it('浏览器不支持时不渲染麦克风按钮', () => {
    render(<Harness />)
    expect(screen.queryByLabelText('语音输入')).toBeNull()
    expect(screen.queryByLabelText('停止语音输入')).toBeNull()
  })
})
