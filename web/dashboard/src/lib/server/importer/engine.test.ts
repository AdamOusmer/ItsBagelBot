// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';

class RpcError extends Error {}

let rpcFail: (subject: string) => Error | null = () => null;
let commandFail: (name: string) => Error | null = () => null;
let existing: string[] = [];
let modulesDown = false;
let patches: { configs: { timers: Record<string, unknown>[] } }[] = [];

mock.module('@bagel/kit/server/nats', () => ({
  RpcError,
  rpc: async (subject: string, payload?: unknown) => {
    patches.push(payload as never);
    const err = rpcFail(subject);
    if (err) throw err;
    return {};
  }
}));
mock.module('@bagel/kit/server/logger', () => ({ logger: { info: () => {} } }));
mock.module('../services', () => ({ invalidate: () => {}, SUB: { modules: 'modules' } }));
mock.module('../commands-store', () => ({
  listCommands: async () => existing.map((name) => ({ name })),
  listModules: async () => {
    if (modulesDown) throw new Error('down');
    return [];
  },
  upsertCommand: async (_uid: string, c: { name: string }) => {
    const err = commandFail(c.name);
    if (err) throw err;
  }
}));
mock.module('../quotes-store', () => ({ addQuote: async () => ({}) }));
mock.module('./strategy', () => ({ SERVER_STRATEGIES: {} }));

const { commitImport } = await import('./engine');

const session = { user_id: 'u1' } as never;
const manifest = {
  commands: [{ name: 'a', responses: ['x'] }, { name: 'b', responses: ['y'] }, { name: 'c', responses: ['z'] }],
  timers: [{ message: 'tick', interval_seconds: 600 }],
  triggers: [{ phrase: 'bad\nline', response: 'r' }]
};

beforeEach(() => {
  rpcFail = () => null;
  commandFail = () => null;
  existing = [];
  modulesDown = false;
  patches = [];
});

test('a refused command fails alone and the rest apply', async () => {
  commandFail = (name) => (name === 'b' ? new RpcError('nope') : null);
  const res = await commitImport(session, { source: 'nightbot', manifest, overwrite: false });
  expect(res.applied.commands).toBe(2);
  expect(res.failed).toContainEqual({ kind: 'command', name: 'b', reason: 'rejected' });
  expect(res.failed).toContainEqual({ kind: 'trigger', name: 'bad\nline', reason: 'invalid' });
});

test('module patch refusal marks timers as module failures', async () => {
  rpcFail = () => new RpcError('refused');
  const res = await commitImport(session, { source: 'nightbot', manifest, overwrite: false });
  expect(res.applied.timers).toBe(0);
  expect(res.failed).toContainEqual({ kind: 'timer', name: 'tick', reason: 'module' });
});

test('modules unavailable marks timers as module failures', async () => {
  modulesDown = true;
  const res = await commitImport(session, { source: 'nightbot', manifest, overwrite: false });
  expect(res.failed).toContainEqual({ kind: 'timer', name: 'tick', reason: 'module' });
});

test('name collisions stay in skipped, not failed', async () => {
  existing = ['a'];
  const res = await commitImport(session, { source: 'nightbot', manifest, overwrite: false });
  expect(res.skipped).toEqual([{ kind: 'command', name: 'a' }]);
  expect(res.failed?.some((f) => f.name === 'a')).toBe(false);
});

test('transport failure rejects the whole commit', async () => {
  commandFail = () => new Error('timeout');
  await expect(commitImport(session, { source: 'nightbot', manifest, overwrite: false })).rejects.toThrow('timeout');
});

test('all applied leaves failed absent', async () => {
  const clean = { commands: [{ name: 'a', responses: ['x'] }] };
  const res = await commitImport(session, { source: 'nightbot', manifest: clean, overwrite: false });
  expect(res.failed).toBeUndefined();
});

async function appliedTimers(timers: Record<string, unknown>[]) {
  patches = [];
  const res = await commitImport(session, {
    source: 'nightbot',
    manifest: { timers: timers as never },
    overwrite: false
  });
  return { res, timers: patches[patches.length - 1].configs.timers };
}

test('timer gate fields map onto the timer definition', async () => {
  const { timers } = await appliedTimers([
    { message: 'gated', interval_seconds: 600, online_only: false, min_chat_lines: 20, chat_window_minutes: 10 },
    { message: 'live', interval_seconds: 600, online_only: true },
    { message: 'unset', interval_seconds: 600 }
  ]);
  expect(timers[0]).toMatchObject({ minChatLines: 20, chatWindowMinutes: 10, allowOffline: true });
  expect(timers[1]).toMatchObject({ minChatLines: 0, chatWindowMinutes: 5, allowOffline: false });
  expect(timers[2]).toMatchObject({ minChatLines: 0, chatWindowMinutes: 5, allowOffline: false });
});

test('a gate with lines but no window defaults the window to five minutes', async () => {
  const { timers } = await appliedTimers([{ message: 'x', interval_seconds: 600, min_chat_lines: 3, chat_window_minutes: 0 }]);
  expect(timers[0]).toMatchObject({ minChatLines: 3, chatWindowMinutes: 5 });
});

test('out of range gate values clamp with a warning', async () => {
  const { res, timers } = await appliedTimers([
    { message: 'x', interval_seconds: 600, min_chat_lines: 500, chat_window_minutes: 90 }
  ]);
  expect(timers[0]).toMatchObject({ minChatLines: 100, chatWindowMinutes: 60 });
  const clamped = (res.diagnostics ?? []).filter((d) => d.code === 'timer_gate_clamped');
  expect(clamped.map((d) => d.severity)).toEqual(['warn', 'warn']);
});
