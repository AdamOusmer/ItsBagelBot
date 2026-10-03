// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, mock, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from './sveltekit';

stubSvelteKit();
privateEnv.TWITCH_BOT_USER_ID = 'bot';

const failing = new Set<string>();
const read = <T>(name: string, value: T) => async (): Promise<T> => {
  if (failing.has(name)) throw new Error(`${name} unavailable`);
  return value;
};

const services = await import('../src/lib/server/services');
mock.module('$lib/server/services', () => ({
  ...services,
  userEnrollment: read('enrollment', { days: [{ date: '2026-09-30', count: 4 }], stats: { total_users: 9, active_users: 8, premium_users: 1, vip_users: 0, paid_users: 1 } }),
  shardSnapshot: read('fleet', { shard_count: 3 }),
  trialList: read('trials', { version: 1, trials: [] }),
  serviceHealth: read('health', [{ service: 'users', ok: true }]),
  auditList: read('audit', []),
  tokenStatus: read('bot', { present: true })
}));
mock.module('$lib/server/giveaways', () => ({ giveawayAlerts: read('giveawayAlerts', []) }));

const { load } = await import('../src/routes/(admin)/+page.server');

const READS = ['enrollment', 'fleet', 'trials', 'health', 'audit', 'bot', 'giveawayAlerts'] as const;

async function panels(): Promise<Record<(typeof READS)[number], { ok: boolean; value: any }>> {
  const data = (await load({ url: new URL('https://admin.example/'), parent: async () => ({ id: '1', role: 'owner' }) } as never)) as any;
  const settled = await Promise.all(READS.map((name) => data[name]));
  return Object.fromEntries(READS.map((name, i) => [name, settled[i]])) as never;
}

describe('admin overview load', () => {
  test('every read answering fills its panel', async () => {
    failing.clear();
    const all = await panels();
    expect(READS.map((name) => all[name].ok)).toEqual(READS.map(() => true));
  });

  test('an outage degrades each panel to an empty fallback instead of failing the page', async () => {
    for (const name of READS) failing.add(name);
    const all = await panels();
    expect(READS.map((name) => all[name].ok)).toEqual(READS.map(() => false));
    expect({ days: all.enrollment.value.days.length, users: all.enrollment.value.stats.total_users, shards: all.fleet.value.shard_count }).toEqual({
      days: 30,
      users: 0,
      shards: 0
    });
  });

  test('one failing read does not take the other panels down', async () => {
    failing.clear();
    failing.add('fleet');
    const all = await panels();
    expect(READS.map((name) => all[name].ok)).toEqual([true, false, true, true, true, true, true]);
  });
});
