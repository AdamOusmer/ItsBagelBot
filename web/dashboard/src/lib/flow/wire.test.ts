// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { wirePath } from './wire';

describe('wirePath', () => {
  test('level points get a straight line', () => {
    expect(wirePath({ x: 0, y: 5 }, { x: 80, y: 5 })).toBe('M0,5 L80,5');
  });

  test('a lower target bends near the source, then runs straight in', () => {
    expect(wirePath({ x: 0, y: 0 }, { x: 80, y: 100 })).toBe('M0,0 H14 Q28,0 28,14 V86 Q28,100 42,100 H80');
  });

  test('a higher target bends the other way', () => {
    expect(wirePath({ x: 0, y: 100 }, { x: 80, y: 0 })).toBe('M0,100 H14 Q28,100 28,86 V14 Q28,0 42,0 H80');
  });

  test('a small step softens to fit', () => {
    expect(wirePath({ x: 0, y: 0 }, { x: 80, y: 8 })).toBe('M0,0 H24 Q28,0 28,4 V4 Q28,8 32,8 H80');
  });

  test('close columns never turn past the halfway point', () => {
    expect(wirePath({ x: 0, y: 0 }, { x: 20, y: 100 })).toBe('M0,0 H0 Q10,0 10,10 V90 Q10,100 20,100 H20');
  });

  test('a backward wire stays well formed', () => {
    expect(wirePath({ x: 80, y: 0 }, { x: 0, y: 100 })).toBe('M80,0 H66 Q52,0 52,14 V86 Q52,100 38,100 H0');
  });

  test('siblings share the same turn', () => {
    const a = { x: 0, y: 0 };
    expect(wirePath(a, { x: 80, y: 100 })).toContain('Q28,0 28,14');
    expect(wirePath(a, { x: 80, y: 200 })).toContain('Q28,0 28,14');
  });
});
