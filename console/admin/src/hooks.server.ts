import type { Handle, HandleServerError, ServerInit } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { requireAdmin } from '$lib/server/access';
import { initConsoleRuntime } from '@bagel/shared/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  tagTransaction
} from '@bagel/shared/server/hooks';
import { rumTransform } from '@bagel/shared/server/rum';
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

// Login/OAuth flow and probes stay reachable without a staff session; every
// other route is gated below.
const PUBLIC_PREFIXES = ['/auth', '/login', '/healthz', '/readyz'];

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(p + '/'));
}

const PERMISSIONS_POLICY = 'camera=(), microphone=(), geolocation=(), payment=()';

// Session + staff gate + the security headers SvelteKit's CSP config does not
// own.
export const handle: Handle = async ({ event, resolve }) => {
  event.locals.session = openSessionCookie(event, COOKIE, open);

  // Staff gate for every non-public request — form actions and +server.ts
  // endpoints included, which layout loads never cover. The per-route
  // requireAdmin checks stay as defense in depth; this hook makes "session
  // exists but is no longer active staff" die at the door (adminCheck is
  // fabric-cached and push-invalidated on the staff scope, so a roster change
  // revokes access on every replica within one request). requireAdmin fails
  // closed on an auth-service outage, matching the per-route posture.
  if (!DEMO && !isPublic(event.url.pathname)) {
    if (event.locals.session && !(await requireAdmin(event.locals.session))) {
      event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
      event.locals.session = null;
      throw redirect(303, '/login?e=denied');
    }
  }

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
