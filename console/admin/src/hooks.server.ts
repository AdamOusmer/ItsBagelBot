import type { Handle } from '@sveltejs/kit';
import { COOKIE, open } from '$lib/server/session';
import { warm } from '@bagel/shared/server/nats';
import { startInvalidationListener } from '$lib/server/rpc';
import { assertConfigSane } from '$lib/server/config-sanity';
import dns from 'node:dns';

// Force node:dns to resolve IPv4 first to bypass k3s IPv6 timeout issues
dns.setDefaultResultOrder('ipv4first');
assertConfigSane();

// Pre-dial NATS at server start so the first request hits a warm connection
// instead of paying the cold dial on the hot path.
warm();
startInvalidationListener();

// Session + the security headers SvelteKit's CSP config does not own.
export const handle: Handle = async ({ event, resolve }) => {
  const cookie = event.cookies.get(COOKIE);
  event.locals.session = cookie ? open(cookie) : null;

  const res = await resolve(event);

  res.headers.set('X-Content-Type-Options', 'nosniff');
  res.headers.set('X-Frame-Options', 'DENY');
  res.headers.set('Referrer-Policy', 'same-origin');
  res.headers.set('Permissions-Policy', 'camera=(), microphone=(), geolocation=(), payment=()');
  res.headers.set('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');

  // HTML is session-bound; never cache. Hashed assets under /_app keep their own
  // immutable Cache-Control from the adapter.
  const ct = res.headers.get('content-type') ?? '';
  if (ct.includes('text/html')) res.headers.set('Cache-Control', 'no-store');

  return res;
};
