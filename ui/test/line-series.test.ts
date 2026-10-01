// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';

import {
  axisValue,
  hoverTime,
  lineChart,
  nearestPoint,
  niceCeiling,
  plainValue,
  seriesClass,
  tooltipBox,
  type LinePoint,
} from '../lib/line-series';

describe('line-series geometry', () => {
  test('ceilings round up to 1, 2, 4, 5 or 10 of the magnitude', () => {
    expect([0, -3, 0.3, 1, 1.5, 3, 4.2, 7, 12, 480].map(niceCeiling)).toEqual([1, 1, 0.4, 1, 2, 4, 5, 10, 20, 500]);
  });

  test('axis labels shorten thousands and keep small rates readable', () => {
    expect([0, 0.25, 2.5, 42, 1500, 25000].map(axisValue)).toEqual(['0', '0.25', '2.5', '42', '1.5k', '25k']);
  });

  test('missing and negative values format as empty', () => {
    expect([null, undefined, -1, Number.NaN, 3].map(plainValue)).toEqual(['', '', '', '', '3.0']);
  });

  const points: LinePoint[] = [
    { at: 40_000, a: 2, b: null },
    { at: 0, a: 1, b: 1 },
    { at: 10_000, a: null, b: 3 },
    { at: Number.NaN, a: 9, b: 9 },
    { at: 50_000, a: 4, b: 2 },
  ];

  test('readings sort by time, drop bad timestamps and break lines at gaps and nulls', () => {
    const chart = lineChart(points, ['a', 'b'], 800);
    expect(chart.readings.map((p) => p.at)).toEqual([0, 10_000, 40_000, 50_000]);
    expect(chart.ceiling).toBe(4);
    expect(chart.paths[0].trim().split(/\s+/).map((c) => c[0])).toEqual(['M', 'M', 'L']);
    expect(chart.paths[1].trim().split(/\s+/).map((c) => c[0])).toEqual(['M', 'L', 'M']);
    expect(chart.dots).toHaveLength(6);
    expect(chart.dotRadius).toBe(1.5);
    expect(chart.sampling).toBe(false);
  });

  test('wide charts get five time ticks, narrow ones three and a shorter plot', () => {
    const wide = lineChart(points, ['a'], 800);
    const narrow = lineChart(points, ['a'], 320);
    expect([wide.width, wide.height, wide.timeTicks.length]).toEqual([800, 260, 5]);
    expect([narrow.width, narrow.height, narrow.timeTicks.length]).toEqual([320, 220, 3]);
    expect(narrow.timeTicks.map((t) => t.anchor)).toEqual(['start', 'middle', 'end']);
    expect(lineChart(points, ['a'], 100).width).toBe(280);
  });

  test('fewer than two plotted readings is still sampling, and no readings means no time axis', () => {
    expect(lineChart([{ at: 1, a: 3 }], ['a'], 800).sampling).toBe(true);
    expect(lineChart([], ['a'], 800).timeTicks).toEqual([]);
  });

  test('hover resolves to the nearest reading and the tooltip stays inside the plot', () => {
    const chart = lineChart(points, ['a'], 800);
    expect(hoverTime(chart, 0)).toBe(chart.startAt);
    expect(hoverTime(chart, 10_000)).toBe(chart.startAt + chart.duration);
    expect(nearestPoint(chart.readings, 41_000)?.at).toBe(40_000);
    expect(nearestPoint([], 1)).toBeNull();
    expect(tooltipBox(chart, 10, 2)).toEqual({ x: 60, y: 31, width: 185, height: 72 });
    expect(tooltipBox(chart, 790, 1)).toEqual({ x: 604, y: 31, width: 185, height: 52 });
  });

  test('series classes carry tone and dash modifiers', () => {
    expect(seriesClass({ key: 'a', label: 'A' }, 'line')).toBe('bb-line-series__line bb-line-series__line--accent');
    expect(seriesClass({ key: 'b', label: 'B', tone: 'warm', dashed: true }, 'key')).toBe(
      'bb-line-series__key bb-line-series__key--warm bb-line-series__key--dashed',
    );
  });
});
