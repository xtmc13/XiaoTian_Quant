import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { Settings2 } from 'lucide-react'
import { useAuthStore } from '@/stores/authStore'
import { cn } from '@/lib/utils'
import { AgentChatPanel } from './AgentChatPanel'
import { DefaultPet } from './pet/DefaultPet'
import {
  PetSettings,
  loadPetConfig,
  loadPetPos,
  petShadowClass,
  savePetConfig,
  savePetPos,
  type PetConfig,
  type PetPosition,
} from './pet/PetSettings'
import './pet/pets.css'

/** 展开/收起动画时长（ms），与 clip-path 过渡配套 */
const EXPAND_MS = 450
const DRAG_THRESHOLD = 6

// ── 桌宠：默认右下角；可自由拖动；点击开/关全屏助手；形象/尺寸可自定义 ──
export function DesktopPet() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
  const location = useLocation()
  const [expanded, setExpanded] = useState(false)
  // 首次展开后保持挂载：收起动画期间不卸载，且保留会话状态与后台流式
  const [rendered, setRendered] = useState(false)
  const [unread, setUnread] = useState(0)
  const [pet, setPet] = useState<PetConfig>(loadPetConfig)
  const [pos, setPos] = useState<PetPosition | null>(loadPetPos)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const dragRef = useRef<{ startX: number; startY: number; baseX: number; baseY: number; moved: boolean } | null>(null)
  const suppressClickRef = useRef(false)
  const [dragging, setDragging] = useState(false)
  const originRef = useRef({ x: 0, y: 0 })
  const petRef = useRef<HTMLDivElement>(null)

  // Esc 收起全屏
  useEffect(() => {
    if (!expanded) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setExpanded(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [expanded])

  // 未登录或登录页不渲染
  if (!isAuthenticated || location.pathname === '/login') return null

  const toggle = () => {
    if (expanded) {
      setExpanded(false)
      return
    }
    const rect = petRef.current?.getBoundingClientRect()
    if (rect) originRef.current = { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }
    setUnread(0)
    setSettingsOpen(false)
    setRendered(true)
    // 等 closed 态（clip 半径 0）先提交，再触发展开过渡
    requestAnimationFrame(() => requestAnimationFrame(() => setExpanded(true)))
  }

  // ── 拖动（Pointer Events，兼容触屏/鼠标） ──
  const defaultPos = (): PetPosition => ({
    x: Math.max(8, window.innerWidth - pet.size - 24),
    y: Math.max(8, window.innerHeight - pet.size - 96),
  })

  const clampPos = (p: PetPosition): PetPosition => ({
    x: Math.min(Math.max(0, p.x), Math.max(0, window.innerWidth - pet.size)),
    y: Math.min(Math.max(0, p.y), Math.max(0, window.innerHeight - pet.size)),
  })

  const onPointerDown = (e: React.PointerEvent) => {
    if (settingsOpen) return
    const start = pos ?? defaultPos()
    dragRef.current = { startX: e.clientX, startY: e.clientY, baseX: start.x, baseY: start.y, moved: false }
    ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
  }

  const onPointerMove = (e: React.PointerEvent) => {
    const d = dragRef.current
    if (!d) return
    const dx = e.clientX - d.startX
    const dy = e.clientY - d.startY
    if (!d.moved && Math.hypot(dx, dy) > DRAG_THRESHOLD) {
      d.moved = true
      setDragging(true)
    }
    if (d.moved) {
      setPos(clampPos({ x: d.baseX + dx, y: d.baseY + dy }))
    }
  }

  const onPointerUp = () => {
    const d = dragRef.current
    dragRef.current = null
    if (!d) return
    setDragging(false)
    if (d.moved) {
      // 拖动结束：抑制随后的 click，避免误触发展开
      suppressClickRef.current = true
      setPos((p) => {
        savePetPos(p)
        return p
      })
    }
  }

  const onClick = () => {
    if (suppressClickRef.current) {
      suppressClickRef.current = false
      return
    }
    toggle()
  }

  const onPetChange = (cfg: PetConfig) => {
    setPet(cfg)
    savePetConfig(cfg)
  }

  const px = pos?.x ?? null
  const py = pos?.y ?? null
  const origin = originRef.current

  return (
    <>
      {/* 桌宠本体 */}
      <div
        ref={petRef}
        role="button"
        tabIndex={0}
        aria-label={expanded ? '收起小天助手' : '打开小天助手'}
        aria-expanded={expanded}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onClick={onClick}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            toggle()
          }
        }}
        className={cn('xt-pet fixed z-[90] cursor-grab outline-none', dragging && 'xt-pet-dragging')}
        style={{
          left: px ?? undefined,
          top: py ?? undefined,
          right: px == null ? 24 : undefined,
          bottom: px == null ? 96 : undefined,
          width: pet.size,
          height: pet.size,
        }}
      >
        <span className={petShadowClass(pet.anim)} style={{ opacity: dragging ? 0.15 : undefined }} />
        <span
          className={cn('block h-full w-full', pet.anim && 'xt-pet-anim', unread > 0 && !expanded && 'xt-pet-excited')}
        >
          {pet.image ? (
            <img
              src={pet.image}
              alt=""
              draggable={false}
              className="h-full w-full rounded-2xl object-contain drop-shadow-lg"
            />
          ) : (
            <DefaultPet size={pet.size} />
          )}
        </span>
        {unread > 0 && !expanded && (
          <span
            aria-label={`${unread} 条未读消息`}
            className="absolute -top-1 -right-1 flex h-5 min-w-[20px] items-center justify-center rounded-full bg-[#f5222d] px-1 text-[10px] font-bold text-white ring-2 ring-white"
          >
            {unread > 9 ? '9+' : unread}
          </span>
        )}
        {/* 设置入口：hover 显现 */}
        <button
          type="button"
          title="桌宠设置"
          aria-label="桌宠设置"
          onPointerDown={(e) => e.stopPropagation()}
          onClick={(e) => {
            e.stopPropagation()
            setSettingsOpen((v) => !v)
          }}
          className={cn(
            'absolute -top-1.5 -left-1.5 rounded-full bg-[var(--ag-card)] p-1 text-[var(--ag-text3)] shadow-md transition-opacity hover:text-[var(--ag-text1)]',
            settingsOpen ? 'opacity-100' : 'opacity-0 focus:opacity-100 hover:opacity-100'
          )}
        >
          <Settings2 size={11} />
        </button>
        {settingsOpen && (
          <PetSettings
            config={pet}
            onChange={onPetChange}
            onResetPos={() => {
              savePetPos(null)
              setPos(null)
            }}
            onClose={() => setSettingsOpen(false)}
          />
        )}
      </div>

      {/* 全屏助手层（圆形扩散动画，桌宠始终在其上方可点击收起） */}
      {rendered && (
        <div
          aria-hidden={!expanded}
          className="fixed inset-0 z-[80]"
          style={{
            clipPath: expanded
              ? `circle(150% at ${origin.x}px ${origin.y}px)`
              : `circle(0px at ${origin.x}px ${origin.y}px)`,
            transition: `clip-path ${EXPAND_MS}ms cubic-bezier(0.22, 1, 0.36, 1)`,
            pointerEvents: expanded ? 'auto' : 'none',
          }}
        >
          <AgentChatPanel
            variant="full"
            open
            onClose={() => setExpanded(false)}
            onUnread={() => {
              // 收起状态下完成的生成计未读（全屏展开时由界面本身呈现）
              if (!expanded) setUnread((n) => Math.min(n + 1, 99))
            }}
          />
        </div>
      )}
    </>
  )
}

export default DesktopPet
