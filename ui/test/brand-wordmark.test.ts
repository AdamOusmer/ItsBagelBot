// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { splitWordmark } from '../lib/brand-wordmark';

describe('splitWordmark', () => {
  test('replaces the last o of the brand name', () => {
    expect(splitWordmark('ItsBagelBot')).toEqual(['ItsBagelB', 't']);
  });

  test('matches an uppercase O and keeps the original case around it', () => {
    expect(splitWordmark('ROBOT')).toEqual(['ROB', 'T']);
  });

  test('returns null when the name has no o', () => {
    expect(splitWordmark('Bagel')).toBeNull();
  });

  test('an o at either end leaves an empty side', () => {
    expect(splitWordmark('o')).toEqual(['', '']);
    expect(splitWordmark('Bot')).toEqual(['B', 't']);
  });
});
