import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { guardSession } from '$lib/server/guard';
import { warm } from '@bagel/shared/server/nats';
import { warm as warmValkey } from '@bagel/shared/server/valkey-store';
import { registerServerConfig } from '@bagel/shared/server/config';
import { rumTransform } from '@bagel/shared/server/rum';
import { ValkeyRateLimiter, warmRateLimiter, clientIp } from '@bagel/shared/server/rate-limit';
import { detectLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';
import dns from 'node:dns';

// Framework-native one-time boot. SvelteKit calls init() once before the first
// request; all boot side effects live here instead of at module-eval.
//
// Boot config reads process.env, NOT $env/dynamic/private: init() runs under the
// server entry's top-level `await server.init()`, so reading the dynamic-env
// proxy here deadlocks that await (unsettled top-level await -> exit 13). In
// adapter-node process.env carries the same Doppler-injected runtime values, and
// request-time code (session, oauth, rpc) keeps using $env/dynamic/private.
export const init: ServerInit = async () => {
  // Force node:dns to resolve IPv4 first to bypass k3s IPv6 timeout issues.
  dns.setDefaultResultOrder('ipv4first');

  const env = process.env;
  assertConfigSane(env);

  // Register the caching-layer config (Valkey read tier + invalidation bus) so
  // shared infra resolves it without touching $env itself.
  registerServerConfig({
    valkey: env.VALKEY_ADDR
      ? {
          addr: env.VALKEY_ADDR,
          password: env.VALKEY_PASSWORD,
          // Optional Sentinel endpoint for write-path clients (rate limiter):
          // tracks the elected master across failovers instead of pinning a
          // node-local instance that may be a read-only replica.
          sentinelAddr: env.VALKEY_SENTINEL_ADDR,
          sentinelMaster: env.VALKEY_MASTER_SET
        }
      : undefined,
    cacheInvalidationPrefix: env.NATS_CACHE_INVALIDATION_PREFIX ?? 'bagel.cache.invalidate'
  });

  // Pre-dial NATS and pre-connect the Valkey read pool and rate-limit write
  // client so the first request hits warm connections instead of paying the
  // cold dial/connect on the hot path.
  warm();
  warmValkey();
  warmRateLimiter();

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
// Keyed by session user id when logged in (stable across CGNAT/mobile IP
// churn), else by client IP.
const authLimiter = new ValkeyRateLimiter({ name: 'auth', capacity: 10, refillPerSec: 10 / 60 });
const writeLimiter = new ValkeyRateLimiter({ name: 'write', capacity: 30, refillPerSec: 0.5 });
const readLimiter = new ValkeyRateLimiter({ name: 'read', capacity: 60, refillPerSec: 2 });

// Kubelet probes are frequent, unauthenticated and share the node IP; limiting
// them would let the limiter fail readiness. Static /_app assets never reach
// this handle (served by sirv in serve-node.js).
const RATE_EXEMPT = new Set(['/healthz', '/readyz']);

function pickLimiter(pathname: string, method: string): ValkeyRateLimiter {
  if (pathname.startsWith('/auth/') || pathname.startsWith('/delegate/')) return authLimiter;
  if (method !== 'GET' && method !== 'HEAD') return writeLimiter;
  return readLimiter;
}

// enforceRateLimit checks the request against its tier's fleet-wide bucket,
// keyed by session user id (stable across CGNAT/mobile IP churn) or client IP.
// It returns a 429 Response when the budget is spent, else null to proceed.
// Exempt paths (kubelet probes) skip the check entirely.
async function enforceRateLimit(event: Parameters<Handle>[0]['event']): Promise<Response | null> {
  if (RATE_EXEMPT.has(event.url.pathname)) {
    return null;
  }
  const key = event.locals.session?.user_id
    ? `u:${event.locals.session.user_id}`
    : `ip:${clientIp(event.request.headers, event.getClientAddress)}`;
  const decision = await pickLimiter(event.url.pathname, event.request.method).check(key);
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
  if (queryLang === 'fr' || queryLang === 'en') {
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

// tagTransaction names the New Relic web transaction by SvelteKit route (so
// per-id paths group instead of exploding by raw URL) and tags request/session
// context for faceting.
function tagTransaction(event: Parameters<Handle>[0]['event']): void {
  const session = event.locals.session;
  newrelic.setTransactionName(`${event.request.method} ${event.route.id ?? event.url.pathname}`);
  newrelic.addCustomAttributes({
    'route.id': event.route.id ?? 'unmatched',
    'http.method': event.request.method,
    'enduser.authenticated': !!session
  });
  if (session?.user_id) newrelic.setUserID(String(session.user_id));
}

// harden sets the security headers SvelteKit's CSP config does not own, then
// makes HTML pages AND navigation redirects uncacheable. A SvelteKit redirect
// carries no content-type or Cache-Control, so the text/html check alone would
// leave 30x responses cacheable: the CF edge could then pin a stale "go here"
// (e.g. /login or a post-action target) and replay it to the wrong
// user/session after a deploy. __data.json is already `private, no-store` and
// hashed /_app assets are served by sirv with their own immutable caching.
function harden(res: Response): void {
  res.headers.set('X-Content-Type-Options', 'nosniff');
  res.headers.set('X-Frame-Options', 'DENY');
  res.headers.set('Referrer-Policy', 'same-origin');
  res.headers.set(
    'Permissions-Policy',
    'camera=(), microphone=(), geolocation=(), payment=(), join-ad-interest-group=(), run-ad-auction=(), shared-storage=(), browsing-topics=()'
  );
  res.headers.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');

  const ct = res.headers.get('content-type') ?? '';
  const isRedirect = res.status >= 300 && res.status < 400;
  if (isRedirect || ct.includes('text/html')) res.headers.set('Cache-Control', 'no-store');
}

// Session + account gates + the security headers SvelteKit's CSP config does
// not own.
export const handle: Handle = async ({ event, resolve }) => {
  const cookie = event.cookies.get(COOKIE);
  event.locals.session = cookie ? open(cookie) : null;
  // Expired/invalid cookie: drop it eagerly so the browser stops replaying it.
  if (cookie && !event.locals.session) {
    event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
  }

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
  tagTransaction(event);

  // Compose the RUM injector with a one-shot <html lang> rewrite: the shell's
  // opening tag ships lang="en" (app.html), so patch it to the resolved locale
  // on the first chunk that carries it. Both transforms are per-request and
  // streaming-safe.
  const rum = rumTransform();
  let langPatched = false;
  const res = await resolve(event, {
    // SvelteKit preloads js + css by default; add fonts so the SSR'd <head>
    // warms the woff2 files in parallel with the bundle instead of waiting for
    // CSS to parse first. Fewer round-trips, less FOUT/CLS on first paint.
    preload: ({ type }) => type === 'js' || type === 'css' || type === 'font',
    transformPageChunk: (opts) => {
      let html = rum(opts);
      if (!langPatched && html.includes('<html')) {
        html = html.replace('lang="en"', `lang="${locale}"`);
        langPatched = true;
      }
      return html;
    }
  });

  harden(res);
  return res;
};

// Send unexpected server errors to New Relic with route/status context. 4xx are
// expected (auth/not-found) and left out so the error rate tracks real faults.
export const handleError: HandleServerError = ({ error, event, status }) => {
  if (status >= 500) {
    newrelic.noticeError(error instanceof Error ? error : new Error(String(error)), {
      'route.id': event.route?.id ?? event.url.pathname,
      'http.status': status
    });
  }
  return { message: 'Internal Error' };
};
