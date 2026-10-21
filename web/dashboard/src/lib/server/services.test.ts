// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, setSystemTime, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/environment', () => ({ dev: false }));
registerServerConfig({ cacheInvalidationPrefix: 'inv' });

let deliver: (subject: string, data: Uint8Array) => void = () => {};
const rpcCalls: string[] = [];
const published: { subject: string; payload: unknown }[] = [];
const NOW = Date.UTC(2026, 9, 18, 12, 0, 0);
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string) => {
    rpcCalls.push(subject);
    if (subject.endsWith('.notifications.list')) {
      return { notifications: [{ id: 1, expires_at: new Date(NOW + 30_000).toISOString() }], unread_count: 1 };
    }
    return { active: true, status: 'paid', locale: 'fr', commands_page_hidden: true, expires_at: '2026-12-01' };
  },
  publish: async (subject: string, payload: unknown) => {
    published.push({ subject, payload });
  },
  subscribeDurable: (_subject: string, onMessage: typeof deliver) => {
    deliver = onMessage;
  }
}));

const liveHub = await import('./live-hub');
const services = await import('./services');
const { fabric, startInvalidationListener } = services;
startInvalidationListener();

const cached = (key: string) => fabric.readKey(key, 60_000, async () => 'reloaded');

function invalidation(scope: string, broadcasterId: string): void {
  deliver(`inv.${scope}`, new TextEncoder().encode(JSON.stringify({ broadcaster_id: broadcasterId })));
}

const KEYS = ['state:42', 'stream-meta:42', 'commands:42', 'notifications:42', 'notifications:7'];

beforeEach(() => {
  fabric.cache.clear();
  rpcCalls.length = 0;
  for (const key of KEYS) fabric.cache.set(key, 'cached', 60_000);
});

async function survivors(): Promise<string[]> {
  const kept: string[] = [];
  for (const key of KEYS) if ((await cached(key)) === 'cached') kept.push(key);
  return kept;
}

test.each([
  { scope: 'commands_page', id: '42', kept: ['stream-meta:42', 'commands:42', 'notifications:42', 'notifications:7'] },
  { scope: 'leaderboard_page', id: '42', kept: ['stream-meta:42', 'commands:42', 'notifications:42', 'notifications:7'] },
  { scope: 'locale', id: '7', kept: KEYS },
  { scope: 'live', id: '42', kept: ['state:42', 'commands:42', 'notifications:42', 'notifications:7'] },
  { scope: 'token-warm', id: '42', kept: KEYS },
  { scope: 'outgress', id: '42', kept: KEYS },
  { scope: 'loyalty', id: '42', kept: KEYS },
  { scope: 'notifications', id: '*', kept: ['state:42', 'stream-meta:42', 'commands:42'] },
  { scope: 'notifications', id: '42', kept: ['state:42', 'stream-meta:42', 'commands:42', 'notifications:7'] },
  { scope: 'unmapped_scope', id: '42', kept: ['stream-meta:42', 'notifications:42', 'notifications:7'] }
])('a $scope event for $id evicts only its keys', async ({ scope, id, kept }) => {
  invalidation(scope, id);
  expect(await survivors()).toEqual([...kept]);
});

test.each([
  { scope: 'commands', notified: true },
  { scope: 'live', notified: false },
  { scope: 'token-warm', notified: false },
  { scope: 'outgress', notified: false }
])('a $scope event refreshes open tabs: $notified', ({ scope, notified }) => {
  const seen: string[] = [];
  const stop = liveHub.subscribe('42', (s) => seen.push(s));
  invalidation(scope, '42');
  stop();
  expect(seen).toEqual(notified ? [scope] : []);
});

test('account, billing and preference reads share one state_get per board', async () => {
  fabric.cache.clear();
  const [account, billing, locale, commandsPage] = await Promise.all([
    services.accountState('42'),
    services.billingState('42'),
    services.userLocale('42'),
    services.userCommandsPage('42')
  ]);
  expect(rpcCalls).toEqual(['bagel.rpc.dashboard.state_get']);
  expect([account.status, billing.expiresAt, locale, commandsPage]).toEqual(['paid', '2026-12-01', 'fr', false]);
});

test('a preference write evicts the shared state for that board', async () => {
  fabric.cache.clear();
  await services.userLocale('42');
  await services.setLocale('42', 'de');
  await services.accountState('42');
  expect(rpcCalls).toEqual(['bagel.rpc.dashboard.state_get', 'bagel.rpc.dashboard.locale_set', 'bagel.rpc.dashboard.state_get']);
});

test('a fetches event evicts that board fetch list without refreshing open tabs', async () => {
  fabric.cache.set('fetches:42', 'cached', 60_000);
  const seen: string[] = [];
  const stop = liveHub.subscribe('42', (scope) => seen.push(scope));
  invalidation('fetches', '42');
  stop();
  expect([await cached('fetches:42'), await cached('state:42'), seen]).toEqual(['reloaded', 'cached', []]);
});

test('broadcastInvalidation evicts here and announces the scope to other pods', async () => {
  published.length = 0;
  fabric.cache.set('fetches:42', 'cached', 60_000);
  await services.broadcastInvalidation({ scope: 'fetches', id: '42' });
  expect(await cached('fetches:42')).toBe('reloaded');
  expect(published).toEqual([{ subject: 'inv.fetches', payload: { broadcaster_id: '42' } }]);
});

test('cached notifications reload once the earliest one expires', async () => {
  fabric.cache.clear();
  setSystemTime(new Date(NOW));
  await services.notificationsForUser('42');
  setSystemTime(new Date(NOW + 29_000));
  await services.notificationsForUser('42');
  expect(rpcCalls).toEqual(['bagel.rpc.notifications.list']);
  setSystemTime(new Date(NOW + 30_000));
  await services.notificationsForUser('42');
  expect(rpcCalls).toEqual(['bagel.rpc.notifications.list', 'bagel.rpc.notifications.list']);
  setSystemTime();
});

