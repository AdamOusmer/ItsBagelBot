// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';

process.env.NEW_RELIC_ENABLED = 'false';

const nats = await import('@bagel/kit/server/nats');
const { RpcError } = nats;

type TwitchReply = { target_id?: string; login?: string; display_name?: string; user_found?: boolean; error?: string };

let twitch: (login: string) => Promise<TwitchReply> = async () => ({});
let sync: (id: string, login: string, name: string) => Promise<unknown> = async () => ({ renamed: true });
const twitchCalls: unknown[] = [];
const syncCalls: unknown[][] = [];

mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, req: { target_login: string }) => {
    twitchCalls.push([subject, req]);
    return twitch(req.target_login);
  }
}));
mock.module('./services', () => ({
  SUB: { outgressRpc: 'bagel.rpc.outgress' },
  syncIdentity: (...args: [string, string, string]) => {
    syncCalls.push(args);
    return sync(...args);
  }
}));

const { healLogin } = await import('./identity-heal');
const { BUCKET_CAPACITY } = await import('./viewer-resolver');

let n = 0;
const fresh = () => `heal${n++}`;

const onTwitch = (login: string): TwitchReply => ({ target_id: '42', login, display_name: 'NewName', user_found: true });

beforeEach(() => {
  twitch = async (login) => onTwitch(login);
  sync = async () => ({ renamed: true });
  twitchCalls.length = 0;
  syncCalls.length = 0;
});

describe('healLogin', () => {
  test('Twitch names the id and identity_sync moves the account onto the new login', async () => {
    const login = fresh();
    expect(await healLogin(login)).toEqual({ kind: 'found', value: { userId: '42', username: login, displayName: 'NewName' } });
    expect(twitchCalls).toEqual([['bagel.rpc.outgress.accountage.get', { target_login: login }]]);
    expect(syncCalls).toEqual([['42', login, 'NewName']]);
  });

  test('a missing display name falls back to the login', async () => {
    const login = fresh();
    twitch = async () => ({ ...onTwitch(login), display_name: ' ' });
    expect(await healLogin(login)).toMatchObject({ kind: 'found', value: { displayName: login } });
  });

  test.each([
    { name: 'user_found false', reply: (): TwitchReply => ({ user_found: false }) },
    { name: 'an error reply', reply: (): TwitchReply => ({ error: 'boom', user_found: true, target_id: '42' }) },
    { name: 'an empty id', reply: (l: string): TwitchReply => ({ ...onTwitch(l), target_id: '' }) },
    { name: 'a different login than asked', reply: (): TwitchReply => onTwitch('someoneelse') }
  ])('$name is missing without calling identity_sync', async ({ reply }) => {
    twitch = async (login) => reply(login);
    expect(await healLogin(fresh())).toEqual({ kind: 'missing' });
    expect(syncCalls).toEqual([]);
  });

  test.each(['not_found', 'invalid'])('a %s refusal from identity_sync is a miss', async (code) => {
    sync = async () => {
      throw new RpcError('nope', code);
    };
    expect(await healLogin(fresh())).toEqual({ kind: 'missing' });
  });

  test('other identity_sync failures and Twitch failures are errors', async () => {
    sync = async () => {
      throw new Error('timeout');
    };
    expect(await healLogin(fresh())).toEqual({ kind: 'error' });
    twitch = async () => {
      throw new Error('outgress down');
    };
    expect(await healLogin(fresh())).toEqual({ kind: 'error' });
  });

  test('repeat and concurrent probes of one login share a single Twitch lookup and sync', async () => {
    const login = fresh();
    await Promise.all([healLogin(login), healLogin(login)]);
    await healLogin(login);
    expect(twitchCalls).toHaveLength(1);
    expect(syncCalls).toHaveLength(1);
  });

  test('a miss is cached so an unregistered Twitch user cannot repeat identity_sync', async () => {
    const login = fresh();
    sync = async () => {
      throw new RpcError('nope', 'not_found');
    };
    await healLogin(login);
    await healLogin(login);
    expect(syncCalls).toHaveLength(1);
  });

  test('a flood of distinct logins runs out of tokens and answers busy without calling Twitch', async () => {
    for (let i = 0; i < BUCKET_CAPACITY; i++) await healLogin(fresh());
    twitchCalls.length = 0;
    expect(await healLogin(fresh())).toEqual({ kind: 'busy' });
    expect(twitchCalls).toEqual([]);
  });
});
