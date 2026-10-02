import { useCallback, useState } from 'react'

// ── 语音输出（speechSynthesis 朗读，零后端） ──
export const TTS_KEY = 'xt-agent-tts'

export function ttsSupported(): boolean {
  return typeof window !== 'undefined' && 'speechSynthesis' in window
}

export function loadTtsEnabled(): boolean {
  try {
    return localStorage.getItem(TTS_KEY) === '1'
  } catch {
    return false
  }
}

export function saveTtsEnabled(v: boolean) {
  try {
    localStorage.setItem(TTS_KEY, v ? '1' : '0')
  } catch {
    /* 存储满等情况静默失败 */
  }
}

/** Markdown → 纯文本（朗读用）：去代码块/标记/链接语法，多行合并为短句 */
export function stripMarkdown(md: string): string {
  return md
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/^#{1,6}\s*/gm, '')
    .replace(/(\*\*|__)(.*?)\1/g, '$2')
    .replace(/(\*|_)(.*?)\1/g, '$2')
    .replace(/~~(.*?)~~/g, '$1')
    .replace(/^\s*[-*+>]\s+/gm, '')
    .replace(/\n{2,}/g, '。')
    .replace(/\n/g, '，')
    .replace(/\s{2,}/g, ' ')
    .trim()
}

/** 朗读一段文本（去 Markdown、封顶 300 字符、优先中文语音）；返回是否实际发起 */
export function speakText(text: string, opts?: { onend?: () => void }): boolean {
  if (!ttsSupported()) return false
  const clean = stripMarkdown(text)
  if (!clean) return false
  const capped = clean.length > 300 ? `${clean.slice(0, 300)}…` : clean
  const utterance = new SpeechSynthesisUtterance(capped)
  utterance.lang = 'zh-CN'
  try {
    const zh = window.speechSynthesis.getVoices?.().find((v) => v.lang?.toLowerCase().startsWith('zh'))
    if (zh) utterance.voice = zh
  } catch {
    /* getVoices 不可用时用默认语音 */
  }
  if (opts?.onend) utterance.onend = opts.onend
  window.speechSynthesis.cancel()
  window.speechSynthesis.speak(utterance)
  return true
}

export function stopSpeak() {
  if (ttsSupported()) window.speechSynthesis.cancel()
}

/** 设置开关（localStorage `xt-agent-tts`）+ 朗读入口（关闭时不发声） */
export function useTts() {
  const [enabled, setEnabledState] = useState(loadTtsEnabled)
  const setEnabled = useCallback((v: boolean) => {
    setEnabledState(v)
    saveTtsEnabled(v)
    if (!v) stopSpeak()
  }, [])
  const speak = useCallback(
    (text: string) => {
      if (enabled) speakText(text)
    },
    [enabled]
  )
  return { enabled, setEnabled, speak, supported: ttsSupported() }
}
