// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync } from 'node:fs';
import { join, relative, sep } from 'node:path';
import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../test/sveltekit';

process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
stubSvelteKit();
mock.module('$lib/server/guard', () => ({ guardSession: async (_event: unknown, session: unknown) => session }));

const { COOKIE, CURSOR_COOKIE, seal } = await import('./lib/server/session');
const { LOCALE_COOKIE } = await import('@bagel/kit/i18n');
const { EDGE_CACHE, applyEdgeCache, edgeCacheHeaders, handle } = await import('./hooks.server');

type Event = Parameters<typeof edgeCacheHeaders>[0];
type EventOptions = {
  routeId?: string;
  session?: unknown;
  edgeCache404?: boolean;
  edgeCacheTag?: string;
  lang?: string;
  cursorCookie?: string;
  localeCookie?: string;
  sessionCookie?: string;
  dataRequest?: boolean;
  skip?: boolean;
  ip?: string;
  socket?: string;
  method?: string;
};

const LOGIN = '/(public)/login';
const CHANNEL = '/(public)/user/[channel]';
const SET_COOKIE = { 'set-cookie': 'bagel_session=; Max-Age=0; Path=/' };

function makeEvent(opts: EventOptions): Event {
  const cookies: Record<string, string | undefined> = {
    [CURSOR_COOKIE]: opts.cursorCookie,
    [LOCALE_COOKIE]: opts.localeCookie,
    [COOKIE]: opts.sessionCookie
  };
  const url = new URL(`https://commands.itsbagelbot.com/user/foo${opts.lang ? `?lang=${opts.lang}` : ''}`);
  return {
    route: { id: opts.routeId ?? CHANNEL },
    request: new Request(url, { method: opts.method, headers: opts.ip ? { 'cf-connecting-ip': opts.ip } : {} }),
    url,
    isDataRequest: !!opts.dataRequest,
    getClientAddress: () => opts.socket ?? '10.0.0.1',
    locals: {
      session: opts.session ?? null,
      edgeCache404: opts.edgeCache404,
      edgeCacheTag: opts.edgeCacheTag,
      edgeCacheSkip: opts.skip
    },
    cookies: { get: (name: string) => cookies[name], set: () => {}, delete: () => {} }
  } as unknown as Event;
}

function htmlResponse(status: number, headers: Record<string, string> = {}): Response {
  return new Response('<html></html>', { status, headers: { 'content-type': 'text/html', ...headers } });
}

function jsonResponse(status = 200, headers: Record<string, string> = {}): Response {
  return new Response('{}', { status, headers: { 'content-type': 'application/json', ...headers } });
}

function routeIds(dir: string, root = dir): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (entry.isDirectory()) return routeIds(join(dir, entry.name), root);
    const isRoute = entry.name === '+page.svelte' || entry.name === '+page.server.ts';
    return isRoute ? ['/' + relative(root, dir).split(sep).join('/')] : [];
  });
}

test('every EDGE_CACHE key is a real route id', () => {
  const real = new Set(routeIds(join(import.meta.dir, 'routes')));
  expect(Object.keys(EDGE_CACHE).filter((key) => !real.has(key))).toEqual([]);
});

const signedIn = seal({
  user_id: '1',
  login: 'streamer',
  display_name: 'Streamer',
  role: 'streamer',
  sid: 's1',
  iat: Math.floor(Date.now() / 1000),
  expires_at: Math.floor(Date.now() / 1000) + 3600
});

const cached = (directives: string) => ({
  'CDN-Cache-Control': directives,
  'Cache-Tag': 'dashboard-edge-shell',
  Vary: 'Accept-Language'
});
const uncached = { 'CDN-Cache-Control': null, 'Cache-Tag': null, Vary: null };
const DEFAULT_TTL = cached('max-age=300, stale-while-revalidate=3600, stale-if-error=86400');

const edgeCases: { name: string; event: EventOptions; res: Response; want: Record<string, string | null> }[] = [
  { name: 'a 404 with locals.edgeCache404 gets the shared cache headers', event: { edgeCache404: true }, res: htmlResponse(404), want: DEFAULT_TTL },
  { name: 'a 404 without the flag stays no-store', event: { edgeCache404: false }, res: htmlResponse(404), want: uncached },
  { name: 'the flag is set but the request carries a session: still no-store', event: { edgeCache404: true, sessionCookie: signedIn }, res: htmlResponse(404), want: uncached },
  { name: 'a plain 200 emits CDN-only directives so browsers revalidate', event: {}, res: htmlResponse(200), want: DEFAULT_TTL },
  {
    name: 'a route-supplied tag joins the deploy tag',
    event: { edgeCacheTag: 'lb:foo' },
    res: htmlResponse(200),
    want: { ...DEFAULT_TTL, 'Cache-Tag': 'dashboard-edge-shell,lb:foo' }
  },
  {
    name: 'a route-supplied tag also tags the edge-cached 404',
    event: { edgeCache404: true, edgeCacheTag: 'lb:foo' },
    res: htmlResponse(404),
    want: { ...DEFAULT_TTL, 'Cache-Tag': 'dashboard-edge-shell,lb:foo' }
  },
  { name: 'login uses its own TTL', event: { routeId: LOGIN }, res: htmlResponse(200), want: cached('max-age=600, stale-while-revalidate=86400, stale-if-error=86400') },
  { name: 'stats uses its own TTL', event: { routeId: '/(public)/stats' }, res: htmlResponse(200), want: cached('max-age=30, stale-while-revalidate=300, stale-if-error=86400') },
  { name: 'unlisted routes stay no-store', event: { routeId: '/(app)/commands' }, res: htmlResponse(200), want: uncached },
  { name: 'a locale cookie bypasses the cache', event: { localeCookie: 'fr' }, res: htmlResponse(200), want: uncached },
  { name: 'a lang param bypasses the cache', event: { lang: 'fr' }, res: htmlResponse(200), want: uncached },
  { name: 'the cursor-off cookie bypasses the cache', event: { cursorCookie: '0' }, res: htmlResponse(200), want: uncached },
  { name: 'non-html responses are not cached', event: {}, res: jsonResponse(), want: uncached },
  {
    name: 'an anonymous data request gets the same CDN headers and tag',
    event: { dataRequest: true, edgeCacheTag: 'lb:foo' },
    res: jsonResponse(),
    want: { ...DEFAULT_TTL, 'Cache-Tag': 'dashboard-edge-shell,lb:foo' }
  },
  { name: 'a data request answered with html is not cached', event: { dataRequest: true }, res: htmlResponse(200), want: uncached },
  { name: 'a data request with a locale cookie is not cached', event: { dataRequest: true, localeCookie: 'fr' }, res: jsonResponse(), want: uncached },
  { name: 'a data request with a lang param is not cached', event: { dataRequest: true, lang: 'fr' }, res: jsonResponse(), want: uncached },
  { name: 'a data request with a session is not cached', event: { dataRequest: true, sessionCookie: signedIn }, res: jsonResponse(), want: uncached },
  { name: 'a data request that sets a cookie is not cached', event: { dataRequest: true }, res: jsonResponse(200, SET_COOKIE), want: uncached },
  { name: 'a degraded data response is not cached', event: { dataRequest: true, skip: true }, res: jsonResponse(), want: uncached },
  { name: 'a degraded page is not cached', event: { skip: true }, res: htmlResponse(200), want: uncached },
  { name: 'a data request 5xx is not cached', event: { dataRequest: true }, res: jsonResponse(500), want: uncached },
  { name: 'a flagged data 404 is cached like its page', event: { dataRequest: true, edgeCache404: true }, res: jsonResponse(404), want: DEFAULT_TTL },
  { name: 'a response that sets a cookie is never cached', event: {}, res: htmlResponse(200, SET_COOKIE), want: uncached }
];

describe('handle edge caching', () => {
  test.each(edgeCases)('$name', async ({ event, res, want }) => {
    const response = await handle({ event: makeEvent(event) as never, resolve: async () => res });
    const seen = Object.fromEntries(Object.keys(want).map((name) => [name, response.headers.get(name)]));
    expect(seen).toEqual(want);
    expect([...response.headers.values()].join(' ')).not.toContain('s-maxage');
  });

  test('a cacheable page is public for the CDN and revalidated by browsers', async () => {
    const response = await handle({ event: makeEvent({}) as never, resolve: async () => htmlResponse(200) });
    expect(response.headers.get('Cache-Control')).toBe('public, max-age=0');
  });

  test('an uncacheable page stays no-store', async () => {
    const response = await handle({ event: makeEvent({ routeId: '/(app)/commands' }) as never, resolve: async () => htmlResponse(200) });
    expect(response.headers.get('Cache-Control')).toBe('no-store');
  });
});

describe('anonymous public rate limit', () => {
  const status = async (event: Event) => (await handle({ event: event as never, resolve: async () => htmlResponse(200) })).status;

  async function drain(event: () => Event): Promise<void> {
    for (let i = 0; i < 120; i++) expect(await status(event())).toBe(200);
  }

  test('120 requests pass, the next is a 429 with Retry-After and no-store', async () => {
    const event = () => makeEvent({ ip: '203.0.113.7' });
    await drain(event);
    const res = await handle({ event: event() as never, resolve: async () => htmlResponse(200) });
    expect({ status: res.status, retry: Number(res.headers.get('Retry-After')) > 0, cache: res.headers.get('Cache-Control') }).toEqual({
      status: 429,
      retry: true,
      cache: 'no-store'
    });
  });

  test('another client IP is not affected', async () => {
    expect(await status(makeEvent({ ip: '203.0.113.8' }))).toBe(200);
  });

  test('data requests are counted too', async () => {
    const event = () => makeEvent({ ip: '203.0.113.9', dataRequest: true });
    await drain(event);
    expect(await status(event())).toBe(429);
  });

  test('a missing CF-Connecting-IP falls back to the socket address', async () => {
    const event = () => makeEvent({ socket: '198.51.100.5' });
    await drain(event);
    expect(await status(event())).toBe(429);
  });

  test('unlisted routes are never limited', async () => {
    for (let i = 0; i < 130; i++) expect(await status(makeEvent({ ip: '203.0.113.10', routeId: '/healthz' }))).toBe(200);
  });

  test('non-GET requests are not counted', async () => {
    for (let i = 0; i < 130; i++) expect(await status(makeEvent({ ip: '203.0.113.11', method: 'POST' }))).toBe(200);
  });
});

describe('edge cache and Set-Cookie', () => {
  test('a garbage session cookie carries a delete Set-Cookie: never cached, even with locals.session null', () => {
    const res = htmlResponse(200, { 'set-cookie': 'bagel_session=; Max-Age=0; Path=/' });
    expect(edgeCacheHeaders(makeEvent({ routeId: CHANNEL }), res)).toBeNull();
  });

  test('a response with Set-Cookie is left uncached', () => {
    const res = htmlResponse(200, { 'set-cookie': 'bagel_session=; Max-Age=0; Path=/' });
    applyEdgeCache(makeEvent({ routeId: CHANNEL }), res);
    expect(res.headers.get('CDN-Cache-Control')).toBeNull();
    expect(res.headers.get('Vary')).toBeNull();
  });
});
