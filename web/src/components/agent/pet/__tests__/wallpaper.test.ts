import { describe, it, expect, beforeEach } from 'vitest'
import {
  DEFAULT_FX,
  detectKind,
  fxToStyle,
  loadActive,
  loadFx,
  loadLibrary,
  newWallpaperId,
  nextWallpaper,
  resolveActive,
  saveActive,
  saveLibrary,
  type WallpaperEntry,
} from '../wallpaper'

const entry = (id: string, kind: 'image' | 'video' = 'image'): WallpaperEntry => ({
  id,
  kind,
  src: kind === 'video' ? 'https://x/w.mp4' : 'https://x/w.jpg',
  name: id,
  addedAt: 0,
})

describe('壁纸引擎存储', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('detectKind 按扩展名与 dataURL 识别', () => {
    expect(detectKind('https://a/b.mp4')).toBe('video')
    expect(detectKind('https://a/b.webm')).toBe('video')
    expect(detectKind('https://a/b.jpg')).toBe('image')
    expect(detectKind('data:video/mp4;base64,xx')).toBe('video')
    expect(detectKind('data:image/png;base64,xx')).toBe('image')
    expect(detectKind('x', 'a.MP4')).toBe('video')
  })

  it('库读写 + 解析当前壁纸', () => {
    saveLibrary([entry('a'), entry('b', 'video')])
    const lib = loadLibrary()
    expect(lib.length).toBe(2)
    expect(resolveActive(lib, 'b')?.kind).toBe('video')
    // activeId 无效回退第一张
    expect(resolveActive(lib, 'nope')?.id).toBe('a')
    // 空库 → null
    expect(resolveActive([], null)).toBeNull()
  })

  it('旧版单壁纸设置自动迁移', () => {
    localStorage.setItem('xt-agent-wallpaper', 'https://x/legacy.jpg')
    const lib = loadLibrary()
    expect(lib.length).toBe(1)
    expect(lib[0].src).toBe('https://x/legacy.jpg')
    expect(loadActive()).toBe(lib[0].id)
    expect(localStorage.getItem('xt-agent-wallpaper')).toBeNull()
  })

  it('nextWallpaper 循环取下一张', () => {
    const lib = [entry('a'), entry('b'), entry('c')]
    expect(nextWallpaper(lib, 'a')?.id).toBe('b')
    expect(nextWallpaper(lib, 'c')?.id).toBe('a')
    expect(nextWallpaper(lib, null)?.id).toBe('a')
    expect(nextWallpaper([], 'a')).toBeNull()
  })

  it('fxToStyle 生成 filter/opacity', () => {
    expect(fxToStyle(DEFAULT_FX)).toEqual({ filter: 'brightness(0.90)', opacity: 1 })
    expect(fxToStyle({ blur: 8, brightness: 100, opacity: 50, darken: 0 })).toEqual({
      filter: 'blur(8px)',
      opacity: 0.5,
    })
    expect(fxToStyle({ blur: 0, brightness: 100, opacity: 100, darken: 0 })).toEqual({ filter: 'none', opacity: 1 })
  })

  it('active/fx 持久化', () => {
    saveActive('wp_1')
    expect(loadActive()).toBe('wp_1')
    expect(newWallpaperId()).toMatch(/^wp_/)
    const fx = { ...DEFAULT_FX, blur: 6 }
    localStorage.setItem('xt-wallpaper-fx', JSON.stringify(fx))
    expect(loadFx().blur).toBe(6)
  })
})
