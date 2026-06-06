/**
 * PWA utilities — Service Worker registration, install prompt, update detection.
 */

interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

let deferredPrompt: BeforeInstallPromptEvent | null = null

/** Register the Service Worker. */
export function registerSW() {
  if (!('serviceWorker' in navigator)) return

  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js')
      .then((registration) => {
        console.log('[PWA] SW registered:', registration.scope)

        // Detect updates
        registration.addEventListener('updatefound', () => {
          const newWorker = registration.installing
          if (!newWorker) return

          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              // New version available — notify user
              showUpdateNotification(newWorker)
            }
          })
        })
      })
      .catch((err) => {
        console.error('[PWA] SW registration failed:', err)
      })
  })
}

/** Listen for the beforeinstallprompt event (desktop/mobile install). */
export function listenInstallPrompt() {
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    deferredPrompt = e as BeforeInstallPromptEvent
    console.log('[PWA] Install prompt deferred')
  })
}

/** Trigger the install prompt (call from a button click). */
export async function promptInstall(): Promise<boolean> {
  if (!deferredPrompt) return false
  deferredPrompt.prompt()
  const { outcome } = await deferredPrompt.userChoice
  deferredPrompt = null
  return outcome === 'accepted'
}

/** Check if the app is already installed (standalone display mode). */
export function isStandalone(): boolean {
  return (
    window.matchMedia('(display-mode: standalone)').matches ||
    // @ts-expect-error iOS specific
    window.navigator.standalone === true
  )
}

/** Check if the app can be installed (prompt is available). */
export function canInstall(): boolean {
  return deferredPrompt !== null
}

/** Show a toast notification when a new version is available. */
function showUpdateNotification(worker: ServiceWorker) {
  // Dispatch a custom event that the app can listen to
  window.dispatchEvent(
    new CustomEvent('sw-update', {
      detail: {
        accept: () => worker.postMessage('SKIP_WAITING'),
      },
    })
  )
}

/** Unregister all Service Workers (useful for debugging). */
export async function unregisterSW() {
  if (!('serviceWorker' in navigator)) return
  const registrations = await navigator.serviceWorker.getRegistrations()
  await Promise.all(registrations.map((r) => r.unregister()))
  console.log('[PWA] All SW unregistered')
}
