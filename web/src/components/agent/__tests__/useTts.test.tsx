import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'

import { loadTtsEnabled, speakText, stripMarkdown, TTS_KEY } from '../useTts'
import { SettingsModal } from '../full/SettingsModal'

const speakMock = vi.fn()
const cancelMock = vi.fn()
const getVoicesMock = vi.fn(() => [
  { lang: 'en-US', name: 'Samantha' },
  { lang: 'zh-CN', name: 'Ting-Ting' },
])

class FakeUtterance {
  text: string
  lang = ''
  voice: unknown = null
  onend: (() => void) | null = null
  constructor(text: string) {
    this.text = text
  }
}

function stubSpeech() {
  vi.stubGlobal('speechSynthesis', { speak: speakMock, cancel: cancelMock, getVoices: getVoicesMock })
  vi.stubGlobal('SpeechSynthesisUtterance', FakeUtterance)
}

describe('stripMarkdown 朗读前清洗', () => {
  it('去标题/加粗/代码/链接，多行合并', () => {
    expect(stripMarkdown('# 标题\n**你好**，这是[链接](http://x)回复')).toBe('标题，你好，这是链接回复')
    expect(stripMarkdown('`code` 与 ```\nblock\n``` 结束')).toBe('code 与 结束')
    expect(stripMarkdown('- 第一项\n- 第二项')).toBe('第一项，第二项')
  })
})

describe('speakText 语音朗读', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubSpeech()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('中文语音优先，先 cancel 再 speak', () => {
    expect(speakText('你好')).toBe(true)
    expect(cancelMock).toHaveBeenCalledTimes(1)
    expect(speakMock).toHaveBeenCalledTimes(1)
    const u = speakMock.mock.calls[0][0] as FakeUtterance
    expect(u.text).toBe('你好')
    expect(u.lang).toBe('zh-CN')
    expect((u.voice as { name: string }).name).toBe('Ting-Ting')
  })

  it('超过 300 字符截断并追加省略号', () => {
    speakText('汉'.repeat(400))
    const u = speakMock.mock.calls[0][0] as FakeUtterance
    expect(u.text.length).toBe(301)
    expect(u.text.endsWith('…')).toBe(true)
  })

  it('空内容（纯 Markdown 符号）不发声', () => {
    expect(speakText('```\n```')).toBe(false)
    expect(speakMock).not.toHaveBeenCalled()
  })

  it('浏览器不支持时返回 false', () => {
    vi.unstubAllGlobals()
    expect(speakText('你好')).toBe(false)
  })
})

describe('SettingsModal 朗读开关', () => {
  beforeEach(() => {
    localStorage.clear()
    stubSpeech()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('开关持久化到 localStorage', () => {
    expect(loadTtsEnabled()).toBe(false)
    render(
      <SettingsModal
        settings={{ system_prompt: '', temperature: 0.7, model: '' }}
        onSave={() => {}}
        onClose={() => {}}
        providers={[]}
      />
    )
    const sw = screen.getByRole('switch', { name: '朗读助手回复' })
    expect(sw.getAttribute('aria-checked')).toBe('false')
    fireEvent.click(sw)
    expect(localStorage.getItem(TTS_KEY)).toBe('1')
    expect(screen.getByRole('switch', { name: '朗读助手回复' }).getAttribute('aria-checked')).toBe('true')
    fireEvent.click(screen.getByRole('switch', { name: '朗读助手回复' }))
    expect(localStorage.getItem(TTS_KEY)).toBe('0')
  })
})
