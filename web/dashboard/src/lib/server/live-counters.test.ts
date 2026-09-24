// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { parseBoard, parseTotals } from './live-counters';

describe('parseTotals', () => {
  test('an unseeded hash reads as unavailable, not as zero', () => {
    expect(parseTotals(['messages_processed'], [null, null])).toBeNull();
    expect(parseTotals(['messages_processed'], [])).toBeNull();
  });

  test('maps each requested counter in order and zero-fills missing fields', () => {
    expect(parseTotals(['a', 'b'], ['1727000000000', '42', null])).toEqual({ a: '42', b: '0' });
  });

  test('rejects malformed or imprecise values', () => {
    expect(parseTotals(['a'], ['seeded', 'junk'])).toBeNull();
    expect(parseTotals(['a'], ['seeded', '9223372036854775808'])).toBeNull();
    expect(parseBoard(1, ['-1:111'])).toBeNull();
    expect(parseBoard(1, ['9223372036854775808:111'])).toBeNull();
  });
});

describe('parseBoard', () => {
  test('an unseeded board reads as unavailable', () => {
    expect(parseBoard(0, ['0000000000000000005:1'])).toBeNull();
  });

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

  test('a seeded but empty board is an empty ranking', () => {
    expect(parseBoard(1, [])?.size).toBe(0);
  });
});
