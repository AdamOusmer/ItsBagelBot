// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { COOKIE, CURSOR_COOKIE, open } from '$lib/server/session';
import { guardSession } from '$lib/server/guard';
import { warm as warmValkey } from '@bagel/shared/server/valkey-store';
import { initConsoleRuntime } from '@bagel/shared/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  tagTransaction
} from '@bagel/shared/server/hooks';
import { rumTransform } from '@bagel/shared/server/rum';
import { ValkeyRateLimiter, warmRateLimiter } from '@bagel/shared/server/rate-limit';
import { warmSessionRevocation } from '@bagel/shared/server/session-revocation';
import { detectLocale, isLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';

// Framework-native one-time boot. SvelteKit calls init() once before the first
// request; all boot side effects live here instead of at module-eval.
//
// Boot config reads process.env, NOT $env/dynamic/private: init() runs under the
// server entry's top-level `await server.init()`, so reading the dynamic-env
// proxy here deadlocks that await (unsettled top-level await -> exit 13). In
// adapter-node process.env carries the same Doppler-injected runtime values, and
// request-time code (session, oauth, rpc) keeps using $env/dynamic/private.
export const init: ServerInit = async () => {
  initConsoleRuntime(process.env, assertConfigSane);

  // Pre-connect the Valkey read pool and rate-limit write client so the first
  // request hits warm connections instead of paying the cold connect on the
  // hot path.
  warmValkey();
  warmRateLimiter();
  warmSessionRevocation();

  // Subscribe to the cache-invalidation bus so writes in Go services push-drop
  // the right keys without waiting on TTL expiry.
  startInvalidationListener();
};

// Fleet-wide rate limits: the buckets live in Valkey (Sentinel master), so
// every pod enforces the same global budget; on any Valkey failure each tier
// degrades to a per-pod bucket with the same tuning (see rate-limit.ts).
// Three tiers, tightest wins by route/method:
//   * auth   — /auth/* + /delegate/*: OAuth redirects/callbacks and session
//     escalation. Brute-force target, humans hit it a handful of times.
//   * write  — any non-GET/HEAD elsewhere: form actions and API mutations.
//     Clicking around settings is bursty, so allow a real burst but a modest
//     sustained rate (the Go batchers coalesce anyway).
//   * read   — everything else: page loads, __data.json, SSE connects.
// Keyed by session user id only: client IPs are never written to Valkey
// (hard policy). Anonymous traffic skips these buckets entirely and is
// limited per IP by traefik's in-memory rateLimit middlewares on the public
// hostnames (console-dashboard.yaml), which also cover the unauthenticated
// brute-force surface on /auth/*.
const authLimiter = new ValkeyRateLimiter({ name: 'auth', capacity: 10, refillPerSec: 10 / 60 });
const writeLimiter = new ValkeyRateLimiter({ name: 'write', capacity: 30, refillPerSec: 0.5 });
const readLimiter = new ValkeyRateLimiter({ name: 'read', capacity: 60, refillPerSec: 2 });

function pickLimiter(pathname: string, method: string): ValkeyRateLimiter {
  if (pathname.startsWith('/auth/') || pathname.startsWith('/delegate/')) return authLimiter;
  if (method !== 'GET' && method !== 'HEAD') return writeLimiter;
  return readLimiter;
}

// enforceRateLimit checks the request against its tier's fleet-wide bucket,
// keyed by session user id (stable across CGNAT/mobile IP churn). Anonymous
// requests pass straight through: kubelet probes and the status page must
// never be limited, and everything else unauthenticated is traefik's job.
// It returns a 429 Response when the budget is spent, else null to proceed.
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

// resolveLocale resolves the UI locale once per request: a valid ?lang override
// wins (and is pinned to the switcher cookie), else the cookie, else the
// browser's Accept-Language, else English. Admin view-as sessions always render
// in English — their language control edits the target account preference, not
// the administrative UI itself.
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

const PERMISSIONS_POLICY =
  'camera=(), microphone=(), geolocation=(), payment=(), join-ad-interest-group=(), run-ad-auction=(), shared-storage=(), browsing-topics=()';

// Session + account gates + the security headers SvelteKit's CSP config does
// not own.
export const handle: Handle = async ({ event, resolve }) => {
  event.locals.session = openSessionCookie(event, COOKIE, open);

  const limited = await enforceRateLimit(event);
  if (limited) {
    return limited;
  }

  // Account gates (ban / deleted account / delegation revoke / delegate scope)
  // for every authenticated request — actions and API endpoints included, which
  // layout loads never cover. Throws kit-native redirects; runs after the rate
  // limiter so the gate RPCs sit behind the same request budget.
  if (event.locals.session) {
    event.locals.session = await guardSession(event, event.locals.session);
  }

  const locale = resolveLocale(event);
  event.locals.locale = locale;
  // Custom-cursor preference: only an explicit '0' cookie turns it off, so a
  // fresh visitor (no cookie) keeps the default animated cursor.
  event.locals.cursorEnabled = event.cookies.get(CURSOR_COOKIE) !== '0';
  tagTransaction(newrelic, event, event.locals.session);

  // Compose the RUM injector with a one-shot <html lang> rewrite: the shell's
  // opening tag ships lang="en" (app.html), so patch it to the resolved locale
  // on the first chunk that carries it. Both transforms are per-request and
  // streaming-safe.
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
  return res;
};

export const handleError: HandleServerError = ({ error, event, status }) => {
  noticeServerError(newrelic, error, event, status);
  return { message: 'Internal Error' };
};
