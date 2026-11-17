// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import type { CommandInput } from './commands-store';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/env', () => ({ dev: false }));
mock.module('./edge-purge', () => ({ schedulePurgeChannel: () => {}, scheduleLeaderboardPurge: () => {} }));

const original = [{ name: 'hello', response: 'old', uses: '9' }];
let writeFailed = false;
let readFailed = false;
let projectionFailed = false;
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: { commands?: unknown[] }) => {
    const projection = subject.endsWith('.replace');
    if (projection && projectionFailed) throw new Error('projection unavailable');
    if (projection) return { commands: body.commands };
    if (writeFailed) throw new Error('write failed');
    return {};
  }
}));
mock.module('@bagel/kit/server/valkey-store', () => ({
  getCommands: async () => {
    if (readFailed) throw new Error('read failed');
    return { projected: true, commands: original };
  }
}));

const { fabric } = await import('./services');
const { upsertCommand, deleteCommand } = await import('./commands-store');
const input: CommandInput = {
  name: 'hello', aliases: [], response: 'new', isActive: true, streamOnlineOnly: false, perm: 'everyone',
  cooldown: 0, userCooldown: 0, allowedUserId: '', bumpCounter: ''
};
const mutations = [
  { operation: 'upsert', write: () => upsertCommand('42', input), names: ['hello'] },
  { operation: 'delete', write: () => deleteCommand('42', 'hello'), names: [] }
];

beforeEach(() => {
  fabric.cache.clear();
  writeFailed = false;
  readFailed = false;
  projectionFailed = false;
});

test.each(mutations)('$operation keeps its optimistic readback when projection fails', async ({ write, names }) => {
  fabric.cache.set('commands:42', original, 60_000);
  projectionFailed = true;
  const result = await write();
  expect(result.commands.map((command) => command.name)).toEqual(names);
  expect(await fabric.readKey<unknown[]>('commands:42', 60_000, async () => [])).toEqual(result.commands);
});

test.each(mutations)('$operation leaves cached commands alone when the durable write fails', async ({ write }) => {
  fabric.cache.set('commands:42', original, 60_000);
  writeFailed = true;
  await expect(write()).rejects.toThrow('write failed');
  expect(await fabric.readKey<unknown[]>('commands:42', 60_000, async () => [])).toEqual(original);
});

test.each(mutations)('$operation preserves the empty response fallback when command reads fail', async ({ write }) => {
  readFailed = true;
  expect((await write()).commands).toEqual([]);
  readFailed = false;
  expect(await fabric.readKey('commands:42', 60_000, async () => 'reloaded')).toBe('reloaded');
});
