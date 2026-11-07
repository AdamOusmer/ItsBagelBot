// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import type { GoveeScene } from '@bagel/kit';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let reply: Record<string, unknown> | Error = {};
const calls: { subject: string; payload: unknown }[] = [];
let moduleRows: unknown[] = [];
const moduleReads = mock(async () => moduleRows);
const moduleWrites: unknown[] = [];

const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  rpcReply: async (subject: string, payload: unknown) => {
    calls.push({ subject, payload });
    if (reply instanceof Error) throw reply;
    return reply;
  },
  rpc: async (subject: string) =>
    subject.includes('.channelpoints.') ? { reward: { id: 'rw-g', title: 'Whole room', cost: 900 } } : { present: true }
}));
mock.module('./commands-store', () => ({
  listModules: moduleReads,
  upsertModule: async (_user: string, _name: string, _enabled: boolean, configs: unknown) => {
    moduleWrites.push(configs);
    return { modules: [], synced: true };
  }
}));

const { goveeStore, parseGroupLights } = await import('./govee-store');
const { invalidate } = await import('./services');

const sunrise: GoveeScene = {
  id: 'lightScene:4280:3853',
  name: 'Sunrise',
  type: 'devices.capabilities.dynamic_scene',
  instance: 'lightScene',
  value: { paramId: 4280, id: 3853 }
};

describe('listScenes', () => {
  beforeEach(() => {
    calls.length = 0;
    moduleReads.mockClear();
    invalidate('govee-scenes:');
  });

  test('asks gossip for that device and returns its scenes', async () => {
    reply = { scenes: [sunrise] };
    expect(await goveeStore('1').listScenes({ device: 'AB:CD', sku: 'H6159' })).toEqual({ scenes: [sunrise] });
    expect(calls).toEqual([{ subject: expect.stringMatching(/\.govee\.scenes$/), payload: { channel_id: '1', device: 'AB:CD', sku: 'H6159' } }]);
  });

  test('never reads the module state, so a recipe works with the module off', async () => {
    reply = { scenes: [sunrise] };
    await goveeStore('1').listScenes({ device: 'AB:CD', sku: 'H6159' });
    expect(moduleReads).not.toHaveBeenCalled();
  });

  test('caches per device', async () => {
    reply = { scenes: [sunrise] };
    const store = goveeStore('1');
    await store.listScenes({ device: 'AB:CD', sku: 'H6159' });
    await store.listScenes({ device: 'AB:CD', sku: 'H6159' });
    expect(calls).toHaveLength(1);
    await store.listScenes({ device: 'EF:01', sku: 'H6159' });
    expect(calls).toHaveLength(2);
  });

  test('a rejected key comes back coded', async () => {
    reply = { scenes: [], error: 'Govee did not accept this API key', key_rejected: true };
    expect(await goveeStore('1').listScenes({ device: 'AB:CD', sku: 'H6159' })).toEqual({
      scenes: [],
      error: 'Govee did not accept this API key',
      code: 'key_rejected'
    });
  });

  test('a gossip refusal is unreachable', async () => {
    reply = { scenes: [], error: 'scene lookup failed' };
    expect(await goveeStore('1').listScenes({ device: 'AB:CD', sku: 'H6159' })).toEqual({ scenes: [], error: 'scene lookup failed', code: 'unreachable' });
  });

  test('a transport failure does not throw', async () => {
    reply = new Error('no responders');
    expect(await goveeStore('1').listScenes({ device: 'AB:CD', sku: 'H6159' })).toEqual({ scenes: [], error: 'no responders', code: 'unreachable' });
  });

  test('clearing the key drops cached scenes', async () => {
    reply = { scenes: [sunrise] };
    const store = goveeStore('1');
    await store.listScenes({ device: 'AB:CD', sku: 'H6159' });
    await store.clearKey();
    await store.listScenes({ device: 'AB:CD', sku: 'H6159' });
    expect(calls.filter((c) => c.subject.endsWith('.govee.scenes'))).toHaveLength(2);
  });
});

describe('light groups', () => {
  const lights = [
    { device: 'AA', sku: 'H1', name: 'Desk' },
    { device: 'BB', sku: 'H2', name: 'Lamp' }
  ];

  test('saving a group keeps the single light bindings and its place in the list', async () => {
    const binding = { device: 'AA', sku: 'H1', rewardId: 'rw-1' };
    const group = (rewardId: string, name: string) => ({ rewardId, name, lights });
    moduleRows = [{ name: 'govee', is_enabled: true, configs: { bindings: [binding], groups: [group('rw-g', 'Old'), group('rw-z', 'Other')] } }];
    moduleWrites.length = 0;
    const draft = { title: 'Whole room', cost: 900, color: '', cooldown: 0, onRedeem: 'fulfill', replyMessage: '', allowOff: false, allowOffline: false } as const;

    expect(await goveeStore('1').saveGroup(group('rw-g', 'Room'), draft)).toEqual({ ok: true });

    const written = moduleWrites[0] as { bindings: { rewardId: string }[]; groups: { name: string }[] };
    expect(written.bindings.map((b) => b.rewardId)).toEqual(['rw-1']);
    expect(written.groups.map((g) => g.name)).toEqual(['Room', 'Other']);
  });

  test('deleting a group leaves the other groups', async () => {
    moduleRows = [{ name: 'govee', is_enabled: true, configs: { groups: [{ rewardId: 'rw-g', name: 'A', lights }, { rewardId: 'rw-z', name: 'B', lights }] } }];
    moduleWrites.length = 0;

    expect(await goveeStore('1').deleteGroup('rw-g')).toEqual({ ok: true });

    expect((moduleWrites[0] as { groups: { name: string }[] }).groups.map((g) => g.name)).toEqual(['B']);
  });

  test('a group needs 2 to 32 distinct lights', () => {
    expect(parseGroupLights(JSON.stringify(lights))).toEqual(lights);
    expect(parseGroupLights(JSON.stringify([lights[0], lights[0]]))).toBeNull();
    expect(parseGroupLights('not json')).toBeNull();
  });
});
