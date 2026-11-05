// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stubSvelteKit } from '../../../test/sveltekit';

process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
process.env.NEW_RELIC_ENABLED = 'false';
stubSvelteKit();
mock.module('#lib/server/guard', () => ({ guardSession: async (_event: unknown, session: unknown) => session }));

const rows = new Map<string, { name: string; is_enabled: boolean; configs?: unknown }[]>();
let rpcDown = false;
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: { user_id: string; name?: string; is_enabled?: boolean; configs?: unknown; modules?: unknown }) => {
    if (rpcDown) throw new Error('down');
    if (subject.endsWith('.modules.list')) return { modules: rows.get(body.user_id) ?? [] };
    if (subject.endsWith('.modules.upsert')) {
      rows.set(body.user_id, [{ name: body.name!, is_enabled: body.is_enabled!, configs: body.configs }]);
      return { modules: rows.get(body.user_id) };
    }
    if (subject.endsWith('.modules.replace')) return { modules: body.modules };
    throw new Error(`Unexpected RPC: ${subject}`);
  }
}));

const { handle } = await import('../../hooks.server');
const page = await import('./[token]/+server');
const stream = await import('./[token]/stream/+server');
const settings = await import('../(app)/settings/overlay-token/+server');

const throwing = new Proxy({}, {
  get(_target, prop) {
    throw new Error(`route touched ${String(prop)}`);
  }
});

function overlayEvent(pathname: string, routeId: string) {
  return {
    url: new URL(`https://dashboard.itsbagelbot.com${pathname}`),
    request: new Request(`https://dashboard.itsbagelbot.com${pathname}`),
    route: { id: routeId },
    locals: throwing,
    cookies: throwing,
    isDataRequest: false
  } as never;
}

function pageGet(token: string): Promise<Response> {
  return page.GET({ params: { token }, locals: throwing, cookies: throwing } as never) as Promise<Response>;
}

function streamGet(token: string, signal?: AbortSignal): Promise<Response> {
  return stream.GET({
    params: { token },
    request: new Request('https://x/stream', { signal }),
    locals: throwing,
    cookies: throwing
  } as never) as Promise<Response>;
}

function settingsCall(method: 'GET' | 'POST', session: unknown, headers: Record<string, string> = { 'content-type': 'application/json' }) {
  const url = new URL('https://dashboard.itsbagelbot.com/settings/overlay-token');
  const init = method === 'POST' ? { method, headers, body: '{}' } : { method };
  const event = { locals: { session }, url, request: new Request(url, init) } as never;
  return (method === 'GET' ? settings.GET(event) : settings.POST(event)) as Promise<Response>;
}

async function mintedToken(channel = '7'): Promise<string> {
  const res = await settingsCall('POST', { user_id: channel });
  const { url } = (await res.json()) as { url: string };
  return url.split('/').at(-1)!;
}

beforeEach(() => {
  rows.clear();
  rpcDown = false;
});

describe('overlay routes', () => {
  test('hooks read no session, cookies or locale for a real overlay route', async () => {
    let resolved = false;
    const res = await handle({
      event: overlayEvent('/overlay/7.token', '/overlay/[token]'),
      resolve: async () => {
        resolved = true;
        return new Response('ok');
      }
    });
    expect(resolved).toBe(true);
    expect(res.headers.has('set-cookie')).toBe(false);
  });

  test('a good token serves the page and the stream without touching session state', async () => {
    const token = await mintedToken();
    const res = await pageGet(token);
    expect(res.status).toBe(200);
    expect(res.headers.get('content-security-policy')).toContain("default-src 'none'");
    expect(res.headers.has('set-cookie')).toBe(false);
    expect(await res.text()).not.toContain(token);

    const abort = new AbortController();
    const live = await streamGet(token, abort.signal);
    expect(live.status).toBe(200);
    expect(live.headers.get('content-type')).toBe('text/event-stream');
    expect(live.headers.has('set-cookie')).toBe(false);
    abort.abort();
  });

  test('a wrong token gets the same 404 on both routes', async () => {
    const token = await mintedToken();
    const flipped = `${token.slice(0, -1)}${token.endsWith('A') ? 'B' : 'A'}`;
    for (const bad of [flipped, `8.${token.split('.')[1]}`, 'garbage', '<script>']) {
      const [first, second] = [await pageGet(bad), await streamGet(bad)];
      expect([first.status, second.status]).toEqual([404, 404]);
      expect([await first.text(), await second.text()]).toEqual(['Not Found', 'Not Found']);
    }
  });

  test('a rotated token stops working at once, a new one works', async () => {
    const first = await mintedToken();
    const second = await mintedToken();
    expect((await pageGet(first)).status).toBe(404);
    expect((await pageGet(second)).status).toBe(200);
  });

  test('a store outage answers 503, not a false 404 or a pass', async () => {
    const token = await mintedToken();
    rpcDown = true;
    expect((await pageGet(token)).status).toBe(503);
    expect((await streamGet(token)).status).toBe(503);
  });

  test('neither route source reaches demo data or session helpers', () => {
    for (const path of ['[token]/+server.ts', '[token]/stream/+server.ts', '../../lib/server/overlay-hub.ts', '../../lib/server/overlay-page.ts', '../../lib/server/overlay-token.ts']) {
      const source = readFileSync(join(import.meta.dir, path), 'utf8');
      expect(source).not.toMatch(/demo-data|\bDEMO\b|locals\.|cookies\.|server\/session/);
    }
  });
});

describe('overlay token endpoint', () => {
  test('stores only a hash and returns the link once', async () => {
    const res = await settingsCall('POST', { user_id: '7' });
    expect(res.headers.get('cache-control')).toBe('no-store');
    const { url } = (await res.json()) as { url: string };
    expect(url).toMatch(/^https:\/\/dashboard\.itsbagelbot\.com\/overlay\/7\.[\w-]{43}$/);
    const stored = JSON.stringify(rows.get('7'));
    expect(stored).not.toContain(url.split('.').at(-1)!);
    expect(rows.get('7')![0]!.is_enabled).toBe(false);
    expect((await (await settingsCall('GET', { user_id: '7' })).json()) as unknown).toEqual({ configured: true });
    expect(await (await settingsCall('GET', { user_id: '8' })).json()).toEqual({ configured: false });
  });

  test('refuses signed-out, delegates and non-JSON posts', async () => {
    expect((await settingsCall('POST', null)).status).toBe(401);
    expect((await settingsCall('GET', null)).status).toBe(401);
    expect((await settingsCall('POST', { user_id: '9', delegate_of: '7' })).status).toBe(403);
    expect((await settingsCall('GET', { user_id: '9', delegate_of: '7' })).status).toBe(403);
    expect((await settingsCall('POST', { user_id: '7' }, { 'content-type': 'text/plain' })).status).toBe(415);
    expect(rows.size).toBe(0);
  });
});

test('robots keeps overlay links out of the one host that allows crawling by default', async () => {
  const source = readFileSync(join(import.meta.dir, '../robots.txt/+server.ts'), 'utf8');
  expect(source).toContain('Disallow: /overlay/');
});
