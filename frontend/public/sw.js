// Minimal network-first service worker (D-004: this is NOT an offline client —
// no API caching, no write queue; mutations always require the server).
//
// Cached: the app shell (index.html, hashed JS/CSS) and /assets/ images.
// Never touched: anything under /api/ — API reads must stay fresh because they
// drive marking actions, and mutations must never be cached or replayed.

const CACHE = 'sofalog-shell-v3'

// Relative to the service worker's own location, so a sub-path deployment works.
const PRECACHE = [
  './',
  './offline.html',
  './manifest.json',
  './icons/logo.svg',
  './icons/icon-192.png',
  './icons/icon-512.png',
  './icons/maskable-512.png',
  './icons/apple-touch-icon.png',
]

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE)
      .then((cache) => cache.addAll(PRECACHE))
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (event) => {
  const { request } = event
  const url = new URL(request.url)

  // Hands off the API entirely, and anything cross-origin or non-GET.
  if (request.method !== 'GET') return
  if (url.origin !== self.location.origin) return
  if (url.pathname.includes('/api/')) return

  event.respondWith(
    fetch(request)
      .then((response) => {
        if (response.ok) {
          const copy = response.clone()
          caches.open(CACHE).then((cache) => cache.put(request, copy))
        }
        return response
      })
      .catch(async () => {
        const cached = await caches.match(request)
        if (cached) return cached
        if (request.mode === 'navigate') {
          const offline = await caches.match('./offline.html')
          if (offline) return offline
        }
        return Response.error()
      }),
  )
})
