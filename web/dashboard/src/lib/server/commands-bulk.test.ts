// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';

const calls: string[] = [];
let failName = '';
mock.module('@bagel/kit/server/logger', () => ({ logger: { error: () => {} } }));
mock.module('./commands-store', () => ({
  listCommands: async () => [
    { name: 'discord', response: 'a', is_active: true },
    { name: 'lurk', response: 'b', is_active: true }
  ],
  upsertCommand: async (_uid: string, c: { name: string; isActive: boolean }) => {
    if (c.name === failName) throw new Error('rpc');
    calls.push(`upsert:${c.name}:${c.isActive}`);
  },
  upsertModule: async (_uid: string, name: string, on: boolean) => calls.push(`module:${name}:${on}`),
  deleteCommand: async (_uid: string, name: string) => calls.push(`delete:${name}`)
}));
const { runBulk } = await import('./commands-bulk');

beforeEach(() => {
  calls.length = 0;
  failName = '';
});

test('disable routes built-ins to modules and customs to commands', async () => {
  const r = await runBulk('1', 'disable', ['uptime', 'discord']);
  expect(r).toEqual([{ name: 'uptime', ok: true }, { name: 'discord', ok: true }]);
  expect(calls).toEqual(['module:uptime:false', 'upsert:discord:false']);
});

test('one failure is reported per item and does not stop the rest', async () => {
  failName = 'discord';
  const r = await runBulk('1', 'enable', ['discord', 'lurk']);
  expect(r).toEqual([{ name: 'discord', ok: false }, { name: 'lurk', ok: true }]);
});

test('delete refuses built-ins and unknown names', async () => {
  const r = await runBulk('1', 'delete', ['uptime', 'ghost', 'lurk']);
  expect(r.map((x) => x.ok)).toEqual([false, false, true]);
  expect(calls).toEqual(['delete:lurk']);
});
