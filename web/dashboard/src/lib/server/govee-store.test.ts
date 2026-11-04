// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import type { GoveeScene } from '@bagel/kit';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let reply: Record<string, unknown> | Error = {};
const calls: { subject: string; payload: unknown }[] = [];
const moduleReads = mock(async () => []);

const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  rpcReply: async (subject: string, payload: unknown) => {
    calls.push({ subject, payload });
    if (reply instanceof Error) throw reply;
    return reply;
  }
}));
mock.module('./commands-store', () => ({ listModules: moduleReads, upsertModule: async () => {} }));

const { goveeStore } = await import('./govee-store');
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
    expect(await goveeStore('1').listScenes('AB:CD', 'H6159')).toEqual({ scenes: [sunrise] });
    expect(calls).toEqual([{ subject: expect.stringMatching(/\.govee\.scenes$/), payload: { channel_id: '1', device: 'AB:CD', sku: 'H6159' } }]);
  });

  test('never reads the module state, so a recipe works with the module off', async () => {
    reply = { scenes: [sunrise] };
    await goveeStore('1').listScenes('AB:CD', 'H6159');
    expect(moduleReads).not.toHaveBeenCalled();
  });

  test('caches per device', async () => {
    reply = { scenes: [sunrise] };
    const store = goveeStore('1');
    await store.listScenes('AB:CD', 'H6159');
    await store.listScenes('AB:CD', 'H6159');
    expect(calls).toHaveLength(1);
    await store.listScenes('EF:01', 'H6159');
    expect(calls).toHaveLength(2);
  });

  test('a rejected key comes back coded', async () => {
    reply = { scenes: [], error: 'Govee did not accept this API key', key_rejected: true };
    expect(await goveeStore('1').listScenes('AB:CD', 'H6159')).toEqual({
      scenes: [],
      error: 'Govee did not accept this API key',
      code: 'key_rejected'
    });
  });

  test('a gossip refusal is unreachable', async () => {
    reply = { scenes: [], error: 'scene lookup failed' };
    expect(await goveeStore('1').listScenes('AB:CD', 'H6159')).toEqual({ scenes: [], error: 'scene lookup failed', code: 'unreachable' });
  });

  test('a transport failure does not throw', async () => {
    reply = new Error('no responders');
    expect(await goveeStore('1').listScenes('AB:CD', 'H6159')).toEqual({ scenes: [], error: 'no responders', code: 'unreachable' });
  });

  test('clearing the key drops cached scenes', async () => {
    reply = { scenes: [sunrise] };
    const store = goveeStore('1');
    await store.listScenes('AB:CD', 'H6159');
    await store.clearKey();
    await store.listScenes('AB:CD', 'H6159');
    expect(calls.filter((c) => c.subject.endsWith('.govee.scenes'))).toHaveLength(2);
  });
});
