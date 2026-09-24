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
    expect(parseTotals(['a', 'b', 'c'], ['1727000000000', '42', null, 'junk'])).toEqual({ a: 42, b: 0, c: 0 });
  });
});

describe('parseBoard', () => {
  test('an unseeded board reads as unavailable', () => {
    expect(parseBoard(0, ['1', '5'])).toBeNull();
  });

  test('pairs members with scores in rank order', () => {
    const board = parseBoard(1, ['111', '900', '222', '40']);
    expect([...board!.entries()]).toEqual([
      ['111', 900],
      ['222', 40]
    ]);
  });

  test('a seeded but empty board is an empty ranking', () => {
    expect(parseBoard(1, [])?.size).toBe(0);
  });
});
