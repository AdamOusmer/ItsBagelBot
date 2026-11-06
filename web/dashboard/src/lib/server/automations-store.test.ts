// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { POLICY } from '@bagel/kit/server/cache-keys';
import type { Automation } from '@bagel/kit/automations';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/env', () => ({ dev: false }));

const calls: Array<{ subject: string; body: any }> = [];
let replies: Record<string, unknown> = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: any) => {
    calls.push({ subject, body });
    if (!(subject in replies)) throw new Error(`Unexpected RPC: ${subject}`);
    return replies[subject];
  }
}));

const { fabric } = await import('./services');
const store = await import('./automations-store');

const manual = { version: 1, trigger: { type: 'manual', params: {} }, actions: [] };
const sample = (id: string, revision = 1): Automation => ({
  id,
  user_id: 7,
  name: `auto ${id}`,
  enabled: true,
  revision,
  definition: manual,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
});

const MOD = 'bagel.rpc.modules.automation';

beforeEach(() => {
  calls.length = 0;
  replies = {};
  fabric.cache.invalidate('automations:7');
});

describe('automations store reads', () => {
  test('list hits the owner once and serves the cached rows afterwards', async () => {
    replies[`${MOD}.list`] = { automations: [sample('a')] };
    expect(await store.listAutomations('7')).toEqual([sample('a')]);
    expect(await store.listAutomations('7')).toEqual([sample('a')]);
    expect(calls).toEqual([{ subject: `${MOD}.list`, body: { user_id: '7' } }]);
  });

  test('a refusal throws so the page degrades instead of caching an empty list', async () => {
    replies[`${MOD}.list`] = { automations: [], error: 'down', code: 'unavailable' };
    await expect(store.listAutomations('7')).rejects.toThrow('down');
  });

  test('get maps not_found to null and bypasses the cache', async () => {
    replies[`${MOD}.get`] = { code: 'not_found' };
    expect(await store.getAutomation('7', 'x')).toBeNull();
    replies[`${MOD}.get`] = { automation: sample('x') };
    expect((await store.getAutomation('7', 'x'))?.id).toBe('x');
    expect(calls.map((c) => c.body)).toEqual([{ user_id: '7', id: 'x' }, { user_id: '7', id: 'x' }]);
  });

  test('runs asks the run log for the broadcaster, optionally one automation', async () => {
    replies['bagel.rpc.automations.runs'] = { runs: [{ run_id: 'r1' }] };
    expect(await store.listRuns('7', 'a')).toEqual([{ run_id: 'r1' }] as never);
    expect(calls[0].body).toEqual({ broadcaster_id: '7', automation_id: 'a', limit: 50 });
    await store.listRuns('7');
    expect(calls[1].body.automation_id).toBeUndefined();
  });
});

describe('automations store writes', () => {
  const input = { name: 'n', enabled: true, definition: manual };

  test('upsert sends the revision guard and evicts the list', async () => {
    fabric.cache.set('automations:7', [sample('old')], POLICY.entity);
    replies[`${MOD}.upsert`] = { automation: sample('a', 3) };
    const res = await store.upsertAutomation('7', { ...input, id: 'a', expectedRevision: 2 });
    expect(res).toEqual({ ok: true, automation: sample('a', 3) });
    expect(calls[0].body).toEqual({
      user_id: '7',
      id: 'a',
      name: 'n',
      enabled: true,
      definition: manual,
      expected_revision: 2
    });
    expect(await fabric.cache.getOrLoad('automations:7', POLICY.entity, async () => [] as Automation[])).toEqual([]);
  });

  test('upsert surfaces validation issues without an automation', async () => {
    replies[`${MOD}.upsert`] = { code: 'invalid', issues: [{ path: 'actions[0].params.text', code: 'required' }] };
    expect(await store.upsertAutomation('7', input)).toEqual({
      ok: false,
      error: undefined,
      code: 'invalid',
      issues: [{ path: 'actions[0].params.text', code: 'required' }],
      conflict: false
    });
  });

  test.each([{ conflict: true }, { code: 'conflict' }])('upsert flags a revision conflict: %o', async (reply) => {
    replies[`${MOD}.upsert`] = reply;
    const res = await store.upsertAutomation('7', { ...input, id: 'a', expectedRevision: 1 });
    expect(res.ok === false && res.conflict).toBe(true);
  });

  test('a duplicate-name refusal stays a field issue, not a conflict', async () => {
    replies[`${MOD}.upsert`] = { code: 'conflict', issues: [{ path: 'name', code: 'duplicate' }] };
    const res = await store.upsertAutomation('7', input);
    expect(res.ok === false && res.conflict).toBe(false);
  });

  test('delete reports the owner verdict and evicts the list', async () => {
    replies[`${MOD}.delete`] = { deleted: true };
    expect(await store.deleteAutomation('7', 'a')).toBe(true);
    replies[`${MOD}.delete`] = { deleted: false, code: 'not_found' };
    expect(await store.deleteAutomation('7', 'a')).toBe(false);
  });

  test('validate returns the owner issues', async () => {
    replies[`${MOD}.validate`] = { issues: [{ path: 'name', code: 'too_long' }] };
    expect(await store.validateAutomation('7', 'n', manual)).toEqual([{ path: 'name', code: 'too_long' }]);
    expect(calls[0].body).toEqual({ user_id: '7', name: 'n', definition: manual });
  });

  test('run now tags the dashboard source with a fresh request id', async () => {
    replies['bagel.rpc.automations.run'] = { accepted: true, run_id: 'r9' };
    expect(await store.runNow('7', 'Hello')).toEqual({ ok: true, runId: 'r9' });
    await store.runNow('7', 'Hello');
    const [a, b] = calls.map((c) => c.body);
    expect(a).toMatchObject({ broadcaster_id: '7', name: 'Hello', source: 'dashboard' });
    expect(a.request_id).not.toBe(b.request_id);
  });

  test('run now relays a refusal', async () => {
    replies['bagel.rpc.automations.run'] = { accepted: false, code: 'not_found', error: 'no such automation' };
    expect(await store.runNow('7', 'x')).toEqual({ ok: false, error: 'no such automation', code: 'not_found' });
  });
});
