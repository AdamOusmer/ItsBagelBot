// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();
process.env.NEW_RELIC_ENABLED = 'false';

type SubOpts = { verify?: boolean; repair?: boolean } | undefined;
const subCalls: SubOpts[] = [];
let subReplies: Array<{ state: string; error: string; checkedAt: string | null }> = [];

const services = await import('../../lib/server/services');
mock.module('$lib/server/services', () => ({
  ...services,
  hasGrant: async () => true,
  accountState: async () => ({ active: true, status: 'free', lifecycle: 'active' }),
  delegationList: async () => [],
  channelSubState: async (_uid: string, opts?: SubOpts) => {
    subCalls.push(opts);
    return subReplies.shift()!;
  }
}));
const commandsStore = await import('../../lib/server/commands-store');
mock.module('$lib/server/commands-store', () => ({
  ...commandsStore,
  listCommands: async () => [],
  listModules: async () => []
}));
const lanes = await import('../../lib/server/overview-lanes');
mock.module('$lib/server/overview-lanes', () => ({ ...lanes, overviewLanes: () => ({}) }));

const { load } = await import('./+page.server');

const boardEvent = (uid: string) => ({ locals: { session: { user_id: uid, scopes: [] } }, url: new URL('http://localhost/') }) as never;
const event = boardEvent('1001');

const known = { state: 'ok', error: '', checkedAt: 'now' };
const unknown = { state: 'unknown', error: '', checkedAt: null };

beforeEach(() => {
  subCalls.length = 0;
});

describe('overview connection loader', () => {
  test('known stored state is not re-verified', async () => {
    subReplies = [known];
    const conn = await ((await load(event)) as { conn: Promise<{ signals: { sub: string } }> }).conn;
    expect(subCalls).toEqual([undefined]);
    expect(conn.signals.sub).toBe('ok');
  });

  test('unknown stored state re-calls with verify and repair and uses the result', async () => {
    subReplies = [unknown, { state: 'pending', error: '', checkedAt: null }];
    const conn = await ((await load(event)) as { conn: Promise<{ signals: { sub: string } }> }).conn;
    expect(subCalls).toEqual([undefined, { verify: true, repair: true }]);
    expect(conn.signals.sub).toBe('pending');
  });

  test('a board verified in the last 30 s reuses that verify instead of calling again', async () => {
    subReplies = [unknown, { state: 'pending', error: '', checkedAt: null }, unknown];
    const first = await ((await load(boardEvent('2002'))) as { conn: Promise<{ signals: { sub: string } }> }).conn;
    const second = await ((await load(boardEvent('2002'))) as { conn: Promise<{ signals: { sub: string } }> }).conn;
    expect(subCalls).toEqual([undefined, { verify: true, repair: true }, undefined]);
    expect([first.signals.sub, second.signals.sub]).toEqual(['pending', 'pending']);
  });
});
