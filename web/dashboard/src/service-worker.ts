// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/// <reference types="@sveltejs/kit" />
import { build, files, version } from '$service-worker';

const sw = self as unknown as ServiceWorkerGlobalScope;

const CACHE = `bagel-dashboard-${version}`;

const PRECACHE_EXCLUDE = new Set(['/og-image.png', '/logo.png', '/premium-logo.png']);

const PRECACHE = [...build, ...files.filter((file) => !PRECACHE_EXCLUDE.has(file))];
const PRECACHE_SET = new Set(PRECACHE);

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

async function serveFromCache(request: Request): Promise<Response> {
  const cached = await caches.match(request);
  return cached ?? fetch(request);
}

// Navigations always hit the network: SSR carries private, no-store data that must never be cached.
async function handleNavigation(request: Request): Promise<Response> {
  try {
    return await fetch(request);
  } catch {
    const offline = await caches.match(OFFLINE_URL);
    return offline ?? Response.error();
  }
}

function isBypassed(request: Request, url: URL): boolean {
  if (request.method !== 'GET') return true;
  if (request.headers.has('range')) return true;
  return url.origin !== sw.location.origin;
}

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
