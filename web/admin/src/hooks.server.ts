// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Handle, HandleServerError, RequestEvent, ServerInit } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { requireAdmin, requireRole } from '$lib/server/access';
import { initConsoleRuntime } from '@bagel/shared/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  tagTransaction
} from '@bagel/shared/server/hooks';
import { rumTransform } from '@bagel/shared/server/rum';
import { detectLocale, isLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';
import { ensureLaneStoreHA } from '$lib/server/lanes';

// Direct use of SvelteKit's build-time flag lets Rollup erase the local demo
// gate from production instead of leaving a runtime-configurable bypass.
const DEMO = dev && process.env.DEMO === '1';

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

  void ensureLaneStoreHA().catch((error) => {
    newrelic.noticeError(error instanceof Error ? error : new Error(String(error)), {
      component: 'nats-kv-ha-reconcile'
    });
  });

  // Subscribe to the cache-invalidation bus so writes in Go services push-drop
  // the right keys without waiting on TTL expiry.
  startInvalidationListener();
};

// The operator sign-in legs and the probes stay reachable without a staff
// session; every other route is gated below.
//
// This list used to be the whole of '/auth', which swept in /auth/bot/* -- the
// bot-account OAuth consent flow. That flow ends by writing a live Twitch
// token for the account the bot speaks as, so being reachable unauthenticated
// meant anyone who could reach the host could install one. The bot legs are
// gated as owner-only below instead.
const PUBLIC_PREFIXES = [
  '/auth/login',
  '/auth/callback',
  '/auth/logout',
  '/login',
  '/healthz',
  '/readyz'
];

const BOT_FLOW_PREFIX = '/auth/bot';

function matches(pathname: string, prefix: string): boolean {
  return pathname === prefix || pathname.startsWith(prefix + '/');
}

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => matches(pathname, p));
}

// staleStaffSession: a non-public request whose session is no longer active
// staff. Flat rather than nested so the handle below reads as a list of gates.
async function staleStaffSession(event: RequestEvent): Promise<boolean> {
  if (DEMO || isPublic(event.url.pathname)) return false;
  if (!event.locals.session) return false;
  return !(await requireAdmin(event.locals.session));
}

// botFlowRefused: the bot-account consent legs demand an owner session in the
// browser that walks them, which is a real behaviour change -- the operator
// now signs in as owner first, then consents as the bot account in that same
// browser, instead of opening a copied link in a fresh one.
async function botFlowRefused(event: RequestEvent): Promise<boolean> {
  if (DEMO || !matches(event.url.pathname, BOT_FLOW_PREFIX)) return false;
  return !(await requireRole(event, 'bot.token'));
}

// resolveLocale resolves the UI locale once per request, mirroring the
// dashboard's hook: a valid ?lang override wins (and is pinned to the switcher
// cookie), else the cookie, else the browser's Accept-Language, else English.
// There is no per-account preference here -- an operator's admin UI language is
// a property of their browser, not of any account they are looking at.
function resolveLocale(event: RequestEvent): ReturnType<typeof detectLocale> {
  const queryLang = event.url.searchParams.get('lang');
  if (isLocale(queryLang)) {
    event.cookies.set(LOCALE_COOKIE, queryLang, { path: '/', maxAge: 31536000, secure: true, sameSite: 'lax' });
  }
  return detectLocale({
    cookie: queryLang || event.cookies.get(LOCALE_COOKIE),
    accept: event.request.headers.get('accept-language')
  });
}

const PERMISSIONS_POLICY = 'camera=(), microphone=(), geolocation=(), payment=()';

// Session + staff gate + the security headers SvelteKit's CSP config does not
// own.
export const handle: Handle = async ({ event, resolve }) => {
  event.locals.session = openSessionCookie(event, COOKIE, open);
  event.locals.locale = resolveLocale(event);

  // Staff gate for every non-public request: form actions and +server.ts
  // endpoints included, which layout loads never cover. The per-route
  // requireAdmin checks stay as defense in depth; this hook makes "session
  // exists but is no longer active staff" die at the door (adminCheck is
  // fabric-cached and push-invalidated on the staff scope, so a roster change
  // revokes access on every replica within one request). requireAdmin fails
  // closed on an auth-service outage, matching the per-route posture.
  if (await staleStaffSession(event)) {
    event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
    event.locals.session = null;
    throw redirect(303, '/login?e=denied');
  }

  if (await botFlowRefused(event)) throw redirect(303, '/login?e=denied');

  tagTransaction(newrelic, event, event.locals.session);

  const res = await resolve(event, {
    preload: preloadStrategy,
    // New Relic Browser (RUM) injection; single-chunk, streaming-safe (shared
    // helper). No-op when the agent isn't connected (dev).
    transformPageChunk: rumTransform()
  });

  harden(res, PERMISSIONS_POLICY);
  return res;
};

export const handleError: HandleServerError = ({ error, event, status }) => {
  noticeServerError(newrelic, error, event, status);
  return { message: 'Internal Error' };
};
