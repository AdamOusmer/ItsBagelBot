// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { digitPhase, statGlyphs } from './stats-digits';

describe('mechanical counter digits', () => {
  test('only digits involved in a carry move, with a seamless duplicate zero', () => {
    expect([0, 1, 2, 3].map((place) => digitPhase(1299.75, place))).toEqual([9.75, 9.75, 2.75, 1]);
    expect([0, 1, 2, 3].map((place) => digitPhase(1300, place))).toEqual([0, 0, 3, 1]);
    expect(digitPhase(1204.25, 1)).toBe(0);
    expect(digitPhase(1204.25, 0)).toBe(4.25);
  });

  test('slow counters rest between brief rolls and zero freezes every wheel', () => {
    expect(digitPhase(4.8, 0, 0.14)).toBe(4);
    expect(digitPhase(4.93, 0, 0.14)).toBeCloseTo(4.5);
    expect(digitPhase(14.75, 0, 0)).toBe(4);
    expect(digitPhase(1299.75, 2, 0)).toBe(2);
  });

  test.each(['en-US', 'fr-FR', 'ar-EG', 'hi-IN', 'ja-JP'])('keeps %s numerals, grouping, decimals and compact scale', (locale) => {
    const numeralFormat = new Intl.NumberFormat(locale, { useGrouping: false });
    const numerals = Array.from({ length: 10 }, (_, i) => numeralFormat.format(i));
    for (const compact of [false, true]) {
      const format = new Intl.NumberFormat(locale, {
        notation: compact ? 'compact' : 'standard',
        minimumFractionDigits: compact ? 2 : 0,
        maximumFractionDigits: compact ? 2 : 0,
        roundingMode: 'floor'
      });
      for (const value of [0, 9.25, 99.9, 999.99, 1000, 9999.9, 1_500_009.75, 1_500_000_000]) {
        const glyphs = statGlyphs(value, format, numerals);
        expect(glyphs.map((glyph) => glyph.text).join('')).toBe(format.format(value));
        expect(new Set(glyphs.map((glyph) => glyph.key)).size).toBe(glyphs.length);
        for (const glyph of glyphs.filter((glyph) => glyph.phase !== null)) {
          expect(numerals[Math.floor(glyph.phase!)]).toBe(glyph.text);
          expect(glyph.phase!).toBeGreaterThanOrEqual(0);
          expect(glyph.phase!).toBeLessThan(10);
        }
      }
    }
  });
});
