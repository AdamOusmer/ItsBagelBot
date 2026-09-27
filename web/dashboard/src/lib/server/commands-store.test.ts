// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { SwrCache } from '@bagel/kit/server/cache';
import { POLICY } from '@bagel/kit/server/cache-keys';

const cache = new SwrCache();
const modules = [{ name: 'queue', is_enabled: true, revision: 8, configs: {} },
  { name: 'loyalty', is_enabled: true, revision: 4, account_created_at: 100, configs: {} }];
let projected = modules;
let projectionFailure = false;
const calls: Array<{ subject: string; body: any }> = [];
mock.module('newrelic', () => ({ default: { noticeError: () => {} } }));
mock.module('./services', () => ({
  SUB: { modules: 'modules', projector: 'projector' },
  fabric: { cache }, invalidate: (key: string) => cache.invalidate(key)
}));
mock.module('@bagel/kit/server/nats', () => ({
  rpc: async (subject: string, body: any) => {
    calls.push({ subject, body });
    if (subject === 'modules.upsert') return { modules };
    if (subject === 'modules.patch') return { modules, rev: 8, conflict: false };
    if (subject === 'projector.modules.replace') {
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
  expect(calls.map((call) => call.subject)).toEqual(['modules.upsert', 'projector.modules.replace']);
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
