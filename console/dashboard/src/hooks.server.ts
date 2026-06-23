import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { warm } from '@bagel/shared/server/nats';
import { warm as warmValkey } from '@bagel/shared/server/valkey-store';
import { registerServerConfig } from '@bagel/shared/server/config';
import { startInvalidationListener } from '$lib/server/rpc';
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
    valkey: env.VALKEY_ADDR ? { addr: env.VALKEY_ADDR, password: env.VALKEY_PASSWORD } : undefined,
    cacheInvalidationPrefix: env.NATS_CACHE_INVALIDATION_PREFIX ?? 'bagel.cache.invalidate'
  });

  // Pre-dial NATS and pre-connect the Valkey read pool so the first request hits
  // warm connections instead of paying the cold dial/connect on the hot path.
  warm();
  warmValkey();

  // Subscribe to the cache-invalidation bus so writes in Go services push-drop
  // the right keys without waiting on TTL expiry.
  startInvalidationListener();
};

// Session + the security headers SvelteKit's CSP config does not own.
export const handle: Handle = async ({ event, resolve }) => {
  const cookie = event.cookies.get(COOKIE);
  event.locals.session = cookie ? open(cookie) : null;

  // New Relic: name the web transaction by SvelteKit route (not the raw URL, so
  // per-id paths group), and tag it with request/session context for faceting.
  const session = event.locals.session;
  newrelic.setTransactionName(`${event.request.method} ${event.route.id ?? event.url.pathname}`);
  newrelic.addCustomAttributes({
    'route.id': event.route.id ?? 'unmatched',
    'http.method': event.request.method,
    'enduser.authenticated': !!session
  });
  if (session?.user_id) newrelic.setUserID(String(session.user_id));

  // New Relic Browser (RUM): inject the agent loader inline in <head>, reusing
  // the per-response CSP nonce SvelteKit already emitted so `script-src` stays
  // nonce-based (no 'unsafe-inline'). Captured from whichever streamed chunk
  // carries it; injected at </head>. Empty when the agent isn't connected (dev),
  // so this is a clean no-op there.
  let cspNonce: string | undefined;
  const res = await resolve(event, {
    // SvelteKit preloads js + css by default; add fonts so the SSR'd <head>
    // warms the woff2 files in parallel with the bundle instead of waiting for
    // CSS to parse first. Fewer round-trips, less FOUT/CLS on first paint.
    preload: ({ type }) => type === 'js' || type === 'css' || type === 'font',
    transformPageChunk: ({ html }) => {
      if (!cspNonce) {
        const m = html.match(/nonce="([^"]+)"/);
        if (m) cspNonce = m[1];
      }
      if (cspNonce && html.includes('</head>')) {
        let snippet = '';
        try {
          snippet = newrelic.getBrowserTimingHeader({ nonce: cspNonce });
        } catch {
          /* agent not ready (e.g. dev); skip injection */
        }
        if (snippet) return html.replace('</head>', `${snippet}</head>`);
      }
      return html;
    }
  });

  res.headers.set('X-Content-Type-Options', 'nosniff');
  res.headers.set('X-Frame-Options', 'DENY');
  res.headers.set('Referrer-Policy', 'same-origin');
  res.headers.set('Permissions-Policy', 'camera=(), microphone=(), geolocation=(), payment=(), join-ad-interest-group=(), run-ad-auction=(), shared-storage=(), browsing-topics=()');
  res.headers.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');

  // HTML is session-bound; never cache. Hashed assets under /_app keep their own
  // immutable Cache-Control from the adapter.
  const ct = res.headers.get('content-type') ?? '';
  if (ct.includes('text/html')) res.headers.set('Cache-Control', 'no-store');

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
