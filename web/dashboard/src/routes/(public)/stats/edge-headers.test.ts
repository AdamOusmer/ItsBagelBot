// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';

mock.module('#lib/server/public-stats', () => ({ publicStats: async () => ({ degraded: false }) }));
mock.module('#lib/server/public-boards', () => ({ publicBoards: async () => ({ degraded: false }) }));

const { GET: statsGet } = await import('./data/+server');
const { GET: boardsGet } = await import('./boards/+server');

const handlers = [
  ['data', statsGet, 'max-age=1, stale-if-error=60'],
  ['boards', boardsGet, 'max-age=1, stale-while-revalidate=2, stale-if-error=60']
] as [string, () => Response | Promise<Response>, string][];

for (const [name, get, cdn] of handlers) {
  test(`${name} uses its declared edge freshness without cookies`, async () => {
    const res = await get();
    expect({
      browser: res.headers.get('cache-control'),
      edge: res.headers.get('cdn-cache-control'),
      cookie: res.headers.get('set-cookie')
    }).toEqual({ browser: 'public, max-age=0', edge: cdn, cookie: null });
  });
}
