// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';

import {
  areaGeometry,
  nearestIndex,
  percent,
  plainValue,
  pointAt,
  stepIndex,
  tipRow,
  tipSide,
  valueText,
  type AreaKey,
  type AreaTipSide,
} from '../lib/area-series';

describe('area-series hover geometry', () => {
  test.each([
    [0, 5, 0],
    [1, 5, 4],
    [0.5, 5, 2],
    [0.37, 5, 1],
    [0.38, 5, 2],
    [-2, 5, 0],
    [9, 5, 4],
    [0.5, 2, 1],
    [0.4, 2, 0],
  ])('fraction %p over %p points snaps to index %p', (fraction, count, expected) => {
    expect(nearestIndex(fraction, count)).toBe(expected);
  });

  test.each([
    [0.5, 1],
    [0.5, 0],
    [Number.NaN, 5],
  ])('fraction %p over %p points has nothing to hover', (fraction, count) => {
    expect(nearestIndex(fraction, count)).toBeNull();
  });

  test('point positions are fractions of the box and match the drawn line', () => {
    const values = [1, 4, 2];
    expect([0, 1, 2].map((i) => pointAt(values, i))).toEqual([
      { x: 0, y: 111 / 178 },
      { x: 0.5, y: 12 / 178 },
      { x: 1, y: 78 / 178 },
    ]);
    expect(areaGeometry(values).line).toBe('M0,111 L320,12 L640,78');
  });

  test.each([
    [[4], 0],
    [[], 0],
    [[1, 2], -1],
    [[1, 2], 2],
  ])('points of %p at index %p do not exist', (values, index) => {
    expect(pointAt(values, index)).toBeNull();
  });

  test('a flat zero series sits on the baseline', () => {
    expect(pointAt([0, 0], 1)?.y).toBe(144 / 178);
  });

  test.each<[number | null, AreaKey, number | null]>([
    [null, 'ArrowLeft', 4],
    [null, 'ArrowRight', 4],
    [null, 'Home', 0],
    [null, 'End', 4],
    [2, 'ArrowLeft', 1],
    [2, 'ArrowRight', 3],
    [0, 'ArrowLeft', 0],
    [4, 'ArrowRight', 4],
    [2, 'Home', 0],
    [2, 'End', 4],
    [2, 'Escape', null],
  ])('from %p, %s moves to %p of five points', (current, key, expected) => {
    expect(stepIndex(current, key, 5)).toBe(expected);
  });

  test('keys do nothing without two points', () => {
    expect([0, 1].map((count) => stepIndex(null, 'End', count))).toEqual([null, null]);
  });

  test.each<[number, AreaTipSide]>([
    [0, 'start'],
    [0.24, 'start'],
    [0.5, 'center'],
    [0.76, 'end'],
    [1, 'end'],
  ])('readout at %p opens toward %p', (fraction, side) => {
    expect(tipSide(fraction)).toBe(side);
  });

  test.each<[number, 'high' | 'low']>([
    [12 / 178, 'low'],
    [0.39, 'low'],
    [0.4, 'high'],
    [144 / 178, 'high'],
  ])('readout for a point at height %p sits %p', (y, row) => {
    expect(tipRow(y)).toBe(row);
  });

  test('values read with grouping, a custom format and a unit', () => {
    const fixed = (v: number) => v.toFixed(1);
    expect([
      valueText(12345, plainValue, ''),
      valueText(3, fixed, 'msgs'),
      valueText(0, plainValue, '%'),
    ]).toEqual(['12,345', '3.0 msgs', '0 %']);
  });

  test('percentages keep three decimals', () => {
    expect([0, 1, 111 / 178, 0.5].map(percent)).toEqual(['0%', '100%', '62.36%', '50%']);
  });
});
