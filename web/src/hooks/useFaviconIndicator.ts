import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { portfolioApi } from '@/lib/api'

const originalFavicon = document.querySelector('link[rel*="icon"]') as HTMLLinkElement | null

function setFavicon(text: string | null) {
  let link = document.querySelector('link[rel*="icon"]') as HTMLLinkElement | null
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  if (!text) {
    if (originalFavicon?.href) {
      link.href = originalFavicon.href
    }
    return
  }

  const canvas = document.createElement('canvas')
  canvas.width = 32
  canvas.height = 32
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  ctx.fillStyle = '#0a0a0a'
  ctx.fillRect(0, 0, 32, 32)

  ctx.beginPath()
  ctx.arc(24, 8, 7, 0, Math.PI * 2)
  ctx.fillStyle = '#CF304A'
  ctx.fill()

  ctx.fillStyle = '#ffffff'
  ctx.font = 'bold 9px sans-serif'
  ctx.textAlign = 'center'
  ctx.textBaseline = 'middle'
  ctx.fillText(text, 24, 8)

  link.href = canvas.toDataURL()
}

export function useFaviconIndicator(enabled = true) {
  const { data: positionsData } = useQuery({
    queryKey: ['portfolio-positions-favicon'],
    queryFn: () => portfolioApi.positions(),
    refetchInterval: 10000,
    enabled,
  })

  useEffect(() => {
    if (!enabled) return
    const positions = positionsData?.positions || []
    const openPositions = positions.filter((p) => p.quantity !== 0).length
    setFavicon(openPositions > 0 ? String(openPositions) : null)

    return () => {
      setFavicon(null)
    }
  }, [positionsData, enabled])
}
