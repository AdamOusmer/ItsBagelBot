// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync } from 'node:fs';
import { join, relative, sep } from 'node:path';
import { describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: { startSegment: (_n: string, _r: boolean, f: () => unknown) => f(), recordMetric: () => {}, noticeError: () => {} }
}));
mock.module('$lib/server/session', () => ({ COOKIE: 'bagel_session', CURSOR_COOKIE: 'bb_cursor', open: () => null }));
mock.module('@bagel/kit/i18n', () => ({ LOCALE_COOKIE: 'locale' }));
mock.module('$lib/server/guard', () => ({ guardSession: async (_e: unknown, s: unknown) => s }));
mock.module('@bagel/kit/server/valkey-store', () => ({ warm: () => {} }));
mock.module('@bagel/kit/server/boot', () => ({ initConsoleRuntime: () => {} }));
mock.module('@bagel/kit/server/hooks', () => ({
  harden: () => {},
  noticeServerError: () => {},
  openSessionCookie: () => null,
  preloadStrategy: () => true,
  resolveLocale: async () => 'en',
  tagTransaction: () => {}
}));
mock.module('@bagel/kit/server/rum', () => ({ rumTransform: () => (html: string) => html }));
mock.module('@bagel/kit/server/rate-limit', () => ({
  ValkeyRateLimiter: class {
    async check() {
      return { allowed: true, retryAfterSec: 0 };
    }
  },
  warmRateLimiter: () => {}
}));
mock.module('@bagel/kit/server/session-revocation', () => ({ warmSessionRevocation: () => {} }));
mock.module('@bagel/kit/server/logger', () => ({ logger: { error: () => {} } }));
mock.module('$lib/server/services', () => ({ startInvalidationListener: () => {} }));
mock.module('$lib/server/config-sanity', () => ({ assertConfigSane: () => {} }));
mock.module('$lib/server/edge-purge', () => ({ purgeEdgeByTag: async () => true }));

const { DEPLOY_CACHE_TAG, EDGE_CACHE, applyEdgeCache, edgeCacheHeaders } = await import('./hooks.server');

type Event = Parameters<typeof edgeCacheHeaders>[0];

const LOGIN = '/(public)/login';
const CHANNEL = '/(public)/user/[channel]';

function makeEvent(opts: {
  routeId: string;
  session?: unknown;
  edgeCache404?: boolean;
  lang?: string;
  cursorCookie?: string;
  localeCookie?: string;
}): Event {
  const url = new URL(`https://commands.itsbagelbot.com/user/foo${opts.lang ? `?lang=${opts.lang}` : ''}`);
  const cookies: Record<string, string | undefined> = { bb_cursor: opts.cursorCookie, locale: opts.localeCookie };
  return {
    route: { id: opts.routeId },
    request: { method: 'GET' },
    url,
    locals: { session: opts.session ?? null, edgeCache404: opts.edgeCache404 },
    cookies: { get: (name: string) => cookies[name] }
  } as unknown as Event;
}

function htmlResponse(status: number, headers: Record<string, string> = {}): Response {
  return new Response('<html></html>', { status, headers: { 'content-type': 'text/html', ...headers } });
}

function routeIds(dir: string, root = dir): string[] {
  const ids: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) ids.push(...routeIds(join(dir, entry.name), root));
    else if (entry.name === '+page.svelte' || entry.name === '+page.server.ts') {
      ids.push('/' + relative(root, dir).split(sep).join('/'));
    }
  }
  return ids;
}

describe('EDGE_CACHE', () => {
  test('every key is a real route id', () => {
    const real = new Set(routeIds(join(import.meta.dir, 'routes')));
    for (const key of Object.keys(EDGE_CACHE)) expect(real.has(key)).toBe(true);
  });
});

describe('edgeCacheHeaders', () => {
  test('a 404 with locals.edgeCache404 gets the shared cache headers', () => {
    const event = makeEvent({ routeId: CHANNEL, edgeCache404: true });
    expect(edgeCacheHeaders(event, htmlResponse(404))).not.toBeNull();
  });

  test('a 404 without the flag stays no-store', () => {
    const event = makeEvent({ routeId: CHANNEL, edgeCache404: false });
    expect(edgeCacheHeaders(event, htmlResponse(404))).toBeNull();
  });

  test('the flag is set but the request carries a session: still no-store', () => {
    const event = makeEvent({ routeId: CHANNEL, edgeCache404: true, session: { user_id: '1' } });
    expect(edgeCacheHeaders(event, htmlResponse(404))).toBeNull();
  });

  test('a plain 200 emits browser revalidation plus CDN-only directives, never s-maxage', () => {
    const headers = edgeCacheHeaders(makeEvent({ routeId: CHANNEL }), htmlResponse(200));
    expect(headers).toEqual({
      'Cache-Control': 'public, max-age=0',
      'CDN-Cache-Control': 'max-age=300, stale-while-revalidate=3600, stale-if-error=86400',
      'Cache-Tag': DEPLOY_CACHE_TAG
    });
    expect(JSON.stringify(headers)).not.toContain('s-maxage');
  });

  test('login and stats use their own TTLs', () => {
    expect(edgeCacheHeaders(makeEvent({ routeId: LOGIN }), htmlResponse(200))?.['CDN-Cache-Control']).toBe(
      'max-age=600, stale-while-revalidate=86400, stale-if-error=86400'
    );
    expect(edgeCacheHeaders(makeEvent({ routeId: '/(public)/stats' }), htmlResponse(200))?.['CDN-Cache-Control']).toBe(
      'max-age=30, stale-while-revalidate=300, stale-if-error=86400'
    );
  });

  test('unlisted routes stay no-store', () => {
    expect(edgeCacheHeaders(makeEvent({ routeId: '/(app)/commands' }), htmlResponse(200))).toBeNull();
  });

  test('a locale cookie, a lang param, or the cursor-off cookie bypasses the cache', () => {
    for (const opts of [{ localeCookie: 'fr' }, { lang: 'fr' }, { cursorCookie: '0' }]) {
      expect(edgeCacheHeaders(makeEvent({ routeId: CHANNEL, ...opts }), htmlResponse(200))).toBeNull();
    }
  });

  test('non-html responses are not cached', () => {
    const res = new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } });
    expect(edgeCacheHeaders(makeEvent({ routeId: CHANNEL }), res)).toBeNull();
  });

  test('a garbage session cookie carries a delete Set-Cookie: never cached, even with locals.session null', () => {
    const res = htmlResponse(200, { 'set-cookie': 'bagel_session=; Max-Age=0; Path=/' });
    expect(edgeCacheHeaders(makeEvent({ routeId: CHANNEL }), res)).toBeNull();
  });
});

describe('applyEdgeCache', () => {
  test('sets both headers and adds Vary: Accept-Language', () => {
    const res = htmlResponse(200);
    applyEdgeCache(makeEvent({ routeId: CHANNEL }), res);
    expect(res.headers.get('Cache-Control')).toBe('public, max-age=0');
    expect(res.headers.get('CDN-Cache-Control')).toContain('stale-while-revalidate=3600');
    expect(res.headers.get('Cache-Tag')).toBe(DEPLOY_CACHE_TAG);
    expect(res.headers.get('Vary')).toBe('Accept-Language');
  });

  test('appends to an existing Vary', () => {
    const res = htmlResponse(200, { vary: 'Accept-Encoding' });
    applyEdgeCache(makeEvent({ routeId: CHANNEL }), res);
    expect(res.headers.get('Vary')).toBe('Accept-Encoding, Accept-Language');
  });

  test('leaves an uncacheable response untouched', () => {
    const res = htmlResponse(200);
    applyEdgeCache(makeEvent({ routeId: CHANNEL, localeCookie: 'en' }), res);
    expect(res.headers.get('CDN-Cache-Control')).toBeNull();
    expect(res.headers.get('Vary')).toBeNull();
  });

  test('a response with Set-Cookie is left uncached', () => {
    const res = htmlResponse(200, { 'set-cookie': 'bagel_session=; Max-Age=0; Path=/' });
    applyEdgeCache(makeEvent({ routeId: CHANNEL }), res);
    expect(res.headers.get('CDN-Cache-Control')).toBeNull();
    expect(res.headers.get('Vary')).toBeNull();
  });
});
