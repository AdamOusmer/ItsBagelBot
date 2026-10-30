// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';

process.env.NEW_RELIC_ENABLED = 'false';

const { RpcError } = await import('@bagel/kit/server/nats');

type Stored = { userId: string; username: string } | null;
type Heal = { kind: 'found'; value: { userId: string; username: string } } | { kind: 'missing' } | { kind: 'busy' } | { kind: 'error' };

let stored: () => Promise<Stored> = async () => null;
let heal: Heal = { kind: 'missing' };
let heals: string[] = [];

mock.module('#lib/server/services', () => ({ resolveLogin: () => stored() }));
mock.module('#lib/server/identity-heal', () => ({
  healLogin: async (login: string) => {
    heals.push(login);
    return heal;
  }
}));

const { lookupLogin } = await import('./channel-lookup');

const renamed = { userId: '42', username: 'newname', displayName: 'NewName' };

beforeEach(() => {
  stored = async () => null;
  heal = { kind: 'missing' };
  heals = [];
});

describe('lookupLogin', () => {
  test('a stored login resolves without touching Twitch', async () => {
    stored = async () => renamed;
    expect(await lookupLogin('newname')).toEqual(renamed);
    expect(heals).toEqual([]);
  });

  test.each([
    { name: 'an empty reply', miss: async () => null },
    {
      name: 'a not_found refusal',
      miss: async () => {
        throw new RpcError('ent: user not found', 'not_found');
      }
    },
    {
      name: 'an invalid refusal',
      miss: async () => {
        throw new RpcError('bad login', 'invalid');
      }
    }
  ])('$name falls through to the heal, which can resolve the renamed channel', async ({ miss }) => {
    stored = miss;
    heal = { kind: 'found', value: renamed };
    expect(await lookupLogin('newname')).toEqual(renamed);
    expect(heals).toEqual(['newname']);
  });

  test.each([
    { heal: { kind: 'missing' } as Heal, want: 'missing' },
    { heal: { kind: 'busy' } as Heal, want: 'failed' },
    { heal: { kind: 'error' } as Heal, want: 'failed' }
  ])('a $heal.kind heal after a definitive miss answers $want', async ({ heal: reply, want }) => {
    heal = reply;
    expect(await lookupLogin('newname')).toBe(want);
  });

  test('an unreachable users service stays failed and never probes Twitch', async () => {
    stored = async () => {
      throw new Error('timeout');
    };
    expect(await lookupLogin('newname')).toBe('failed');
    expect(heals).toEqual([]);
  });
});
