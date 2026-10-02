// ── 默认桌宠形象：小天鲸（蓝白小鲸鱼，React SVG 组件） ──
export function DefaultPet({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 100 100" aria-hidden role="img">
      <defs>
        <linearGradient id="xt-pet-body" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#8fd0ff" />
          <stop offset="100%" stopColor="#5aa9f2" />
        </linearGradient>
      </defs>
      {/* 尾巴 */}
      <path d="M82 44 Q94 36 92 26 Q84 32 78 36 Z" fill="#5aa9f2" />
      <path d="M82 52 Q96 54 96 64 Q88 60 80 58 Z" fill="#5aa9f2" />
      {/* 身体 */}
      <ellipse cx="48" cy="52" rx="36" ry="28" fill="url(#xt-pet-body)" />
      {/* 肚皮 */}
      <ellipse cx="46" cy="62" rx="24" ry="14" fill="#eaf6ff" />
      {/* 眼睛 */}
      <circle cx="38" cy="46" r="5.5" fill="#1d2b3f" />
      <circle cx="39.5" cy="44.5" r="2" fill="#fff" />
      <circle cx="58" cy="46" r="5.5" fill="#1d2b3f" />
      <circle cx="59.5" cy="44.5" r="2" fill="#fff" />
      {/* 腮红 */}
      <ellipse cx="30" cy="55" rx="4.5" ry="2.5" fill="#ffb3c1" opacity="0.8" />
      <ellipse cx="66" cy="55" rx="4.5" ry="2.5" fill="#ffb3c1" opacity="0.8" />
      {/* 嘴 */}
      <path d="M44 55 Q48 59 52 55" fill="none" stroke="#1d2b3f" strokeWidth="2.2" strokeLinecap="round" />
      {/* 鱼鳍小手 */}
      <ellipse cx="22" cy="56" rx="7" ry="4.5" fill="#5aa9f2" transform="rotate(-25 22 56)" />
    </svg>
  )
}

export default DefaultPet
