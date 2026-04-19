// Minimal service worker for the PWA shell.
//
// Strategy:
//  - Pre-cache the app shell (static assets + offline fallback) on install.
//  - Network-first for HTML, falling back to the cache.
//  - Cache-first for static assets (/static/*).
//  - Never cache API responses — stale booking data is worse than no data.

const VERSION = "v1";
const SHELL_CACHE = `re-shell-${VERSION}`;
const RUNTIME_CACHE = `re-runtime-${VERSION}`;
const SHELL_ASSETS = [
	"/static/js/app.js",
	"/static/css/app.css",
	"/static/offline.html",
];

self.addEventListener("install", (event) => {
	event.waitUntil(
		caches.open(SHELL_CACHE).then((cache) => cache.addAll(SHELL_ASSETS))
	);
	self.skipWaiting();
});

self.addEventListener("activate", (event) => {
	event.waitUntil(
		caches.keys().then((keys) =>
			Promise.all(
				keys
					.filter((k) => !k.endsWith(VERSION))
					.map((k) => caches.delete(k))
			)
		).then(() => self.clients.claim())
	);
});

self.addEventListener("fetch", (event) => {
	const req = event.request;
	if (req.method !== "GET") return;

	const url = new URL(req.url);
	if (url.origin !== self.location.origin) return;

	// Never cache API / auth requests.
	if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/auth/")) {
		return;
	}

	// Static assets: cache-first.
	if (url.pathname.startsWith("/static/")) {
		event.respondWith(cacheFirst(req));
		return;
	}

	// Everything else (HTML pages): network-first with offline fallback.
	event.respondWith(networkFirst(req));
});

async function cacheFirst(req) {
	const cache = await caches.open(SHELL_CACHE);
	const cached = await cache.match(req);
	if (cached) return cached;
	const resp = await fetch(req);
	if (resp.ok) cache.put(req, resp.clone());
	return resp;
}

async function networkFirst(req) {
	const cache = await caches.open(RUNTIME_CACHE);
	try {
		const resp = await fetch(req);
		if (resp.ok) cache.put(req, resp.clone());
		return resp;
	} catch (_) {
		const cached = await cache.match(req);
		if (cached) return cached;
		const shell = await caches.open(SHELL_CACHE);
		return (await shell.match("/static/offline.html")) || new Response("Offline", { status: 503 });
	}
}
