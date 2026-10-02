// ── 壁纸引擎存储层：壁纸库 / 画面调节 / 自动轮播（localStorage） ──

export interface WallpaperEntry {
  id: string
  /** image=静态图 video=MP4/WebM 循环 */
  kind: 'image' | 'video'
  /** dataURL 或 http(s) URL */
  src: string
  name: string
  addedAt: number
}

export interface WallpaperFx {
  /** 模糊 0-20 px */
  blur: number
  /** 亮度 40-140 % */
  brightness: number
  /** 整体不透明度 10-100 % */
  opacity: number
  /** 暗化遮罩 0-90 %（压在壁纸与文字之间的黑色 scrim） */
  darken: number
}

export interface WallpaperRotate {
  enabled: boolean
  /** 切换间隔（分钟） */
  intervalMin: number
}

export const DEFAULT_FX: WallpaperFx = { blur: 0, brightness: 90, opacity: 100, darken: 55 }
export const DEFAULT_ROTATE: WallpaperRotate = { enabled: false, intervalMin: 5 }
export const MAX_WALLPAPER_BYTES = 8 * 1024 * 1024

const KEYS = {
  library: 'xt-wallpapers',
  active: 'xt-wallpaper-active',
  fx: 'xt-wallpaper-fx',
  rotate: 'xt-wallpaper-rotate',
  legacy: 'xt-agent-wallpaper',
}

export function newWallpaperId(): string {
  return `wp_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`
}

/** 从文件名/URL 推断壁纸类型 */
export function detectKind(src: string, fallbackName = ''): 'image' | 'video' {
  const tail = (fallbackName || src.split('?')[0]).toLowerCase()
  if (/\.(mp4|webm|mov|m4v)$/.test(tail)) return 'video'
  if (src.startsWith('data:video') || src.startsWith('data:image/gif'))
    return src.startsWith('data:video') ? 'video' : 'image'
  return 'image'
}

function read<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : null
  } catch {
    return null
  }
}

/** 读取壁纸库（含旧版单壁纸迁移） */
export function loadLibrary(): WallpaperEntry[] {
  const list = read<WallpaperEntry[]>(KEYS.library)
  if (list && Array.isArray(list)) return list
  // 迁移旧版单壁纸设置
  try {
    const legacy = localStorage.getItem(KEYS.legacy)
    if (legacy) {
      const entry: WallpaperEntry = {
        id: newWallpaperId(),
        kind: detectKind(legacy),
        src: legacy,
        name: '壁纸',
        addedAt: Date.now(),
      }
      saveLibrary([entry])
      saveActive(entry.id)
      localStorage.removeItem(KEYS.legacy)
      return [entry]
    }
  } catch {
    /* 静默 */
  }
  return []
}

export function saveLibrary(list: WallpaperEntry[]) {
  try {
    localStorage.setItem(KEYS.library, JSON.stringify(list))
  } catch {
    /* 存储满等情况静默失败 */
  }
}

export function loadActive(): string | null {
  try {
    return localStorage.getItem(KEYS.active)
  } catch {
    return null
  }
}

export function saveActive(id: string | null) {
  try {
    if (id) localStorage.setItem(KEYS.active, id)
    else localStorage.removeItem(KEYS.active)
  } catch {
    /* 静默 */
  }
}

export function loadFx(): WallpaperFx {
  const raw = read<Partial<WallpaperFx>>(KEYS.fx)
  return { ...DEFAULT_FX, ...(raw || {}) }
}

export function saveFx(fx: WallpaperFx) {
  try {
    localStorage.setItem(KEYS.fx, JSON.stringify(fx))
  } catch {
    /* 静默 */
  }
}

export function loadRotate(): WallpaperRotate {
  const raw = read<Partial<WallpaperRotate>>(KEYS.rotate)
  return { ...DEFAULT_ROTATE, ...(raw || {}) }
}

export function saveRotate(r: WallpaperRotate) {
  try {
    localStorage.setItem(KEYS.rotate, JSON.stringify(r))
  } catch {
    /* 静默 */
  }
}

/** 解析当前生效壁纸（activeId 无效时回退库内第一张） */
export function resolveActive(library: WallpaperEntry[], activeId: string | null): WallpaperEntry | null {
  if (library.length === 0) return null
  return library.find((w) => w.id === activeId) || library[0]
}

/** 轮播：取当前壁纸在库中的下一个（跳过当前失效项） */
export function nextWallpaper(library: WallpaperEntry[], activeId: string | null): WallpaperEntry | null {
  if (library.length === 0) return null
  const idx = library.findIndex((w) => w.id === activeId)
  return library[(idx + 1) % library.length]
}

/** fx → CSS filter / opacity 字符串 */
export function fxToStyle(fx: WallpaperFx): { filter: string; opacity: number } {
  const parts: string[] = []
  if (fx.brightness !== 100) parts.push(`brightness(${(fx.brightness / 100).toFixed(2)})`)
  if (fx.blur > 0) parts.push(`blur(${fx.blur}px)`)
  return { filter: parts.join(' ') || 'none', opacity: fx.opacity / 100 }
}
