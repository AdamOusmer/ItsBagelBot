/// <reference types="@sveltejs/kit" />
import { build, files, version } from '$service-worker';

// SvelteKit types the worker global as a plain WorkerGlobalScope; narrow it to
// the service-worker scope so skipWaiting()/clients/location are typed.
const sw = self as unknown as ServiceWorkerGlobalScope;

// `version` is the commit SHA (svelte.config.js version.name), so the cache is
// deploy-scoped and activate() can drop every prior generation.
const CACHE = `bagel-dashboard-${version}`;

// Large or crawler-only static files with no offline value; kept out of the
// precache so we don't push megabytes of imagery to every install.
const PRECACHE_EXCLUDE = new Set([
  '/og-image.png',
  '/logo.png',
  '/premium-logo.png',
  '/robots.txt'
]);

// Content-hashed build output (always safe to cache) plus the static files we
// actually want offline: icons, the web manifest, and offline.html.
const PRECACHE = [...build, ...files.filter((file) => !PRECACHE_EXCLUDE.has(file))];
const PRECACHE_SET = new Set(PRECACHE);

// Navigation fallback shown when an SSR page can't be reached. It lives in
// static/ so SvelteKit lists it in `files` and it is part of PRECACHE above.
const OFFLINE_URL = '/offline.html';

async function precache(): Promise<void> {
  const cache = await caches.open(CACHE);
  await cache.addAll(PRECACHE);
}

sw.addEventListener('install', (event) => {
  event.waitUntil(precache().then(() => sw.skipWaiting()));
});

async function dropOldCaches(): Promise<void> {
  const keys = await caches.keys();
  await Promise.all(keys.filter((key) => key !== CACHE).map((key) => caches.delete(key)));
}

sw.addEventListener('activate', (event) => {
  event.waitUntil(dropOldCaches().then(() => sw.clients.claim()));
});

function isPrecached(pathname: string): boolean {
  return PRECACHE_SET.has(pathname);
}

// Cache-first for precached assets: under a fixed version they never change, so
// a hit is authoritative and the network is only touched on a miss.
async function serveFromCache(request: Request): Promise<Response> {
  const cached = await caches.match(request);
  return cached ?? fetch(request);
}

// Online navigations always hit the network (SSR carries private, no-store
// data we must never cache); only a network failure falls back to the shell.
async function handleNavigation(request: Request): Promise<Response> {
  try {
    return await fetch(request);
  } catch {
    const offline = await caches.match(OFFLINE_URL);
    return offline ?? Response.error();
  }
}

// Requests the worker refuses to touch: non-GET, ranged/streamed media, and
// cross-origin. Bypassed requests behave exactly as if no worker existed.
function isBypassed(request: Request, url: URL): boolean {
  if (request.method !== 'GET') return true;
  if (request.headers.has('range')) return true;
  return url.origin !== sw.location.origin;
}

// Returns a Response promise to serve, or undefined to let the request pass
// through untouched (SSR HTML, __data.json and API calls are never cached).
function route(request: Request, url: URL): Promise<Response> | undefined {
  if (isPrecached(url.pathname)) return serveFromCache(request);
  if (request.mode === 'navigate') return handleNavigation(request);
  return undefined;
}

sw.addEventListener('fetch', (event) => {
  const { request } = event;
  const url = new URL(request.url);
  if (isBypassed(request, url)) return;

  const response = route(request, url);
  if (response) event.respondWith(response);
});
