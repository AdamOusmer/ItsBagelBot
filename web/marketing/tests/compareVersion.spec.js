// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { compareVersion } from '../src/lib/compareVersion.ts';

describe('compareVersion', () => {
  test('orders core versions numerically', () => {
    expect(compareVersion('v0.1.3-beta', 'v0.1.2-beta')).toBeGreaterThan(0);
    expect(compareVersion('v0.1.1-beta', 'v0.1.3-beta')).toBeLessThan(0);
    expect(compareVersion('v0.1.0-alpha', 'v0.1.3-beta')).toBeLessThan(0);
  });

  test('treats bare release as newer than the same core with a prerelease', () => {
    expect(compareVersion('v1.0.0', 'v1.0.0-beta')).toBeGreaterThan(0);
  });

  test('sorts prerelease labels lexicographically', () => {
    expect(compareVersion('v1.0.0-beta', 'v1.0.0-alpha')).toBeGreaterThan(0);
  });

  test('newest-first sort matches the changelog page', () => {
    const tags = [
      'v0.1.0-beta',
      'v0.1.3-beta',
      'v0.1.0-alpha',
      'v0.1.1-beta',
      'v0.1.2-beta',
    ];
    expect([...tags].sort((a, b) => compareVersion(b, a))).toEqual([
      'v0.1.3-beta',
      'v0.1.2-beta',
      'v0.1.1-beta',
      'v0.1.0-beta',
      'v0.1.0-alpha',
    ]);
  });
});
