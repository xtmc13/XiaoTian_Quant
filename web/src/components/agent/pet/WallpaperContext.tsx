import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  loadActive,
  loadFx,
  loadLibrary,
  loadRotate,
  nextWallpaper,
  resolveActive,
  saveActive,
  saveFx,
  saveLibrary,
  saveRotate,
  type WallpaperEntry,
  type WallpaperFx,
  type WallpaperRotate,
} from './wallpaper'

export interface WallpaperState {
  library: WallpaperEntry[]
  active: WallpaperEntry | null
  fx: WallpaperFx
  rotate: WallpaperRotate
  setActive: (id: string | null) => void
  addWallpaper: (entry: Omit<WallpaperEntry, 'id' | 'addedAt'>) => WallpaperEntry
  removeWallpaper: (id: string) => void
  setFx: (fx: WallpaperFx) => void
  setRotate: (r: WallpaperRotate) => void
}

const WallpaperContext = createContext<WallpaperState | null>(null)

/** 壁纸引擎状态：库 / 当前 / 调节 / 轮播（仅 DesktopPet 树内可用，测试外可直接 useWallpaper） */
export function WallpaperProvider({ children }: { children: ReactNode }) {
  const [library, setLibrary] = useState<WallpaperEntry[]>(loadLibrary)
  const [activeId, setActiveId] = useState<string | null>(loadActive)
  const [fx, setFxState] = useState<WallpaperFx>(loadFx)
  const [rotate, setRotateState] = useState<WallpaperRotate>(loadRotate)

  const active = useMemo(() => resolveActive(library, activeId), [library, activeId])

  const setActive = useCallback((id: string | null) => {
    setActiveId(id)
    saveActive(id)
  }, [])

  const addWallpaper = useCallback((entry: Omit<WallpaperEntry, 'id' | 'addedAt'>) => {
    const full: WallpaperEntry = {
      ...entry,
      id: `wp_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 7)}`,
      addedAt: Date.now(),
    }
    setLibrary((prev) => {
      const next = [...prev, full]
      saveLibrary(next)
      return next
    })
    return full
  }, [])

  const removeWallpaper = useCallback(
    (id: string) => {
      setLibrary((prev) => {
        const next = prev.filter((w) => w.id !== id)
        saveLibrary(next)
        return next
      })
      if (activeId === id) {
        setActiveId(null)
        saveActive(null)
      }
    },
    [activeId]
  )

  const setFx = useCallback((fx: WallpaperFx) => {
    setFxState(fx)
    saveFx(fx)
  }, [])

  const setRotate = useCallback((r: WallpaperRotate) => {
    setRotateState(r)
    saveRotate(r)
  }, [])

  // 自动轮播（到点切下一张，无需界面打开）
  useEffect(() => {
    if (!rotate.enabled || library.length < 2) return
    const t = setInterval(
      () => {
        setActiveId((prev) => {
          const nxt = nextWallpaper(library, prev)
          saveActive(nxt?.id ?? null)
          return nxt?.id ?? null
        })
      },
      Math.max(1, rotate.intervalMin) * 60_000
    )
    return () => clearInterval(t)
  }, [rotate.enabled, rotate.intervalMin, library])

  const value = useMemo(
    () => ({ library, active, fx, rotate, setActive, addWallpaper, removeWallpaper, setFx, setRotate }),
    [library, active, fx, rotate, setActive, addWallpaper, removeWallpaper, setFx, setRotate]
  )

  return <WallpaperContext.Provider value={value}>{children}</WallpaperContext.Provider>
}

/** 无 Provider 时返回 null 状态兜底（测试/悬浮面板场景） */
export function useWallpaper(): WallpaperState | null {
  return useContext(WallpaperContext)
}

export default WallpaperContext
