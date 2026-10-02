import { useRef, useState } from 'react'
import { ImagePlus, RotateCcw, X } from 'lucide-react'

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

const MAX_IMAGE_BYTES = 2 * 1024 * 1024

export interface PetSettingsProps {
  config: PetConfig
  onChange: (cfg: PetConfig) => void
  onResetPos: () => void
  onClose: () => void
  /** 助手开屏壁纸（dataURL/URL），空 = 默认光效 */
  wallpaper?: string
  onWallpaperChange?: (url: string) => void
}

// ── 桌宠设置弹层：自定义形象 / 尺寸 / 动画 / 开屏壁纸 / 重置 ──
export function PetSettings({
  config,
  onChange,
  onResetPos,
  onClose,
  wallpaper = '',
  onWallpaperChange,
}: PetSettingsProps) {
  const fileRef = useRef<HTMLInputElement>(null)
  const wallRef = useRef<HTMLInputElement>(null)
  const [url, setUrl] = useState('')
  const [wallUrl, setWallUrl] = useState('')
  const [error, setError] = useState('')

  const applyImage = (dataURL: string) => {
    onChange({ ...config, image: dataURL })
    setError('')
  }

  const onPickFile = (file: File) => {
    if (file.size > MAX_IMAGE_BYTES) {
      setError('图片不能超过 2MB')
      return
    }
    const reader = new FileReader()
    reader.onload = () => applyImage(String(reader.result || ''))
    reader.onerror = () => setError('读取图片失败')
    reader.readAsDataURL(file)
  }

  const onApplyUrl = () => {
    const u = url.trim()
    if (!u) return
    applyImage(u)
    setUrl('')
  }

  return (
    <div
      role="dialog"
      aria-label="桌宠设置"
      className="absolute bottom-full right-0 z-[95] mb-3 w-64 rounded-2xl border border-[var(--ag-stroke3)] bg-[var(--ag-card)]/95 p-3 shadow-[var(--ag-shadow-panel)] backdrop-blur-xl"
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
          className="rounded p-1 text-[var(--ag-text3)] hover:bg-white/8"
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
          onClick={onApplyUrl}
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

      {/* 开屏壁纸 */}
      {onWallpaperChange && (
        <>
          <div className="mb-1 mt-2.5 flex items-center justify-between">
            <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-[var(--ag-text4)]">
              开屏壁纸
            </span>
            <button
              type="button"
              onClick={() => wallRef.current?.click()}
              className="text-[10px] text-[var(--ag-accent)] hover:opacity-80"
            >
              上传
            </button>
          </div>
          <input
            ref={wallRef}
            type="file"
            accept="image/*"
            className="hidden"
            aria-label="上传开屏壁纸"
            onChange={(e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (!f) return
              if (f.size > 4 * 1024 * 1024) {
                setError('壁纸不能超过 4MB')
                return
              }
              const reader = new FileReader()
              reader.onload = () => onWallpaperChange(String(reader.result || ''))
              reader.readAsDataURL(f)
            }}
          />
          <div className="flex gap-1">
            <input
              value={wallUrl}
              onChange={(e) => setWallUrl(e.target.value)}
              placeholder={wallpaper ? '已设置壁纸（可粘贴新链接替换）' : '粘贴壁纸图片 URL（可选）'}
              aria-label="壁纸地址"
              className="min-w-0 flex-1 rounded-lg border border-[var(--ag-stroke2)] bg-[var(--ag-card)] px-2 py-1 text-[11px] text-[var(--ag-text1)] placeholder:text-[var(--ag-text4)] focus:border-[var(--ag-accent)]/50 focus:outline-none"
            />
            <button
              type="button"
              onClick={() => {
                if (wallUrl.trim()) {
                  onWallpaperChange(wallUrl.trim())
                  setWallUrl('')
                }
              }}
              disabled={!wallUrl.trim()}
              className="shrink-0 rounded-lg bg-[var(--ag-text1)] px-2 py-1 text-[11px] text-[var(--ag-bg)] hover:opacity-85 disabled:opacity-40"
            >
              使用
            </button>
            <button
              type="button"
              onClick={() => onWallpaperChange('')}
              disabled={!wallpaper}
              className="shrink-0 rounded-lg border border-[var(--ag-stroke2)] px-2 py-1 text-[11px] text-[var(--ag-text3)] hover:text-[var(--ag-text1)] disabled:opacity-40"
            >
              清除
            </button>
          </div>
        </>
      )}
    </div>
  )
}

export default PetSettings
