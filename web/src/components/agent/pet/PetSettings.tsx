import { useRef, useState } from 'react'
import { Check, Clapperboard, Image as ImageIcon, ImagePlus, Link2, RotateCcw, Trash2, X } from 'lucide-react'
import { useWallpaper } from './WallpaperContext'
import { MAX_WALLPAPER_BYTES, detectKind, type WallpaperFx } from './wallpaper'
import { cn } from '@/lib/utils'

export interface PetConfig {
  /** 自定义形象 dataURL；空串 = 默认小天鲸 */
  image: string
  size: number
  anim: boolean
}

export const PET_KEYS = {
  image: 'xt-pet-image',
  size: 'xt-pet-size',
  anim: 'xt-pet-anim',
  pos: 'xt-pet-pos',
}

export const PET_DEFAULT_SIZE = 72

export function loadPetConfig(): PetConfig {
  let image = ''
  let size = PET_DEFAULT_SIZE
  let anim = true
  try {
    image = localStorage.getItem(PET_KEYS.image) || ''
    const s = Number(localStorage.getItem(PET_KEYS.size))
    if (s >= 48 && s <= 160) size = s
    anim = localStorage.getItem(PET_KEYS.anim) !== 'off'
  } catch {
    /* 静默 */
  }
  return { image, size, anim }
}

export function savePetConfig(cfg: PetConfig) {
  try {
    localStorage.setItem(PET_KEYS.image, cfg.image)
    localStorage.setItem(PET_KEYS.size, String(cfg.size))
    localStorage.setItem(PET_KEYS.anim, cfg.anim ? 'on' : 'off')
  } catch {
    /* 存储满等情况静默失败 */
  }
}

export interface PetPosition {
  x: number
  y: number
}

export function loadPetPos(): PetPosition | null {
  try {
    const raw = localStorage.getItem(PET_KEYS.pos)
    if (!raw) return null
    const p = JSON.parse(raw)
    if (typeof p.x === 'number' && typeof p.y === 'number') return p as PetPosition
  } catch {
    /* 静默 */
  }
  return null
}

export function savePetPos(p: PetPosition | null) {
  try {
    if (p) localStorage.setItem(PET_KEYS.pos, JSON.stringify(p))
    else localStorage.removeItem(PET_KEYS.pos)
  } catch {
    /* 静默 */
  }
}

const PET_IMAGE_MAX_BYTES = 2 * 1024 * 1024

export interface PetSettingsProps {
  config: PetConfig
  onChange: (cfg: PetConfig) => void
  onResetPos: () => void
  onClose: () => void
}

// ── 壁纸引擎：壁纸库 / 当前壁纸 / 调节滑条 / 自动轮播 ──
function WallpaperEngine() {
  const wp = useWallpaper()
  const fileRef = useRef<HTMLInputElement>(null)
  const [url, setUrl] = useState('')
  const [error, setError] = useState('')
  if (!wp) return null

  const addFromFile = (file: File) => {
    if (file.size > MAX_WALLPAPER_BYTES) {
      setError('壁纸文件不能超过 8MB')
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const src = String(reader.result || '')
      wp.addWallpaper({ kind: detectKind(src, file.name), src, name: file.name || '壁纸' })
      setError('')
    }
    reader.onerror = () => setError('读取文件失败')
    reader.readAsDataURL(file)
  }

  const addFromUrl = () => {
    const u = url.trim()
    if (!u) return
    wp.addWallpaper({ kind: detectKind(u), src: u, name: u.split('/').pop()?.slice(0, 24) || '壁纸' })
    setUrl('')
    setError('')
  }

  const fxSlider = (key: keyof WallpaperFx, label: string, min: number, max: number, unit: string) => (
    <div className="flex items-center gap-2">
      <span className="w-8 shrink-0 text-[10px] text-[var(--ag-text3)]">{label}</span>
      <input
        type="range"
        min={min}
        max={max}
        value={wp.fx[key]}
        onChange={(e) => wp.setFx({ ...wp.fx, [key]: Number(e.target.value) })}
        aria-label={label}
        className="min-w-0 flex-1 accent-[var(--ag-accent)]"
      />
      <span className="w-10 shrink-0 text-right text-[9px] tabular-nums text-[var(--ag-text4)]">
        {wp.fx[key]}
        {unit}
      </span>
    </div>
  )

  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">壁纸引擎</span>
        <button
          type="button"
          onClick={() => fileRef.current?.click()}
          className="flex items-center gap-1 text-[10px] text-[var(--ag-accent)] hover:opacity-80"
        >
          <ImagePlus size={10} />
          上传
        </button>
      </div>
      <input
        ref={fileRef}
        type="file"
        accept="image/*,video/mp4,video/webm"
        className="hidden"
        aria-label="上传壁纸"
        onChange={(e) => {
          const f = e.target.files?.[0]
          e.target.value = ''
          if (f) addFromFile(f)
        }}
      />
      <div className="flex gap-1">
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="或粘贴图片/MP4 链接"
          aria-label="壁纸链接"
          className="min-w-0 flex-1 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1 text-[11px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/50 focus:outline-none"
        />
        <button
          type="button"
          onClick={addFromUrl}
          disabled={!url.trim()}
          className="shrink-0 rounded-lg bg-[var(--ag-text1)] px-2 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85 disabled:opacity-40"
        >
          添加
        </button>
      </div>
      {error && <p className="mt-1 text-[10px] text-[var(--ag-red)]">{error}</p>}

      {/* 壁纸库列表 */}
      <div className="mt-1.5 max-h-36 space-y-0.5 overflow-y-auto" aria-label="壁纸库">
        {/* 无壁纸（默认光效） */}
        <button
          type="button"
          onClick={() => wp.setActive(null)}
          aria-pressed={!wp.active}
          className={cn(
            'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[11px] transition-colors',
            !wp.active ? 'bg-[var(--ag-accent)]/10 text-[var(--ag-accent)]' : 'text-[var(--ag-text3)] hover:bg-white/5'
          )}
        >
          <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-gradient-to-br from-[var(--ag-accent)]/40 to-[#7aa2ff]/30 text-[var(--ag-text2)]">
            ✦
          </span>
          <span className="min-w-0 flex-1 truncate">默认光效背景</span>
          {!wp.active && <Check size={12} className="shrink-0" />}
        </button>
        {wp.library.map((w) => (
          <div
            key={w.id}
            role="button"
            tabIndex={0}
            onClick={() => wp.setActive(w.id)}
            onKeyDown={(e) => e.key === 'Enter' && wp.setActive(w.id)}
            aria-pressed={wp.active?.id === w.id}
            className={cn(
              'group flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors',
              wp.active?.id === w.id ? 'bg-[var(--ag-accent)]/10' : 'hover:bg-white/5'
            )}
          >
            {w.kind === 'video' ? (
              <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-black/40 text-[var(--ag-text3)]">
                <Clapperboard size={13} />
              </span>
            ) : (
              <img src={w.src} alt="" className="size-8 shrink-0 rounded-md object-cover" draggable={false} />
            )}
            <span className="min-w-0 flex-1 truncate text-[11px] text-[var(--ag-text2)]">{w.name}</span>
            {wp.active?.id === w.id && <Check size={12} className="shrink-0 text-[var(--ag-accent)]" />}
            <button
              type="button"
              title="删除壁纸"
              aria-label={`删除壁纸 ${w.name}`}
              onClick={(e) => {
                e.stopPropagation()
                wp.removeWallpaper(w.id)
              }}
              className="shrink-0 rounded p-0.5 text-[var(--ag-text4)] opacity-0 transition-opacity group-hover:opacity-100 hover:text-[var(--ag-red)]"
            >
              <Trash2 size={11} />
            </button>
          </div>
        ))}
      </div>

      {/* 调节滑条 */}
      <div className="mt-2 space-y-1">
        {fxSlider('blur', '模糊', 0, 20, 'px')}
        {fxSlider('brightness', '亮度', 40, 140, '%')}
        {fxSlider('opacity', '透明', 10, 100, '%')}
        {fxSlider('darken', '暗化', 0, 90, '%')}
      </div>

      {/* 自动轮播 */}
      <div className="mt-2 flex items-center gap-2">
        <label className="flex flex-1 cursor-pointer items-center gap-1.5 text-[11px] text-[var(--ag-text2)]">
          <input
            type="checkbox"
            checked={wp.rotate.enabled}
            onChange={(e) => wp.setRotate({ ...wp.rotate, enabled: e.target.checked })}
            disabled={wp.library.length < 2}
            aria-label="自动轮播壁纸"
            className="accent-[var(--ag-accent)] disabled:opacity-40"
          />
          自动轮播{wp.library.length < 2 ? '（需≥2张）' : ''}
        </label>
        <select
          value={wp.rotate.intervalMin}
          onChange={(e) => wp.setRotate({ ...wp.rotate, intervalMin: Number(e.target.value) })}
          disabled={!wp.rotate.enabled}
          aria-label="轮播间隔"
          className="rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-1.5 py-0.5 text-[10px] text-[var(--ag-text2)] focus:outline-none disabled:opacity-40"
        >
          {[1, 5, 15, 30].map((m) => (
            <option key={m} value={m}>
              {m} 分钟
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}

// ── 桌宠设置弹层：自定义形象 / 尺寸 / 动画 / 壁纸引擎 / 重置 ──
export function PetSettings({ config, onChange, onResetPos, onClose }: PetSettingsProps) {
  const fileRef = useRef<HTMLInputElement>(null)
  const [url, setUrl] = useState('')
  const [error, setError] = useState('')

  const applyImage = (dataURL: string) => {
    onChange({ ...config, image: dataURL })
    setError('')
  }

  const onPickFile = (file: File) => {
    if (file.size > PET_IMAGE_MAX_BYTES) {
      setError('图片不能超过 2MB')
      return
    }
    const reader = new FileReader()
    reader.onload = () => applyImage(String(reader.result || ''))
    reader.onerror = () => setError('读取图片失败')
    reader.readAsDataURL(file)
  }

  return (
    <div
      role="dialog"
      aria-label="桌宠设置"
      className="absolute bottom-full right-0 z-[95] mb-3 max-h-[72vh] w-80 overflow-y-auto rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/95 p-3 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
      onClick={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
    >
      <div className="mb-2 flex items-center">
        <span className="text-[12px] font-semibold text-[var(--ag-text1)]">桌宠设置</span>
        <span className="min-w-0 flex-1" />
        <button
          type="button"
          onClick={onClose}
          aria-label="关闭桌宠设置"
          className="rounded p-1 text-[var(--ag-text3)] hover:bg-white/6"
        >
          <X size={13} />
        </button>
      </div>

      {/* 形象 */}
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">形象</div>
      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        className="hidden"
        aria-label="上传桌宠图片"
        onChange={(e) => {
          const f = e.target.files?.[0]
          e.target.value = ''
          if (f) onPickFile(f)
        }}
      />
      <div className="flex gap-1.5">
        <button
          type="button"
          onClick={() => fileRef.current?.click()}
          className="flex flex-1 items-center justify-center gap-1 rounded-lg border border-[var(--ag-stroke2)] px-2 py-1.5 text-[11px] text-[var(--ag-text2)] hover:border-[var(--ag-accent)]/50 hover:text-[var(--ag-accent)]"
        >
          <ImagePlus size={12} />
          上传图片
        </button>
        <button
          type="button"
          onClick={() => {
            onChange({ ...config, image: '' })
            setError('')
          }}
          disabled={!config.image}
          className="flex items-center gap-1 rounded-lg border border-[var(--ag-stroke2)] px-2 py-1.5 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-text1)] disabled:opacity-40"
        >
          <RotateCcw size={12} />
          默认
        </button>
      </div>
      <div className="mt-1.5 flex gap-1">
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="或粘贴图片 URL（支持动图）"
          aria-label="图片地址"
          className="min-w-0 flex-1 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1 text-[11px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/50 focus:outline-none"
        />
        <button
          type="button"
          onClick={() => {
            if (url.trim()) {
              applyImage(url.trim())
              setUrl('')
            }
          }}
          disabled={!url.trim()}
          className="shrink-0 rounded-lg bg-[var(--ag-text1)] px-2 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85 disabled:opacity-40"
        >
          使用
        </button>
      </div>
      {error && <p className="mt-1 text-[10px] text-[var(--ag-red)]">{error}</p>}

      {/* 尺寸 */}
      <div className="mb-1 mt-2.5 flex items-center justify-between">
        <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">尺寸</span>
        <span className="text-[10px] tabular-nums text-[var(--ag-text3)]">{config.size}px</span>
      </div>
      <input
        type="range"
        min={48}
        max={140}
        value={config.size}
        onChange={(e) => onChange({ ...config, size: Number(e.target.value) })}
        aria-label="桌宠尺寸"
        className="w-full accent-[var(--ag-accent)]"
      />

      {/* 动画 + 重置位置 */}
      <div className="mt-2.5 flex items-center gap-2">
        <label className="flex flex-1 cursor-pointer items-center gap-1.5 text-[11px] text-[var(--ag-text2)]">
          <input
            type="checkbox"
            checked={config.anim}
            onChange={(e) => onChange({ ...config, anim: e.target.checked })}
            aria-label="待机动画"
            className="accent-[var(--ag-accent)]"
          />
          待机浮动动画
        </label>
        <button
          type="button"
          onClick={onResetPos}
          className="shrink-0 rounded-lg border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-text1)]"
        >
          重置位置
        </button>
      </div>

      {/* 壁纸引擎 */}
      <div className="mt-3 border-t border-[var(--ag-stroke3)] pt-2.5">
        <WallpaperEngine />
      </div>
    </div>
  )
}

export function petShadowClass(anim: boolean) {
  return cn('absolute -bottom-1 left-1/2 h-2 w-3/5 -translate-x-1/2 rounded-full bg-black', anim && 'xt-pet-shadow')
}

export default PetSettings
