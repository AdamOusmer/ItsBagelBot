// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { POLICY } from '@bagel/kit/server/cache-keys';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/environment', () => ({ dev: false }));
mock.module('./edge-purge', () => ({ schedulePurgeChannel: () => {} }));

const modules = [{ name: 'queue', is_enabled: true, revision: 8, configs: {} },
  { name: 'loyalty', is_enabled: true, revision: 4, account_created_at: 100, configs: {} }];
let projected = modules;
let projectionFailure = false;
const calls: Array<{ subject: string; body: any }> = [];
const nats = await import('@bagel/kit/server/nats');
const { SUB, fabric: { cache } } = await import('./services');
const UPSERT = `${SUB.modules}.upsert`;
const REPLACE = `${SUB.projector}.modules.replace`;
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: any) => {
    calls.push({ subject, body });
    if (subject === UPSERT) return { modules };
    if (subject === `${SUB.modules}.patch`) return { modules, rev: 8, conflict: false };
    if (subject === REPLACE) {
      if (projectionFailure) throw new Error('projection unavailable');
      return { modules: projected };
    }
    throw new Error(`Unexpected RPC: ${subject}`);
  }
}));
const { upsertModule, patchModule } = await import('./commands-store');

beforeEach(() => {
  calls.length = 0;
  projectionFailure = false;
  projected = modules;
  cache.invalidate('modules:1001');
});

test('upsert projects persisted revisions and account metadata instead of guessing rows', async () => {
  cache.set('modules:1001', [{ name: 'queue', is_enabled: false, revision: 7 }], POLICY.projected);
  await upsertModule('1001', 'queue', true, {});
  expect(calls.map((call) => call.subject)).toEqual([UPSERT, REPLACE]);
  expect(calls[1].body.modules).toEqual(modules);
  expect(await cache.getOrLoad('modules:1001', POLICY.projected, async () => [] as typeof modules)).toEqual(modules);
});

test('projection readback keeps a newer accepted revision in the cache', async () => {
  projected = [{ name: 'queue', is_enabled: false, revision: 9, configs: {} }];
  await upsertModule('1001', 'queue', true, {});
  expect(await cache.getOrLoad('modules:1001', POLICY.projected, async () => [] as typeof modules)).toEqual(projected);
});

test('patch synchronizes its committed config instead of invalidating into an old projection', async () => {
  expect(await patchModule({ userId: '1001', name: 'queue', isEnabled: true, partial: {}, expectedRev: 7 }))
    .toEqual({ rev: 8, conflict: false });
  expect(calls[1].body.modules).toEqual(modules);
});

test('a projection outage retains the committed SQL value for immediate readback', async () => {
  projectionFailure = true;
  await upsertModule('1001', 'queue', true, {});
  expect(await cache.getOrLoad('modules:1001', POLICY.projected, async () => [] as typeof modules)).toEqual(modules);
});
