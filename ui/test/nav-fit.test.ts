// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { decideNavCollapse } from '../lib/nav-fit';

describe('decideNavCollapse', () => {
  test('stays expanded while content fits', () => {
    expect(decideNavCollapse({ available: 1000, needed: 900 }, false)).toBe(false);
  });

  test('exact fit does not collapse', () => {
    expect(decideNavCollapse({ available: 1000, needed: 1000 }, false)).toBe(false);
  });

  test('collapses once content no longer fits', () => {
    expect(decideNavCollapse({ available: 1000, needed: 1001 }, false)).toBe(true);
  });

  test('stays collapsed inside the hysteresis band', () => {
    expect(decideNavCollapse({ available: 1000, needed: 995 }, true, 12)).toBe(true);
  });

  test('expands exactly at the hysteresis boundary', () => {
    expect(decideNavCollapse({ available: 1000, needed: 988 }, true, 12)).toBe(false);
    expect(decideNavCollapse({ available: 1000, needed: 989 }, true, 12)).toBe(true);
  });

  test('a collapsed nav that still overflows stays collapsed', () => {
    expect(decideNavCollapse({ available: 800, needed: 1200 }, true)).toBe(true);
  });

  test('an expanded nav with room to spare never collapses on its own', () => {
    expect(decideNavCollapse({ available: 1200, needed: 400 }, false)).toBe(false);
  });

  test('default hysteresis is a few px, not zero', () => {
    expect(decideNavCollapse({ available: 1000, needed: 996 }, true)).toBe(true);
    expect(decideNavCollapse({ available: 1000, needed: 980 }, true)).toBe(false);
  });
});
