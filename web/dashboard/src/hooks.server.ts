// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { COOKIE, CURSOR_COOKIE, open } from '$lib/server/session';
import { guardSession } from '$lib/server/guard';
import { LOCALE_COOKIE } from '@bagel/kit/i18n';
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
import { rumTransform } from '@bagel/kit/server/rum';
import { ValkeyRateLimiter, warmRateLimiter } from '@bagel/kit/server/rate-limit';
import { warmSessionRevocation } from '@bagel/kit/server/session-revocation';
import { logger } from '@bagel/kit/server/logger';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';
import { purgeEdgeByTag } from '$lib/server/edge-purge';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
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
  '/(public)/[user]': [300, 3600],
  '/(public)/user/[channel]': [300, 3600]
};

export const DEPLOY_CACHE_TAG = 'dashboard-edge-shell';

const DEPLOY_PURGE_DELAY_MS = 30_000;

// Relies on console-dashboard rolling with maxSurge 0: the last new pod's purge lands after every old pod is gone.
function scheduleDeployPurge(): void {
  if (!process.env.CF_ZONE_ID || !process.env.CF_CACHE_PURGE_TOKEN) return;
  setTimeout(purgeDeployShell, DEPLOY_PURGE_DELAY_MS).unref?.();
}

async function purgeDeployShell(): Promise<void> {
  if (!(await purgeEdgeByTag(DEPLOY_CACHE_TAG))) logger.error({ tag: DEPLOY_CACHE_TAG }, '[deploy-purge] edge purge failed');
}

type HookEvent = Parameters<Handle>[0]['event'];

function cacheableStatus(res: Response, event: HookEvent): boolean {
  return res.status === 200 || (res.status === 404 && !!event.locals.edgeCache404);
}

function cacheableRequest(event: HookEvent): boolean {
  return event.request.method === 'GET' || event.request.method === 'HEAD';
}

function cacheableResponse(res: Response, event: HookEvent): boolean {
  // Expired session cookies get a delete Set-Cookie with no session: caching it would replay to every visitor.
  if (res.headers.has('set-cookie')) return false;
  return cacheableStatus(res, event) && !!res.headers.get('content-type')?.includes('text/html');
}

function anonymousDefaultRender(event: HookEvent): boolean {
  if (event.locals.session) return false;
  if (event.cookies.get(LOCALE_COOKIE) || event.url.searchParams.has('lang')) return false;
  return event.cookies.get(CURSOR_COOKIE) !== '0';
}

// Cloudflare keys the cache on normalized Accept-Language only: cache renders whose locale comes from that header alone.
export function edgeCacheHeaders(event: HookEvent, res: Response): Record<string, string> | null {
  const ttl = EDGE_CACHE[event.route.id ?? ''];
  if (!ttl || !cacheableRequest(event)) return null;
  if (!cacheableResponse(res, event) || !anonymousDefaultRender(event)) return null;
  return {
    'Cache-Control': 'public, max-age=0',
    'CDN-Cache-Control': `max-age=${ttl[0]}, stale-while-revalidate=${ttl[1]}, stale-if-error=86400`,
    'Cache-Tag': DEPLOY_CACHE_TAG
  };
}

export function applyEdgeCache(event: HookEvent, res: Response): void {
  const headers = edgeCacheHeaders(event, res);
  if (!headers) return;
  for (const [name, value] of Object.entries(headers)) res.headers.set(name, value);
  res.headers.append('Vary', 'Accept-Language');
}

const PERMISSIONS_POLICY =
  'camera=(), microphone=(), geolocation=(), payment=(), join-ad-interest-group=(), run-ad-auction=(), shared-storage=(), browsing-topics=()';

export const handle: Handle = async ({ event, resolve }) => {
  event.locals.session = openSessionCookie(event, COOKIE, open);

  const limited = await enforceRateLimit(event);
  if (limited) {
    return limited;
  }

  if (event.locals.session) {
    event.locals.session = await guardSession(event, event.locals.session);
  }

  const locale = await resolveLocale(event, !!event.locals.session?.impersonator_id);
  event.locals.locale = locale;
  event.locals.cursorEnabled = event.cookies.get(CURSOR_COOKIE) !== '0';
  tagTransaction(newrelic, event, event.locals.session);

  const rum = rumTransform();
  let langPatched = false;
  const res = await resolve(event, {
    preload: preloadStrategy,
    transformPageChunk: (opts) => {
      let html = rum(opts);
      if (!langPatched && html.includes('<html')) {
        html = html.replace('lang="en"', `lang="${locale}"`);
        langPatched = true;
      }
      return html;
    }
  });

  harden(res, PERMISSIONS_POLICY);
  applyEdgeCache(event, res);
  return res;
};

export const handleError: HandleServerError = ({ error, event, status }) => {
  noticeServerError(newrelic, error, event, status);
  return { message: 'Internal Error' };
};
