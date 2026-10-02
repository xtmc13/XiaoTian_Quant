import { useEffect, useState } from 'react'
import { useWallpaper } from '../pet/WallpaperContext'
import { WallpaperMedia } from '../pet/WallpaperMedia'
import { DEFAULT_FX } from '../pet/wallpaper'
import { cn } from '@/lib/utils'

/** 开屏总时长（ms）与淡出时长 */
const BOOT_MS = 2400
const FADE_MS = 450

export interface BootSplashProps {
  version?: string
  onDone: () => void
}

// ── 开屏动画：大 Logo + 欢迎语 + 进度条（对标 dsh-boot-animation） ──
export function BootSplash({ version, onDone }: BootSplashProps) {
  const wp = useWallpaper()
  const wallpaper = wp?.active ?? null
  const fx = wp?.fx ?? DEFAULT_FX
  const [progress, setProgress] = useState(0)
  const [fading, setFading] = useState(false)

  useEffect(() => {
    const start = Date.now()
    const t = setInterval(() => {
      const p = Math.min(100, Math.round(((Date.now() - start) / BOOT_MS) * 100))
      setProgress(p)
      if (p >= 100) {
        clearInterval(t)
        setFading(true)
        setTimeout(onDone, FADE_MS)
      }
    }, 60)
    return () => clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const skip = () => {
    if (fading) return
    setFading(true)
    setTimeout(onDone, FADE_MS)
  }

  return (
    <div
      role="dialog"
      aria-label="开屏动画"
      onClick={skip}
      className={cn(
        'fixed inset-0 z-[100] flex select-none flex-col items-center justify-center overflow-hidden bg-[#0b0c10] transition-opacity duration-[450ms]',
        fading && 'pointer-events-none opacity-0'
      )}
    >
      {/* 背景：壁纸（图片/视频循环） or 默认光效 */}
      {wallpaper ? (
        <div className="absolute inset-0 overflow-hidden">
          <WallpaperMedia entry={wallpaper} fx={fx} className="absolute inset-0 h-full w-full" />
        </div>
      ) : (
        <div className="absolute inset-0">
          <div className="absolute left-1/2 top-1/3 h-[60vmin] w-[80vmin] -translate-x-1/2 -translate-y-1/2 rounded-full bg-[var(--ag-accent)]/16 blur-[110px]" />
          <div className="absolute bottom-0 right-0 h-[40vmin] w-[50vmin] translate-x-1/4 translate-y-1/4 rounded-full bg-[#7aa2ff]/10 blur-[100px]" />
          <div
            className="absolute inset-0 opacity-[0.35]"
            style={{
              backgroundImage: 'radial-gradient(rgba(255,255,255,0.22) 1px, transparent 1px)',
              backgroundSize: '28px 28px',
              maskImage: 'radial-gradient(ellipse at center, black 30%, transparent 75%)',
              WebkitMaskImage: 'radial-gradient(ellipse at center, black 30%, transparent 75%)',
            }}
          />
        </div>
      )}
      <div className="absolute inset-0 bg-gradient-to-b from-black/30 via-transparent to-black/55" />

      {/* 内容 */}
      <div className="relative flex flex-col items-center px-6">
        <h1 className="bg-gradient-to-br from-white via-white to-[#8fb0ff] bg-clip-text text-center text-[13vmin] font-black leading-none tracking-tight text-transparent drop-shadow-[0_0_35px_rgba(77,107,254,0.35)]">
          小天量化
        </h1>
        <p className="mt-4 flex items-center gap-2 text-[11px] tracking-[0.35em] text-white/50">
          <span className="h-px w-8 bg-white/25" />
          欢迎来到专业工作模式
          <span className="h-px w-8 bg-white/25" />
        </p>
        <p className="mt-1.5 text-[9px] tracking-[0.5em] text-white/30">XIAOTIAN QUANT PROFESSIONAL MODE</p>

        {/* 进度条 */}
        <div className="mt-10 h-[3px] w-[46vmin] max-w-xs overflow-hidden rounded-full bg-white/10">
          <div
            className="h-full rounded-full bg-gradient-to-r from-[var(--ag-accent)] to-[#8fb0ff] transition-[width] duration-100"
            style={{ width: `${progress}%` }}
          />
        </div>
        <p className="mt-3 font-mono text-[10px] tracking-widest text-white/40">
          initializing agent
          <span className="inline-block w-4 animate-pulse">…</span> {progress}%
        </p>
      </div>

      <p className="absolute bottom-5 right-6 text-[10px] text-white/30">点击任意处跳过</p>
      {version && <p className="absolute bottom-5 left-6 font-mono text-[10px] text-white/25">{version}</p>}
    </div>
  )
}

export default BootSplash
