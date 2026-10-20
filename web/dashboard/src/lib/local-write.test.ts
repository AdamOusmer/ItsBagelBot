// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { consumeEcho, expectEcho } from './local-write';

describe('local write echo', () => {
  test('each write absorbs exactly one echo, later invalidations refresh', () => {
    expectEcho('b1');
    expect(consumeEcho('b1')).toBe(true);
    expect(consumeEcho('b1')).toBe(false);
  });

  test('two writes absorb two echoes', () => {
    expectEcho('b1');
    expectEcho('b1');
    expect([consumeEcho('b1'), consumeEcho('b1'), consumeEcho('b1')]).toEqual([true, true, false]);
  });

  test('an echo for another board is never absorbed', () => {
    expectEcho('b1');
    expect(consumeEcho('b2')).toBe(false);
    expect(consumeEcho('b1')).toBe(true);
  });

  test('a lost echo expires instead of hiding a later change', () => {
    const now = performance.now.bind(performance);
    expectEcho('b9');
    performance.now = () => now() + 3500;
    try {
      expect(consumeEcho('b9')).toBe(false);
    } finally {
      performance.now = now;
    }
  });
});
