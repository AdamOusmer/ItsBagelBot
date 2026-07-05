import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { warm } from '@bagel/shared/server/nats';
import { warm as warmValkey } from '@bagel/shared/server/valkey-store';
import { registerServerConfig } from '@bagel/shared/server/config';
import { rumTransform } from '@bagel/shared/server/rum';
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

  let queryLang = event.url.searchParams.get('lang');
  if (queryLang === 'fr' || queryLang === 'en') {
    // If the url contains a valid lang override (e.g. from the marketing site), pin it.
    event.cookies.set(LOCALE_COOKIE, queryLang, { path: '/', maxAge: 31536000, secure: true, sameSite: 'lax' });
  }

  // Resolve the UI locale once per request: query parameter wins (if just set), else
  // the explicit switcher cookie, else the browser's Accept-Language, else English.
  // Admin view-as sessions are always rendered in English. Their language
  // control edits the target account preference; it must not translate the
  // administrative UI itself.
  const locale = event.locals.session?.impersonator_id
    ? 'en'
    : detectLocale({
        cookie: queryLang || event.cookies.get(LOCALE_COOKIE),
        accept: event.request.headers.get('accept-language')
      });
  event.locals.locale = locale;

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

  res.headers.set('X-Content-Type-Options', 'nosniff');
  res.headers.set('X-Frame-Options', 'DENY');
  res.headers.set('Referrer-Policy', 'same-origin');
  res.headers.set(
    'Permissions-Policy',
    'camera=(), microphone=(), geolocation=(), payment=(), join-ad-interest-group=(), run-ad-auction=(), shared-storage=(), browsing-topics=()'
  );
  res.headers.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');

  // HTML pages AND navigation redirects are session-bound; never let the browser
  // or the CF edge cache them. A SvelteKit redirect carries no content-type and
  // no Cache-Control, so the text/html check alone leaves 30x responses
  // cacheable: the edge can then pin a stale "go here" (e.g. /login, or a
  // post-action target) and replay it to the wrong user/session after a deploy.
  // SvelteKit already marks __data.json `private, no-store`; hashed /_app assets
  // are served by sirv with their own immutable caching, so this never touches
  // them.
  const ct = res.headers.get('content-type') ?? '';
  const isRedirect = res.status >= 300 && res.status < 400;
  if (isRedirect || ct.includes('text/html')) res.headers.set('Cache-Control', 'no-store');

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
