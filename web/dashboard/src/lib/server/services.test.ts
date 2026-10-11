// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/environment', () => ({ dev: false }));
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

let deliver: (subject: string, data: Uint8Array) => void = () => {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  subscribeDurable: (_subject: string, onMessage: typeof deliver) => {
    deliver = onMessage;
  }
}));

const { fabric, startInvalidationListener } = await import('./services');
startInvalidationListener();

const cached = (key: string) => fabric.readKey(key, 60_000, async () => 'reloaded');

function invalidation(scope: string, broadcasterId: string): void {
  deliver(`inv.${scope}`, new TextEncoder().encode(JSON.stringify({ broadcaster_id: broadcasterId })));
}

beforeEach(() => {
  fabric.cache.set('commands_page:42', 'cached', 60_000);
  fabric.cache.set('leaderboard_page:42', 'cached', 60_000);
});

test.each([
  { name: 'a commands_page event evicts that channel only', scope: 'commands_page', id: '42', key: 'commands_page:42', want: 'reloaded' },
  { name: 'an event for another channel keeps it', scope: 'commands_page', id: '7', key: 'commands_page:42', want: 'cached' },
  { name: 'a coarse flush evicts it too', scope: 'unmapped_scope', id: '42', key: 'commands_page:42', want: 'reloaded' }
])('commands page flag cache: $name', async ({ scope, id, key, want }) => {
  invalidation(scope, id);
  expect(await cached(key)).toBe(want);
});

test.each([
  { name: 'a leaderboard_page event evicts that channel only', scope: 'leaderboard_page', id: '42', want: 'reloaded' },
  { name: 'an event for another channel keeps it', scope: 'leaderboard_page', id: '7', want: 'cached' },
  { name: 'a commands_page event leaves it alone', scope: 'commands_page', id: '42', want: 'cached' },
  { name: 'a coarse flush evicts it too', scope: 'unmapped_scope', id: '42', want: 'reloaded' }
])('leaderboard page flag cache: $name', async ({ scope, id, want }) => {
  invalidation(scope, id);
  expect(await cached('leaderboard_page:42')).toBe(want);
});
