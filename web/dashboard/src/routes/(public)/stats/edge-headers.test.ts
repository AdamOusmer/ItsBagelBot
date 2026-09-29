// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';

mock.module('$lib/server/public-stats', () => ({ publicStats: async () => ({ degraded: false }) }));
mock.module('$lib/server/public-boards', () => ({ publicBoards: async () => ({ degraded: false }) }));

const { GET: statsGet } = await import('./data/+server');
const { GET: boardsGet } = await import('./boards/+server');

const handlers = [['data', statsGet], ['boards', boardsGet]] as [string, () => Response | Promise<Response>][];

for (const [name, get] of handlers) {
  test(`${name} is edge-cacheable with stale-while-revalidate and no s-maxage`, async () => {
    const res = await get();
    const cdn = res.headers.get('cdn-cache-control') ?? '';
    expect(res.headers.get('cache-control')).toBe('public, max-age=0');
    expect(cdn).toContain('max-age=2');
    expect(cdn).toContain('stale-while-revalidate=');
    expect(`${res.headers.get('cache-control')} ${cdn}`).not.toContain('s-maxage');
    expect(res.headers.get('set-cookie')).toBeNull();
  });
}
