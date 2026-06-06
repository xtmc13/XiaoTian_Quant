/**
 * Service Worker for XiaoTianQuant PWA
 *
 * Caching strategy:
 *   - Static assets (JS/CSS/HTML): Cache-first, stale-while-revalidate
 *   - API responses: Network-first, fallback to cache
 *   - Images: Cache-first with 30-day TTL
 *   - WebSocket upgrades: Pass-through (no caching)
 */

const CACHE_VERSION = 'v3'
const STATIC_CACHE = `xt-static-${CACHE_VERSION}`
const API_CACHE = `xt-api-${CACHE_VERSION}`
const IMG_CACHE = `xt-img-${CACHE_VERSION}`

const STATIC_ASSETS = [
  '/',
  '/index.html',
  '/assets/index.css',
]

const MAX_API_ENTRIES = 100
const MAX_IMG_ENTRIES = 200

/* ── Install: pre-cache shell ── */
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(STATIC_CACHE)
      .then((cache) => cache.addAll(STATIC_ASSETS))
      .then(() => self.skipWaiting())
  )
})

/* ── Activate: clean old caches ── */
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((key) => key.startsWith('xt-') && !key.includes(CACHE_VERSION))
          .map((key) => caches.delete(key))
      )
    ).then(() => self.clients.claim())
  )
})

/* ── Fetch: routing by request type ── */
self.addEventListener('fetch', (event) => {
  const { request } = event
  const url = new URL(request.url)

  // Skip non-GET requests
  if (request.method !== 'GET') return

  // Skip WebSocket upgrades
  if (request.headers.get('Upgrade') === 'websocket') return

  // Skip browser extensions / chrome-extension
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return

  // Route by path / destination
  if (isAPI(request, url)) {
    event.respondWith(apiStrategy(request))
  } else if (isImage(request)) {
    event.respondWith(imageStrategy(request))
  } else if (isStatic(request, url)) {
    event.respondWith(staticStrategy(request))
  }
})

/* ── Routing helpers ── */

function isAPI(request, url) {
  return url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws/')
}

function isImage(request) {
  return request.destination === 'image'
}

function isStatic(request, url) {
  return (
    request.destination === 'script' ||
    request.destination === 'style' ||
    request.destination === 'document' ||
    url.pathname.endsWith('.js') ||
    url.pathname.endsWith('.css') ||
    url.pathname.endsWith('.html') ||
    url.pathname.endsWith('.json') ||
    url.pathname.endsWith('.woff2')
  )
}

/* ── Strategies ── */

/** Cache-first for static assets: fast load, background update. */
async function staticStrategy(request) {
  const cache = await caches.open(STATIC_CACHE)
  const cached = await cache.match(request)

  if (cached) {
    // Background revalidate
    fetch(request)
      .then((response) => {
        if (response.ok) cache.put(request, response.clone())
      })
      .catch(() => { /* ignore */ })
    return cached
  }

  const response = await fetch(request)
  if (response.ok) {
    cache.put(request, response.clone())
  }
  return response
}

/** Network-first for API: always fresh, cache as fallback. */
async function apiStrategy(request) {
  const cache = await caches.open(API_CACHE)

  try {
    const networkResponse = await fetch(request)
    if (networkResponse.ok) {
      cache.put(request, networkResponse.clone())
      await trimCache(cache, MAX_API_ENTRIES)
    }
    return networkResponse
  } catch (err) {
    const cached = await cache.match(request)
    if (cached) return cached
    throw err
  }
}

/** Cache-first for images with TTL eviction. */
async function imageStrategy(request) {
  const cache = await caches.open(IMG_CACHE)
  const cached = await cache.match(request)

  if (cached) {
    const age = Date.now() - (cached.headers.get('sw-cached-at') || 0)
    if (age < 30 * 24 * 60 * 60 * 1000) return cached // < 30 days
  }

  const response = await fetch(request)
  if (response.ok) {
    const cloned = response.clone()
    const headers = new Headers(cloned.headers)
    headers.set('sw-cached-at', String(Date.now()))
    const wrapped = new Response(cloned.body, { status: cloned.status, statusText: cloned.statusText, headers })
    cache.put(request, wrapped)
    await trimCache(cache, MAX_IMG_ENTRIES)
  }
  return response
}

/** Trim cache to max entries (LRU by access time). */
async function trimCache(cache, max) {
  const keys = await cache.keys()
  if (keys.length <= max) return
  // Remove oldest half when over limit
  const toDelete = keys.slice(0, Math.floor(keys.length / 2))
  await Promise.all(toDelete.map((req) => cache.delete(req)))
}

/* ── Message handling (from main thread) ── */

self.addEventListener('message', (event) => {
  if (event.data === 'SKIP_WAITING') {
    self.skipWaiting()
  }
})
