// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { COOKIE, CURSOR_COOKIE, open } from '$lib/server/session';
import { guardSession } from '$lib/server/guard';
import { warm as warmValkey } from '@bagel/kit/server/valkey-store';
import { initConsoleRuntime } from '@bagel/kit/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  tagTransaction
} from '@bagel/kit/server/hooks';
import { rumTransform } from '@bagel/kit/server/rum';
import { ValkeyRateLimiter, warmRateLimiter } from '@bagel/kit/server/rate-limit';
import { warmSessionRevocation } from '@bagel/kit/server/session-revocation';
import { detectLocale, isLocale, LOCALE_COOKIE, ensureCatalog } from '@bagel/kit/i18n';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
export const init: ServerInit = async () => {
  initConsoleRuntime(process.env, assertConfigSane);

  warmValkey();
  warmRateLimiter();
  warmSessionRevocation();

  startInvalidationListener();
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

function resolveLocale(event: Parameters<Handle>[0]['event']): ReturnType<typeof detectLocale> {
  const queryLang = event.url.searchParams.get('lang');
  if (isLocale(queryLang)) {
    event.cookies.set(LOCALE_COOKIE, queryLang, { path: '/', maxAge: 31536000, secure: true, sameSite: 'lax' });
  }
  if (event.locals.session?.impersonator_id) {
    return 'en';
  }
  return detectLocale({
    cookie: queryLang || event.cookies.get(LOCALE_COOKIE),
    accept: event.request.headers.get('accept-language')
  });
}

const EDGE_CACHE: Record<string, readonly [edgeTtlSec: number, swrSec: number]> = {
  '/login': [600, 86_400],
  '/(public)/stats': [30, 300],
  '/(public)/[user]': [60, 300],
  '/user/[channel]': [60, 300]
};

function cacheableStatus(res: Response, event: Parameters<Handle>[0]['event']): boolean {
  return res.status === 200 || (res.status === 404 && !!event.locals.edgeCache404);
}

// Cloudflare's cache key ignores cookies and Accept-Language: cache only anonymous default renders.
export function edgeCacheControl(event: Parameters<Handle>[0]['event'], res: Response): string | null {
  const ttl = EDGE_CACHE[event.route.id ?? ''];
  if (!ttl) return null;
  if (event.request.method !== 'GET' && event.request.method !== 'HEAD') return null;
  if (!cacheableStatus(res, event) || !res.headers.get('content-type')?.includes('text/html')) return null;
  if (event.locals.session) return null;
  if (event.locals.locale !== 'en') return null;
  if (event.url.searchParams.has('lang')) return null;
  if (event.cookies.get(CURSOR_COOKIE) === '0') return null;
  return `public, max-age=0, s-maxage=${ttl[0]}, stale-while-revalidate=${ttl[1]}`;
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

  const locale = resolveLocale(event);
  event.locals.locale = locale;
  await ensureCatalog(event.locals.locale);
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
  const cacheControl = edgeCacheControl(event, res);
  if (cacheControl) res.headers.set('Cache-Control', cacheControl);
  return res;
};

export const handleError: HandleServerError = ({ error, event, status }) => {
  noticeServerError(newrelic, error, event, status);
  return { message: 'Internal Error' };
};
