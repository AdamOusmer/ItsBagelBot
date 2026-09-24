// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: { startSegment: (_n: string, _r: boolean, f: () => unknown) => f(), recordMetric: () => {}, noticeError: () => {} }
}));
mock.module('$lib/server/session', () => ({ COOKIE: 'bagel_session', CURSOR_COOKIE: 'bb_cursor', open: () => null }));
mock.module('$lib/server/guard', () => ({ guardSession: async (_e: unknown, s: unknown) => s }));
mock.module('@bagel/kit/server/valkey-store', () => ({ warm: () => {} }));
mock.module('@bagel/kit/server/boot', () => ({ initConsoleRuntime: () => {} }));
mock.module('@bagel/kit/server/hooks', () => ({
  harden: () => {},
  noticeServerError: () => {},
  openSessionCookie: () => null,
  preloadStrategy: () => true,
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
mock.module('@bagel/kit/i18n', () => ({
  detectLocale: () => 'en',
  ensureCatalog: async () => {},
  isLocale: (v: unknown) => v === 'en' || v === 'fr',
  LOCALE_COOKIE: 'bb_locale'
}));
mock.module('$lib/server/services', () => ({ startInvalidationListener: () => {} }));
mock.module('$lib/server/config-sanity', () => ({ assertConfigSane: () => {} }));

const { edgeCacheControl } = await import('./hooks.server');

type Event = Parameters<typeof edgeCacheControl>[0];

function makeEvent(opts: { routeId: string; session?: unknown; locale?: string; edgeCache404?: boolean; lang?: string; cursorCookie?: string }): Event {
  const url = new URL(`https://commands.itsbagelbot.com/user/foo${opts.lang ? `?lang=${opts.lang}` : ''}`);
  return {
    route: { id: opts.routeId },
    request: { method: 'GET' },
    url,
    locals: { session: opts.session ?? null, locale: opts.locale ?? 'en', edgeCache404: opts.edgeCache404 },
    cookies: { get: (name: string) => (name === 'bb_cursor' ? opts.cursorCookie : undefined) }
  } as unknown as Event;
}

function htmlResponse(status: number): Response {
  return new Response('<html></html>', { status, headers: { 'content-type': 'text/html' } });
}

describe('edgeCacheControl', () => {
  test('a 404 with locals.edgeCache404 gets the shared cache header', () => {
    const event = makeEvent({ routeId: '/user/[channel]', edgeCache404: true });
    expect(edgeCacheControl(event, htmlResponse(404))).not.toBeNull();
  });

  test('a 404 without the flag stays no-store', () => {
    const event = makeEvent({ routeId: '/user/[channel]', edgeCache404: false });
    expect(edgeCacheControl(event, htmlResponse(404))).toBeNull();
  });

  test('the flag is set but the request carries a session: still no-store', () => {
    const event = makeEvent({ routeId: '/user/[channel]', edgeCache404: true, session: { user_id: '1' } });
    expect(edgeCacheControl(event, htmlResponse(404))).toBeNull();
  });

  test('a plain 200 on a listed route still caches (unrelated to the new gate)', () => {
    const event = makeEvent({ routeId: '/user/[channel]' });
    expect(edgeCacheControl(event, htmlResponse(200))).not.toBeNull();
  });
});
