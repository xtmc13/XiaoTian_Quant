import { fxToStyle, type WallpaperEntry, type WallpaperFx } from './wallpaper'

export interface WallpaperMediaProps {
  entry: WallpaperEntry
  fx: WallpaperFx
  className?: string
}

// ── 壁纸媒体：image / video(MP4 循环) + 调节滤镜（模糊/亮度/透明度 + 暗化遮罩） ──
export function WallpaperMedia({ entry, fx, className }: WallpaperMediaProps) {
  const { filter, opacity } = fxToStyle(fx)
  return (
    <>
      {entry.kind === 'video' ? (
        <video
          src={entry.src}
          autoPlay
          muted
          loop
          playsInline
          aria-hidden
          className={className}
          style={{ filter, opacity, objectFit: 'cover' }}
        />
      ) : (
        <img
          src={entry.src}
          alt=""
          draggable={false}
          aria-hidden
          className={className}
          style={{ filter, opacity, objectFit: 'cover' }}
        />
      )}
      {/* 暗化遮罩：压文字对比度 */}
      {fx.darken > 0 && (
        <div
          aria-hidden
          className="pointer-events-none absolute inset-0 bg-black"
          style={{ opacity: fx.darken / 100 }}
        />
      )}
    </>
  )
}

export default WallpaperMedia
