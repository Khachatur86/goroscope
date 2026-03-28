/**
 * Goroscope Service Worker — caches static assets (JS/CSS/fonts) with a
 * stale-while-revalidate strategy. API calls are always network-only.
 */

const CACHE = "goroscope-v1";

// Only cache static asset extensions.
function isStaticAsset(url) {
  return /\.(js|css|woff2?|ttf|eot|svg|png|ico)(\?.*)?$/.test(new URL(url).pathname);
}

self.addEventListener("install", (event) => {
  // Activate immediately — skip waiting for old SW to finish.
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  // Evict old caches.
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))
      )
    )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  const url = event.request.url;

  // Always bypass cache for API requests and non-GET methods.
  if (event.request.method !== "GET" || url.includes("/api/")) {
    return;
  }

  if (!isStaticAsset(url)) {
    return;
  }

  // Stale-while-revalidate for static assets.
  event.respondWith(
    caches.open(CACHE).then(async (cache) => {
      const cached = await cache.match(event.request);
      const networkFetch = fetch(event.request).then((res) => {
        if (res.ok) cache.put(event.request, res.clone());
        return res;
      });
      return cached ?? networkFetch;
    })
  );
});
