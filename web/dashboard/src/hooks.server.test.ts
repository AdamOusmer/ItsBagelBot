// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync } from 'node:fs';
import { join, relative, sep } from 'node:path';
import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../test/sveltekit';

process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
stubSvelteKit();
let guardImpl: (session: unknown) => Promise<unknown> = async (session) => session;
mock.module('#lib/server/guard', () => ({ guardSession: (_event: unknown, session: unknown) => guardImpl(session) }));

const { COOKIE, CURSOR_COOKIE, seal } = await import('./lib/server/session');
const { LOCALE_COOKIE } = await import('@bagel/kit/i18n');
const { EDGE_CACHE, applyEdgeCache, edgeCacheHeaders, handle } = await import('./hooks.server');
const { redirect } = await import('@sveltejs/kit');
const { CHANNEL_ROUTE_NODES, URL_PURGED_ROUTES } = await import('./lib/server/channel-urls');

type Event = Parameters<typeof edgeCacheHeaders>[0];
type EventOptions = {
  routeId?: string;
  session?: unknown;
  edgeOutcome?: string;
  edgeCacheTag?: string;
  search?: string;
  isDataRequest?: boolean;
  lang?: string;
  cursorCookie?: string;
  localeCookie?: string;
  sessionCookie?: string;
  locale?: string;
};

const LOGIN = '/(public)/login';
const CHANNEL = '/(public)/user/[channel]';
const LEADERBOARD = '/(public)/[user]';
const QUEUE = '/(public)/queue/[user]';
const SET_COOKIE = { 'set-cookie': 'bagel_session=; Max-Age=0; Path=/' };

function makeEvent(opts: EventOptions): Event {
  const cookies: Record<string, string | undefined> = {
    [CURSOR_COOKIE]: opts.cursorCookie,
    [LOCALE_COOKIE]: opts.localeCookie,
    [COOKIE]: opts.sessionCookie
  };
  const search = opts.search ?? (opts.lang ? `?lang=${opts.lang}` : opts.isDataRequest ? '?x-sveltekit-invalidated=001' : '');
  const url = new URL(`https://commands.itsbagelbot.com/user/foo${search}`);
  const requested = new URL(`https://commands.itsbagelbot.com/user/foo${opts.isDataRequest ? '/__data.json' : ''}${search}`);
  return {
    route: { id: opts.routeId ?? CHANNEL },
    request: new Request(requested),
    url,
    isDataRequest: opts.isDataRequest ?? false,
    locals: { session: opts.session ?? null, locale: opts.locale ?? 'en', edgeOutcome: opts.edgeOutcome, edgeCacheTag: opts.edgeCacheTag },
    cookies: { get: (name: string) => cookies[name], set: () => {}, delete: () => {} }
  } as unknown as Event;
}

function htmlResponse(status: number, headers: Record<string, string> = {}): Response {
  return new Response('<html></html>', { status, headers: { 'content-type': 'text/html', ...headers } });
}

type DataReply = { status?: number; contentType?: string; headers?: Record<string, string> };

function dataResponse({ status = 200, contentType = 'application/json', headers = {} }: DataReply = {}): Response {
  return new Response('{"type":"data","nodes":[]}', {
    status,
    headers: { 'content-type': contentType, 'cache-control': 'private, no-store', ...headers }
  });
}

function routeIds(dir: string, root = dir): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (entry.isDirectory()) return routeIds(join(dir, entry.name), root);
    const isRoute = entry.name === '+page.svelte' || entry.name === '+page.server.ts';
    return isRoute ? ['/' + relative(root, dir).split(sep).join('/')] : [];
  });
}

test('every URL-purged route has the node count its page-data masks assume', () => {
  const routes = join(import.meta.dir, 'routes');
  const layoutsOn = (id: string) =>
    ['', ...id.split('/').filter(Boolean).slice(0, -1)].reduce<{ dir: string; n: number }>(
      (acc, seg) => {
        const dir = seg ? join(acc.dir, seg) : acc.dir;
        const hasLayout = readdirSync(dir).some((f) => f.startsWith('+layout.'));
        return { dir, n: acc.n + (hasLayout ? 1 : 0) };
      },
      { dir: routes, n: 0 }
    ).n;
  expect([...URL_PURGED_ROUTES].map((id) => layoutsOn(id) + 1)).toEqual([...URL_PURGED_ROUTES].map(() => CHANNEL_ROUTE_NODES));
});

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
const CHANNEL_TTL = 'max-age=3600, stale-while-revalidate=3600, stale-if-error=86400';
const DEFAULT_TTL = cached(CHANNEL_TTL);
const SHORT_TTL = cached('max-age=60, stale-while-revalidate=60, stale-if-error=86400');
const PAGE = { edgeOutcome: 'page' };
const BOARD = { ...PAGE, routeId: LEADERBOARD, edgeCacheTag: 'lb:foo' };
const LEADERBOARD_TTL = {
  ...cached('max-age=30, stale-while-revalidate=30, stale-if-error=86400'),
  'Cache-Tag': 'dashboard-edge-shell,lb:foo'
};

const edgeCases: { name: string; event: EventOptions; res: Response; want: Record<string, string | null> }[] = [
  { name: 'a hidden-channel 404 gets the route TTL', event: { edgeOutcome: 'hidden' }, res: htmlResponse(404), want: DEFAULT_TTL },
  { name: 'an unknown-channel 404 gets the short TTL', event: { edgeOutcome: 'missing' }, res: htmlResponse(404), want: SHORT_TTL },
  { name: 'a degraded or unverified page is never cached', event: { edgeOutcome: 'skip' }, res: htmlResponse(200), want: uncached },
  { name: 'a 404 without an outcome stays no-store', event: {}, res: htmlResponse(404), want: uncached },
  { name: 'a 404 for a rendered page outcome stays no-store', event: { edgeOutcome: 'page' }, res: htmlResponse(404), want: uncached },
  { name: 'a hidden 404 on a signed-in request: still no-store', event: { edgeOutcome: 'hidden', sessionCookie: signedIn }, res: htmlResponse(404), want: uncached },
  { name: 'anonymous page data shares the page TTL', event: { ...PAGE, isDataRequest: true }, res: dataResponse(), want: DEFAULT_TTL },
  { name: 'every enumerated invalidation mask is shareable', event: { ...PAGE, isDataRequest: true, search: '?x-sveltekit-invalidated=111' }, res: dataResponse(), want: DEFAULT_TTL },
  { name: 'a mask the purge does not enumerate is never cached', event: { ...PAGE, isDataRequest: true, search: '?x-sveltekit-invalidated=10' }, res: dataResponse(), want: uncached },
  { name: 'a trailing-slash data variant is never cached', event: { ...PAGE, isDataRequest: true, search: '?x-sveltekit-invalidated=001&x-sveltekit-trailing-slash=1' }, res: dataResponse(), want: uncached },
  { name: 'a channel page with any query string is never cached', event: { ...PAGE, search: '?utm_source=x' }, res: htmlResponse(200), want: uncached },
  { name: 'a 404 with a query string is never cached', event: { edgeOutcome: 'missing', search: '?x=1' }, res: htmlResponse(404), want: uncached },
  { name: 'routes outside URL purge keep their query variants', event: { routeId: '/(public)/stats', search: '?ref=x' }, res: htmlResponse(200), want: cached('max-age=30, stale-while-revalidate=300, stale-if-error=86400') },
  { name: 'hidden-channel data shares the hidden TTL', event: { edgeOutcome: 'hidden', isDataRequest: true }, res: dataResponse(), want: DEFAULT_TTL },
  { name: 'data from a load that recorded no outcome is never cached', event: { isDataRequest: true }, res: dataResponse(), want: uncached },
  { name: 'skipped page data is never cached', event: { edgeOutcome: 'skip', isDataRequest: true }, res: dataResponse(), want: uncached },
  { name: 'streamed page data is never cached', event: { ...PAGE, isDataRequest: true }, res: dataResponse({ contentType: 'text/sveltekit-data' }), want: uncached },
  { name: 'page data answered with html is not cached', event: { ...PAGE, isDataRequest: true }, res: htmlResponse(200), want: uncached },
  { name: 'page data with a locale cookie is not cached', event: { ...PAGE, isDataRequest: true, localeCookie: 'fr' }, res: dataResponse(), want: uncached },
  { name: 'page data that sets a cookie is not cached', event: { ...PAGE, isDataRequest: true }, res: dataResponse({ headers: SET_COOKIE }), want: uncached },
  { name: 'a page data 5xx is not cached', event: { ...PAGE, isDataRequest: true }, res: dataResponse({ status: 500 }), want: uncached },
  { name: 'leaderboard: any board page is shared under its channel tag', event: { ...BOARD, search: '?page=40' }, res: htmlResponse(200), want: LEADERBOARD_TTL },
  { name: 'leaderboard: page data is shared under its channel tag', event: { ...BOARD, isDataRequest: true, search: '?page=2&x-sveltekit-invalidated=001' }, res: dataResponse(), want: LEADERBOARD_TTL },
  { name: 'leaderboard: search pages are shared under their channel tag', event: { ...BOARD, search: '?q=fan' }, res: htmlResponse(200), want: LEADERBOARD_TTL },
  { name: 'leaderboard: search page data is shared under its channel tag', event: { ...BOARD, isDataRequest: true, search: '?q=fan&x-sveltekit-invalidated=001' }, res: dataResponse(), want: LEADERBOARD_TTL },
  { name: 'leaderboard: a hidden board 404 carries its channel tag', event: { ...BOARD, edgeOutcome: 'hidden' }, res: htmlResponse(404), want: LEADERBOARD_TTL },
  { name: 'leaderboard: a busy or degraded board is never cached', event: { ...BOARD, edgeOutcome: 'skip' }, res: htmlResponse(200), want: uncached },
  { name: 'queue: the live page is shared for a few seconds', event: { ...PAGE, routeId: QUEUE }, res: htmlResponse(200), want: cached('max-age=5, stale-while-revalidate=5, stale-if-error=86400') },
  { name: 'queue: a disabled-module 404 shares the same short TTL', event: { edgeOutcome: 'hidden', routeId: QUEUE }, res: htmlResponse(404), want: cached('max-age=5, stale-while-revalidate=5, stale-if-error=86400') },
  { name: 'queue: a degraded line is never cached', event: { edgeOutcome: 'skip', routeId: QUEUE }, res: htmlResponse(200), want: uncached },
  { name: 'signed-in page data is never cached', event: { ...PAGE, isDataRequest: true, sessionCookie: signedIn }, res: dataResponse(), want: uncached },
  { name: 'a plain 200 emits CDN-only directives so browsers revalidate', event: {}, res: htmlResponse(200), want: DEFAULT_TTL },
  { name: 'login uses its own TTL', event: { routeId: LOGIN }, res: htmlResponse(200), want: cached('max-age=600, stale-while-revalidate=86400, stale-if-error=86400') },
  { name: 'stats uses its own TTL', event: { routeId: '/(public)/stats' }, res: htmlResponse(200), want: cached('max-age=30, stale-while-revalidate=300, stale-if-error=86400') },
  { name: 'unlisted routes stay no-store', event: { routeId: '/(app)/commands' }, res: htmlResponse(200), want: uncached },
  { name: 'a locale cookie bypasses the cache', event: { localeCookie: 'fr' }, res: htmlResponse(200), want: uncached },
  { name: 'a lang param bypasses the cache', event: { lang: 'fr' }, res: htmlResponse(200), want: uncached },
  { name: 'the cursor-off cookie bypasses the cache', event: { cursorCookie: '0' }, res: htmlResponse(200), want: uncached },
  { name: 'non-html page responses are not cached', event: PAGE, res: new Response('{}', { headers: { 'content-type': 'application/json' } }), want: uncached },
  { name: 'a response that sets a cookie is never cached', event: {}, res: htmlResponse(200, SET_COOKIE), want: uncached }
];

describe('edge cache locale bucket', () => {
  const emits = (event: EventOptions, res: Response) => edgeCacheHeaders(makeEvent(event), res) !== null;

  test.each(['en', 'fr'])('a %s render gets CDN headers', (locale) => {
    expect(emits({ locale }, htmlResponse(200))).toBe(true);
    expect(emits({ locale, ...PAGE, isDataRequest: true }, dataResponse())).toBe(true);
  });

  test.each(['de', 'ru', 'es', 'pt-br'])('a %s render gets no CDN headers', (locale) => {
    expect(emits({ locale }, htmlResponse(200))).toBe(false);
    expect(emits({ locale, ...PAGE, isDataRequest: true }, dataResponse())).toBe(false);
    expect(emits({ locale, edgeOutcome: 'hidden' }, htmlResponse(404))).toBe(false);
  });
});

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

test.each(['/(app)/discord', '/(app)/discord/[guildId]/settings', '/(app)/discord/connect', '/(app)/discord/callback'])(
  'offline route %s answers 404 before any load or endpoint runs',
  async (routeId) => {
    let resolved = false;
    const run = handle({
      event: makeEvent({ routeId }) as never,
      resolve: async () => {
        resolved = true;
        return htmlResponse(200);
      }
    });
    await expect(run).rejects.toMatchObject({ status: 404 });
    expect(resolved).toBe(false);
  }
);

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

test('the readiness probe skips session, locale and hardening work', async () => {
  const url = new URL('https://dashboard.itsbagelbot.com/readyz');
  const deleted: string[] = [];
  const event = {
    route: { id: '/readyz' },
    request: new Request(url),
    url,
    locals: {},
    cookies: { get: () => 'garbage', set: () => {}, delete: (name: string) => deleted.push(name) }
  };
  const ok = new Response('ok');
  const response = await handle({ event: event as never, resolve: async () => ok });
  expect(response).toBe(ok);
  expect(deleted).toEqual([]);
  expect(event.locals).toEqual({});
});

describe('handle session admission', () => {
  function newUserEvents(): () => Event {
    const userId = crypto.randomUUID();
    const session = seal({
      user_id: userId,
      login: 'streamer',
      display_name: 'Streamer',
      role: 'streamer',
      sid: `s${userId}`,
      iat: Math.floor(Date.now() / 1000),
      expires_at: Math.floor(Date.now() / 1000) + 3600
    });
    const url = new URL('https://dashboard.itsbagelbot.com/auth/login');
    return () =>
      ({
        route: { id: '/auth/login' },
        request: new Request(url),
        url,
        isDataRequest: false,
        locals: {},
        cookies: { get: (name: string) => (name === COOKIE ? session : undefined), set: () => {}, delete: () => {} }
      }) as unknown as Event;
  }

  async function attempt(event: Event): Promise<number> {
    try {
      const res = await handle({ event: event as never, resolve: async () => htmlResponse(200) });
      return res.status;
    } catch (e) {
      return (e as { status: number }).status;
    }
  }

  test('a rate-limited request answers 429 even when the guard would redirect', async () => {
    let guarded = 0;
    guardImpl = async () => {
      guarded++;
      throw redirect(303, '/login?e=revoked');
    };
    try {
      const sameUser = newUserEvents();
      const statuses = await Promise.all(Array.from({ length: 11 }, () => attempt(sameUser())));
      expect({ redirected: statuses.filter((s) => s === 303).length, limited: statuses.filter((s) => s === 429).length, guarded }).toEqual({
        redirected: 10,
        limited: 1,
        guarded: 11
      });
    } finally {
      guardImpl = async (session) => session;
    }
  });

  test('an admitted request carries the guarded session', async () => {
    guardImpl = async (session) => ({ ...(session as object), sections: ['commands'] });
    try {
      let seen: unknown;
      await handle({
        event: newUserEvents()() as never,
        resolve: async (event: { locals: { session: unknown } }) => {
          seen = event.locals.session;
          return htmlResponse(200);
        }
      } as never);
      expect((seen as { sections?: string[] }).sections).toEqual(['commands']);
    } finally {
      guardImpl = async (session) => session;
    }
  });
});
