// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';

type Master = {
  hmget: () => Promise<(string | null)[]>;
  exists: () => Promise<number>;
  zrevrange: () => Promise<string[]>;
} | null;

let master: Master = null;
mock.module('@bagel/kit/server/valkey-master', () => ({ masterClient: () => master }));

const { liveBoard, liveTotals, parseBoard, parseTotals } = await import('./live-counters');

const fakeMaster = (parts: Partial<NonNullable<Master>>): Master => ({
  hmget: async () => [],
  exists: async () => 0,
  zrevrange: async () => [],
  ...parts
});

const reads: { name: string; master: Master; read: () => Promise<unknown>; want: unknown }[] = [
  {
    name: 'an unseeded hash reads as unavailable, not as zero',
    master: fakeMaster({ hmget: async () => [null, null] }),
    read: () => liveTotals('1', ['messages_processed']),
    want: null
  },
  {
    name: 'totals map each requested counter in order and zero-fill missing fields',
    master: fakeMaster({ hmget: async () => ['1727000000000', '42', null] }),
    read: () => liveTotals('1', ['a', 'b']),
    want: { a: '42', b: '0' }
  },
  {
    name: 'totals are unavailable without a valkey master',
    master: null,
    read: () => liveTotals('1', ['a']),
    want: null
  },
  {
    name: 'totals are unavailable when the valkey read fails',
    master: fakeMaster({
      hmget: async () => {
        throw new Error('valkey down');
      }
    }),
    read: () => liveTotals('1', ['a']),
    want: null
  },
  {
    name: 'an unseeded board reads as unavailable',
    master: fakeMaster({ zrevrange: async () => ['0000000000000000005:1'] }),
    read: () => liveBoard('feed', 10),
    want: null
  },
  {
    name: 'a seeded but empty board is an empty ranking',
    master: fakeMaster({ exists: async () => 1 }),
    read: () => liveBoard('feed', 10),
    want: new Map()
  },
  {
    name: 'a seeded board keeps the ranking order of its members',
    master: fakeMaster({ exists: async () => 1, zrevrange: async () => ['0000000000000000900:111', '0000000000000000040:222'] }),
    read: () => liveBoard('feed', 10),
    want: new Map([['111', '900'], ['222', '40']])
  }
];

describe('live counter reads', () => {
  test.each(reads)('$name', async ({ master: client, read, want }) => {
    master = client;
    expect(await read()).toEqual(want);
  });
});

describe('parseTotals', () => {
  test('rejects malformed or imprecise values', () => {
    expect(parseTotals(['a'], ['seeded', 'junk'])).toBeNull();
    expect(parseTotals(['a'], ['seeded', '9223372036854775808'])).toBeNull();
    expect(parseBoard(1, ['-1:111'])).toBeNull();
    expect(parseBoard(1, ['9223372036854775808:111'])).toBeNull();
  });
});

describe('parseBoard', () => {
  test('reads exact values from ordered board members', () => {
    const board = parseBoard(1, ['0000000000000000900:111', '0000000000000000040:222']);
    expect([...board!.entries()]).toEqual([
      ['111', '900'],
      ['222', '40']
    ]);
  });

  test('retains adjacent full-width counter values', () => {
    const board = parseBoard(1, [
      '9223372036854775807:111',
      '9223372036854775806:222'
    ]);
    expect([...board!.entries()]).toEqual([
      ['111', '9223372036854775807'],
      ['222', '9223372036854775806']
    ]);
  });
});
