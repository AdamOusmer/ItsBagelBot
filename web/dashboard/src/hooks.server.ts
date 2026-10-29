// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import type { Handle, HandleServerError, ResolveOptions, ServerInit } from '@sveltejs/kit/hooks';
import newrelic from 'newrelic';
import { COOKIE, CURSOR_COOKIE, open } from '#lib/server/session';
import { guardSession } from '#lib/server/guard';
import { LOCALE_COOKIE, type Locale } from '@bagel/kit/i18n';
import { warm as warmValkey } from '@bagel/kit/server/valkey-store';
import { initConsoleRuntime } from '@bagel/kit/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  resolveLocale,
  tagTransaction
} from '@bagel/kit/server/hooks';
import { ValkeyRateLimiter, warmRateLimiter } from '@bagel/kit/server/rate-limit';
import { warmSessionRevocation } from '@bagel/kit/server/session-revocation';
import { logger } from '@bagel/kit/server/logger';
import { startInvalidationListener } from '#lib/server/services';
import { assertConfigSane } from '#lib/server/config-sanity';
import { purgeEdgeByTag } from '#lib/server/edge-purge';
import { enumerableChannelQuery } from '#lib/server/channel-urls';
import { preloadCatalog, preloadSlices } from '#lib/server/catalog-preload';
import { countView, setAnalyticsApp } from '@bagel/kit/server/analytics';

setAnalyticsApp('dashboard');

// process.env, not $app/env/private: it only exposes variables declared in src/env.ts.
export const init: ServerInit = async () => {
  initConsoleRuntime(process.env, assertConfigSane);

  warmValkey();
  warmRateLimiter();
  warmSessionRevocation();

  startInvalidationListener();
  scheduleDeployPurge();
};

// Keyed by session user id only: client IPs must never be written to Valkey.
const authLimiter = new ValkeyRateLimiter({ name: 'auth', capacity: 10, refillPerSec: 10 / 60 });
const writeLimiter = new ValkeyRateLimiter({ name: 'write', capacity: 30, refillPerSec: 0.5 });
const readLimiter = new ValkeyRateLimiter({ name: 'read', capacity: 60, refillPerSec: 2 });

function pickLimiter(pathname: string, method: string): ValkeyRateLimiter {
  if (pathname.startsWith('/auth/') || pathname.startsWith('/delegate/')) return authLimiter;
  if (method !== 'GET' && method !== 'HEAD') return writeLimiter;
  return readLimiter;
}

// Anonymous requests pass through: kubelet probes and the status page must never be limited.
async function enforceRateLimit(event: Parameters<Handle>[0]['event']): Promise<Response | null> {
  const userId = event.locals.session?.user_id;
  if (!userId) {
    return null;
  }
  const decision = await pickLimiter(event.url.pathname, event.request.method).check(`u:${userId}`);
  if (decision.allowed) {
    return null;
  }
  newrelic.addCustomAttributes({ 'ratelimit.limited': true, 'route.id': event.route.id ?? 'unmatched' });
  return new Response('Too many requests', {
    status: 429,
    headers: {
      'Retry-After': String(decision.retryAfterSec),
      'Cache-Control': 'no-store',
      'Content-Type': 'text/plain; charset=utf-8'
    }
  });
}

export const EDGE_CACHE: Record<string, readonly [edgeTtlSec: number, swrSec: number]> = {
  '/(public)/login': [600, 86_400],
  '/(public)/stats': [30, 300],
  '/(public)/[user]': [30, 30],
  '/(public)/queue/[user]': [5, 5],
  '/(public)/user/[channel]': [3600, 3600]
};

const SHORT_EDGE_TTL = [60, 60] as const;

export const DEPLOY_CACHE_TAG = 'dashboard-edge-shell';

// Must land after the last old pod exits: readiness + minReadySeconds 10 + terminationGracePeriodSeconds 45 (console-dashboard.yaml).
const DEPLOY_PURGE_DELAY_MS = 90_000;

function scheduleDeployPurge(): void {
  if (!process.env.CF_ZONE_ID || !process.env.CF_CACHE_PURGE_TOKEN) return;
  setTimeout(purgeDeployShell, DEPLOY_PURGE_DELAY_MS).unref?.();
}

async function purgeDeployShell(): Promise<void> {
  if (!(await purgeEdgeByTag(DEPLOY_CACHE_TAG))) logger.error({ tag: DEPLOY_CACHE_TAG }, '[deploy-purge] edge purge failed');
}

type HookEvent = Parameters<Handle>[0]['event'];

type EdgeTtl = readonly [edgeTtlSec: number, swrSec: number];

function edgeTtl(event: HookEvent): EdgeTtl | null {
  const routeTtl = EDGE_CACHE[event.route.id ?? ''];
  if (!routeTtl) return null;
  switch (event.locals.edgeOutcome) {
    case 'skip':
      return null;
    case 'missing':
      return SHORT_EDGE_TTL;
    default:
      return routeTtl;
  }
}

function cacheableStatus(res: Response, event: HookEvent): boolean {
  if (res.status === 200) return true;
  const outcome = event.locals.edgeOutcome;
  return res.status === 404 && (outcome === 'hidden' || outcome === 'missing');
}

// Data responses are 200 even when a load failed or redirected: only a load that recorded its outcome is shareable.
function cacheableBody(res: Response, event: HookEvent): boolean {
  const type = res.headers.get('content-type') ?? '';
  if (!event.isDataRequest) return type.includes('text/html');
  return !!event.locals.edgeOutcome && type.includes('application/json');
}

function urlPurgeable(event: HookEvent): boolean {
  return enumerableChannelQuery(event.route.id ?? '', new URL(event.request.url).search, event.isDataRequest);
}

function cacheableRequest(event: HookEvent): boolean {
  const method = event.request.method;
  return (method === 'GET' || method === 'HEAD') && urlPurgeable(event);
}

function cacheableResponse(res: Response, event: HookEvent): boolean {
  // Expired session cookies get a delete Set-Cookie with no session: caching it would replay to every visitor.
  if (res.headers.has('set-cookie')) return false;
  return cacheableStatus(res, event) && cacheableBody(res, event);
}

const EDGE_SHARED_LOCALES: readonly string[] = ['en', 'fr'];

function sharedCacheLocale(event: HookEvent): boolean {
  return EDGE_SHARED_LOCALES.includes(event.locals.locale);
}

function anonymousDefaultRender(event: HookEvent): boolean {
  if (event.locals.session || !sharedCacheLocale(event)) return false;
  if (event.cookies.get(LOCALE_COOKIE) || event.url.searchParams.has('lang')) return false;
  return event.cookies.get(CURSOR_COOKIE) !== '0';
}

// Cloudflare keys the cache on normalized Accept-Language only: cache renders whose locale comes from that header alone.
export function edgeCacheHeaders(event: HookEvent, res: Response): Record<string, string> | null {
  const ttl = edgeTtl(event);
  if (!ttl || !cacheableRequest(event)) return null;
  if (!cacheableResponse(res, event) || !anonymousDefaultRender(event)) return null;
  return {
    'Cache-Control': 'public, max-age=0',
    'CDN-Cache-Control': `max-age=${ttl[0]}, stale-while-revalidate=${ttl[1]}, stale-if-error=86400`,
    'Cache-Tag': [DEPLOY_CACHE_TAG, event.locals.edgeCacheTag].filter(Boolean).join(',')
  };
}

export function applyEdgeCache(event: HookEvent, res: Response): void {
  const headers = edgeCacheHeaders(event, res);
  if (!headers) return;
  for (const [name, value] of Object.entries(headers)) res.headers.set(name, value);
  res.headers.append('Vary', 'Accept-Language');
}

// The guard starts alongside the limiter; a 429 answers without waiting for it, and wins over its redirect.
async function admitSession(event: HookEvent): Promise<Response | null> {
  const session = event.locals.session;
  if (!session) return null;
  const guarded = guardSession(event, session);
  guarded.catch(() => {});
  const limited = await enforceRateLimit(event);
  if (limited) return limited;
  event.locals.session = await guarded;
  return null;
}

const OFFLINE_ROUTE_ROOT = '/(app)/discord';

function offlineRoute(event: HookEvent): boolean {
  const id = event.route.id ?? '';
  return id === OFFLINE_ROUTE_ROOT || id.startsWith(OFFLINE_ROUTE_ROOT + '/');
}

const PERMISSIONS_POLICY =
  'camera=(), microphone=(), geolocation=(), payment=(), join-ad-interest-group=(), run-ad-auction=(), shared-storage=(), browsing-topics=()';

async function prepareRequest(event: HookEvent): Promise<void> {
  const locale = await resolveLocale(event, !!event.locals.session?.impersonator_id);
  event.locals.locale = locale;
  event.locals.cursorEnabled = event.cookies.get(CURSOR_COOKIE) !== '0';
  tagTransaction(newrelic, event, event.locals.session);
  countView(event);
  if (offlineRoute(event)) error(404, 'Not Found');
}

function pageChunkTransform(locale: Locale): NonNullable<ResolveOptions['transformPageChunk']> {
  let langPatched = false;
  return ({ html }) => {
    if (!langPatched && html.includes('<html')) {
      html = html.replace('lang="en"', `lang="${locale}"`);
      langPatched = true;
    }
    return preloadCatalog(html, locale);
  };
}

export const handle: Handle = async ({ event, resolve }) => {
  if (event.url.pathname === '/readyz') return resolve(event);

  event.locals.session = openSessionCookie(event, COOKIE, open);

  const limited = await admitSession(event);
  if (limited) {
    return limited;
  }

  await prepareRequest(event);
  const locale = event.locals.locale;
  const res = await resolve(event, { preload: preloadStrategy, transformPageChunk: pageChunkTransform(locale) });

  preloadSlices(res.headers, locale);
  harden(res, PERMISSIONS_POLICY);
  applyEdgeCache(event, res);
  return res;
};

export const handleError: HandleServerError = ({ kind, error, event }) => {
  noticeServerError(newrelic, error, event, kind === 'unknown' ? 500 : error.status);
  return { message: 'Internal Error' };
};
