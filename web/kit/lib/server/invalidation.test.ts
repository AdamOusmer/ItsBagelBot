// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { registerServerConfig } from './config';

process.env.NEW_RELIC_ENABLED = 'false';
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

let deliver: (subject: string, data: Uint8Array) => void = () => {};
const nats = await import('./nats');
mock.module('./nats', () => ({
  ...nats,
  subscribeDurable: (_subject: string, onMessage: typeof deliver) => {
    deliver = onMessage;
  }
}));

const { startInvalidationBus } = await import('./invalidation');
const { SwrCache } = await import('./cache');

const applied: unknown[] = [];
const cache = new SwrCache();
startInvalidationBus({
  cache,
  scopes: { '*': () => [], rename: (id, keys) => [`state:${id}`, ...keys], modules: (id) => [`modules:${id}`] },
  onApplied: (scope, id, prefixes, keys) => applied.push({ scope, id, prefixes, keys })
});

test.each([
  { id: '42', expected: ['reloaded', 'cached', 'cached'] },
  { id: '*', expected: ['reloaded', 'reloaded', 'cached'] }
])('a modules invalidation for $id respects wildcard family boundaries', async ({ id, expected }) => {
  const keys = ['modules:42', 'modules:7', 'commands:42'];
  for (const key of keys) cache.set(key, 'cached', 60_000);
  send('inv.modules', { broadcaster_id: id });
  expect(await Promise.all(keys.map((key) => cache.getOrLoad(key, 60_000, async () => 'reloaded')))).toEqual(expected);
});

const send = (subject: string, body: unknown) => deliver(subject, new TextEncoder().encode(JSON.stringify(body)));

test('keys from the message reach the scope route and the applied callback', () => {
  applied.length = 0;
  send('inv.rename', { broadcaster_id: '42', keys: ['new', 'old'] });
  expect(applied).toEqual([{ scope: 'rename', id: '42', prefixes: ['state:42', 'new', 'old'], keys: ['new', 'old'] }]);
});

test('absent, malformed, oversized and surplus keys are dropped', () => {
  applied.length = 0;
  send('inv.rename', { broadcaster_id: '42' });
  send('inv.rename', { broadcaster_id: '42', keys: 'new' });
  send('inv.rename', { broadcaster_id: '42', keys: [1, '', 'x'.repeat(65), 'ok'] });
  send('inv.rename', { broadcaster_id: '42', keys: Array.from({ length: 20 }, (_, i) => `k${i}`) });
  expect(applied.map((a) => (a as { keys: string[] }).keys)).toEqual([[], [], ['ok'], Array.from({ length: 8 }, (_, i) => `k${i}`)]);
});
