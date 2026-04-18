// Thournaments service worker. Keeps the app shell snappy between navigations
// by caching /static/* and the htmx runtime. HTML pages go network-first so
// match state is never stale.

const CACHE = 'thournament-shell-v1';
const SHELL = [
    '/static/css/main.css',
    '/static/js/bracket.js',
    '/static/img/lanops.png',
    '/static/manifest.webmanifest',
    'https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js',
];

self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE).then((cache) =>
            // addAll is atomic — if any single URL fails the whole install fails.
            // Tolerate individual misses (the unpkg one in particular).
            Promise.allSettled(SHELL.map((u) => cache.add(u)))
        ).then(() => self.skipWaiting())
    );
});

self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys().then((keys) =>
            Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
        ).then(() => self.clients.claim())
    );
});

self.addEventListener('fetch', (event) => {
    const req = event.request;
    if (req.method !== 'GET') return;

    const url = new URL(req.url);

    // Static assets → cache-first (versioned via ?v= from the server so stale
    // URLs fall off naturally; fresh URLs miss and refetch).
    if (url.pathname.startsWith('/static/') || url.hostname === 'unpkg.com') {
        event.respondWith(
            caches.match(req).then((hit) => hit || fetch(req).then((res) => {
                if (res.ok) {
                    const copy = res.clone();
                    caches.open(CACHE).then((c) => c.put(req, copy)).catch(() => {});
                }
                return res;
            }))
        );
        return;
    }

    // Everything else (HTML, POSTs, SSE) → network as usual. Don't cache
    // dynamic pages — match state changes constantly.
});
