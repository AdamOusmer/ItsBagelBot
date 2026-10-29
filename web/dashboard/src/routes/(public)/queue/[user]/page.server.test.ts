// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';
import { RpcError } from '@bagel/kit/server/nats';

stubSvelteKit();

type View = { open: boolean; total: number; entries: { login: string; joinedAt: number }[]; degraded: boolean };

let moduleEnabled: () => Promise<boolean> = async () => true;
let resolveReply: () => Promise<{ userId: string; username: string; displayName?: string }> = async () => ({ userId: '42', username: 'somechannel' });
let queueView: View = { open: true, total: 2, entries: [{ login: 'first', joinedAt: 1000 }, { login: 'second', joinedAt: 2000 }], degraded: false };
const queueReads: string[] = [];
const moduleReads: string[] = [];

mock.module('$lib/server/services', () => ({ resolveLogin: () => resolveReply() }));
mock.module('$lib/server/module-blob', () => ({
  readModuleBlob: async (userId: string, modId: string) => {
    moduleReads.push(`${userId}:${modId}`);
    return { enabled: await moduleEnabled(), configs: {} };
  }
}));
mock.module('$lib/server/queue-store', () => ({
  readQueue: async (userId: string) => {
    queueReads.push(userId);
    return queueView;
  }
}));

const { load } = await import('./+page.server');
const { GET } = await import('./data/+server');
const { QUEUE_EDGE_HEADERS, QUEUE_UNCACHED_HEADERS } = await import('./edge-headers');

type Outcome = { status?: number; message?: string; location?: string; edge?: unknown; page?: Record<string, unknown>; headers?: Headers; body?: Record<string, unknown> };

function arrange(setup: { enabled?: () => Promise<boolean>; resolve?: typeof resolveReply; view?: Partial<View> } = {}): void {
  moduleEnabled = setup.enabled ?? (async () => true);
  resolveReply = setup.resolve ?? (async () => ({ userId: '42', username: 'somechannel' }));
  queueView = { open: true, total: 2, entries: [{ login: 'first', joinedAt: 1000 }, { login: 'second', joinedAt: 2000 }], degraded: false, ...setup.view };
  queueReads.length = 0;
  moduleReads.length = 0;
}

function failure(e: unknown, locals: Record<string, unknown>): Outcome {
  const err = e as { status?: number; location?: string; body?: { message?: string } };
  return { status: err.status, message: err.body?.message, location: err.location, edge: locals.edgeOutcome };
}

async function loadPage(user = 'somechannel', host = 'queue.itsbagelbot.com', path = `/${user}`): Promise<Outcome> {
  const locals: Record<string, unknown> = { locale: 'en' };
  try {
    const page = (await load({ params: { user }, url: new URL(`https://${host}${path}`), locals } as never)) as unknown as Record<string, unknown>;
    return { page, edge: locals.edgeOutcome };
  } catch (e) {
    return failure(e, locals);
  }
}

async function loadData(user = 'somechannel', host = 'queue.itsbagelbot.com'): Promise<Outcome> {
  const locals: Record<string, unknown> = { locale: 'en' };
  try {
    const res = await GET({ params: { user }, url: new URL(`https://${host}/${user}/data`), locals } as never);
    return { headers: res.headers, body: (await res.json()) as Record<string, unknown> };
  } catch (e) {
    return failure(e, locals);
  }
}

const notFound = { status: 404, message: 'Channel not found', edge: 'hidden' };

describe('(public)/queue/[user] load', () => {
  test('a request off the queue host 404s as missing without reading anything', async () => {
    arrange();
    expect(await loadPage('somechannel', 'leaderboard.itsbagelbot.com')).toEqual({ status: 404, message: 'Not found', edge: 'missing' });
    expect(queueReads).toEqual([]);
  });

  test('a malformed login 404s as missing', async () => {
    arrange();
    expect(await loadPage('not-a-login')).toEqual({ status: 404, message: 'Channel not found', edge: 'missing' });
  });

  test('an unknown login 404s as missing', async () => {
    arrange({ resolve: async () => Promise.reject(new RpcError('no such channel', 'not_found')) });
    expect(await loadPage()).toEqual({ status: 404, message: 'Channel not found', edge: 'missing' });
    expect(queueReads).toEqual([]);
  });

  test('a failed lookup 404s without an edge outcome so it is never cached', async () => {
    arrange({ resolve: async () => Promise.reject(new Error('users service down')) });
    expect(await loadPage()).toEqual({ status: 404, message: 'Channel not found', edge: undefined });
  });

  test('a disabled Play Queue module 404s edge-cached as hidden', async () => {
    arrange({ enabled: async () => false });
    expect(await loadPage()).toEqual(notFound);
    expect(moduleReads).toEqual(['42:queue']);
  });

  test('a mixed-case or at-prefixed login redirects to the canonical path', async () => {
    arrange();
    const out = await loadPage('@SomeChannel', 'queue.itsbagelbot.com', '/@SomeChannel');
    expect({ status: out.status, location: out.location }).toEqual({ status: 308, location: '/somechannel' });
    expect(queueReads).toEqual([]);
  });

  test('the redirect keeps the internal route prefix in front of the login', async () => {
    arrange();
    const out = await loadPage('SomeChannel', 'queue.itsbagelbot.com', '/queue/SomeChannel?lang=fr');
    expect(out.location).toBe('/queue/somechannel?lang=fr');
  });

  test('a live queue renders the snapshot under a shareable edge outcome', async () => {
    arrange({ resolve: async () => ({ userId: '42', username: 'somechannel', displayName: 'SomeChannel' }) });
    const out = await loadPage();
    expect(out.edge).toBe('page');
    expect(out.page).toMatchObject({
      login: 'somechannel',
      channelName: 'SomeChannel',
      snapshot: { open: true, total: 2, degraded: false, entries: [{ login: 'first', joinedAt: 1000 }, { login: 'second', joinedAt: 2000 }] }
    });
    expect(typeof (out.page?.snapshot as { at: number }).at).toBe('number');
    expect(queueReads).toEqual(['42']);
  });

  test('an unreadable line renders a degraded page that the edge skips', async () => {
    arrange({ view: { open: false, total: 0, entries: [], degraded: true } });
    const out = await loadPage();
    expect(out.edge).toBe('skip');
    expect(out.page).toMatchObject({ snapshot: { degraded: true, entries: [] } });
  });

  test('an unreadable module flag fails open and renders without caching', async () => {
    arrange({ enabled: async () => Promise.reject(new Error('commands service blip')) });
    const out = await loadPage();
    expect({ edge: out.edge, entries: (out.page?.snapshot as { entries: unknown[] }).entries.length }).toEqual({ edge: 'skip', entries: 2 });
  });
});

describe('(public)/queue/[user]/data GET', () => {
  test('a live queue is edge-cacheable for a moment with stale-while-revalidate and no s-maxage', async () => {
    arrange();
    const out = await loadData();
    expect(out.body).toMatchObject({ open: true, total: 2, degraded: false });
    expect(out.headers?.get('cache-control')).toBe(QUEUE_EDGE_HEADERS['cache-control']);
    const cdn = out.headers?.get('cdn-cache-control') ?? '';
    expect(cdn).toContain('max-age=2');
    expect(cdn).toContain('stale-while-revalidate=');
    expect(`${out.headers?.get('cache-control')} ${cdn}`).not.toContain('s-maxage');
  });

  test('a degraded read is never edge-cached', async () => {
    arrange({ view: { open: false, total: 0, entries: [], degraded: true } });
    const out = await loadData();
    expect(out.headers?.get('cache-control')).toBe(QUEUE_UNCACHED_HEADERS['cache-control']);
    expect(out.headers?.get('cdn-cache-control')).toBeNull();
  });

  test('the data path 404s for a disabled module, an unknown login and a foreign host', async () => {
    arrange({ enabled: async () => false });
    expect((await loadData()).status).toBe(404);
    arrange({ resolve: async () => Promise.reject(new RpcError('no such channel', 'not_found')) });
    expect((await loadData()).status).toBe(404);
    arrange();
    expect((await loadData('somechannel', 'commands.itsbagelbot.com')).status).toBe(404);
  });
});
