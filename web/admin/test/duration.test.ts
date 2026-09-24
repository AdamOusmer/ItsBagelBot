// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import { durationLabel } from '../src/lib/duration';

describe('durationLabel', () => {
  test('starts at nanoseconds and steps up by thousands with two decimals', () => {
    expect(durationLabel(0)).toBe('0.00 ns');
    expect(durationLabel(64)).toBe('64.00 ns');
    expect(durationLabel(1_234)).toBe('1.23 µs');
    expect(durationLabel(3_400_000)).toBe('3.40 ms');
    expect(durationLabel(2_500_000_000)).toBe('2.50 s');
  });

  test('stays in seconds past a thousand seconds', () => {
    expect(durationLabel(4_200_000_000_000)).toBe('4200.00 s');
  });
});
