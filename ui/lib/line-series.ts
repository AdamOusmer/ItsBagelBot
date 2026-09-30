// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { SeriesTone } from './tone';

type Millis = number;
type Px = number;
type SeriesKey = string;
type SeriesKeys = readonly SeriesKey[];

export type LinePoint = { readonly at: number; readonly [key: string]: number | null };

export type LineTone = SeriesTone;

export type LineSeriesSpec = { key: string; label: string; tone?: LineTone; dashed?: boolean };

export type LineTimeTick = { at: number; x: number; anchor: 'start' | 'middle' | 'end' };

export type LineValueTick = { value: number; y: number; label: string };

export type LineDot = { series: number; x: number; y: number };

export type LineChart = {
  width: number;
  height: number;
  left: number;
  right: number;
  top: number;
  bottom: number;
  plotWidth: number;
  plotHeight: number;
  startAt: number;
  duration: number;
  ceiling: number;
  readings: LinePoint[];
  latest: LinePoint | undefined;
  valueTicks: LineValueTick[];
  timeTicks: LineTimeTick[];
  paths: string[];
  dots: LineDot[];
  dotRadius: number;
  sampling: boolean;
};

export const LINE_DEFAULT_WIDTH = 800;

const MIN_WIDTH = 280;
const NARROW_WIDTH = 500;
const PAD = { left: 55, right: 18, top: 24, bottom: 34 };
const MIN_SPAN_MS = 8000;
const GAP_MS = 30_000;
const VALUE_TICKS = 5;
const TOOLTIP_WIDTH = 185;
const TOOLTIP_LINE = 20;
const NICE_STEPS = [1, 2, 4, 5];

export function plottable(value: number | null | undefined): number | null {
  if (value === null || value === undefined) return null;
  return Number.isFinite(value) && value >= 0 ? value : null;
}

export function niceCeiling(value: number): number {
  if (value <= 0) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(value));
  const normalized = value / magnitude;
  return (NICE_STEPS.find((step) => normalized <= step) ?? 10) * magnitude;
}

export function axisValue(value: number): string {
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}k`;
  if (value === 0) return '0';
  if (value < 1) return value.toFixed(2);
  return value.toFixed(value < 10 ? 1 : 0);
}

export function plainValue(value: number | null | undefined): string {
  const v = plottable(value);
  return v === null ? '' : axisValue(v);
}

export function clockTime(at: Millis): string {
  return new Date(at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function seriesClass(spec: LineSeriesSpec, part: string): string {
  const base = `bb-line-series__${part}`;
  const dashed = spec.dashed ? ` ${base}--dashed` : '';
  return `${base} ${base}--${spec.tone ?? 'accent'}${dashed}`;
}

export function lineX(chart: LineChart, at: Millis): Px {
  return chart.left + ((at - chart.startAt) / chart.duration) * chart.plotWidth;
}

export function lineY(chart: LineChart, value: number): number {
  return chart.top + chart.plotHeight * (1 - value / chart.ceiling);
}

function anchorOf(index: number, count: number): LineTimeTick['anchor'] {
  if (index === 0) return 'start';
  return index === count - 1 ? 'end' : 'middle';
}

function peak(readings: readonly LinePoint[], keys: SeriesKeys): number {
  return Math.max(0, ...readings.flatMap((point) => keys.map((key) => plottable(point[key]) ?? 0)));
}

function hasValue(point: LinePoint, keys: SeriesKeys): boolean {
  return keys.some((key) => plottable(point[key]) !== null);
}

function seriesPath(chart: LineChart, key: SeriesKey): string {
  let previousAt: number | null = null;
  return chart.readings
    .map((point) => {
      const value = plottable(point[key]);
      if (value === null) {
        previousAt = null;
        return '';
      }
      const command = previousAt !== null && point.at - previousAt <= GAP_MS ? 'L' : 'M';
      previousAt = point.at;
      return `${command}${lineX(chart, point.at).toFixed(2)},${lineY(chart, value).toFixed(2)}`;
    })
    .join(' ');
}

function seriesDots(chart: LineChart, keys: SeriesKeys): LineDot[] {
  return chart.readings.flatMap((point) =>
    keys.flatMap((key, series) => {
      const value = plottable(point[key]);
      return value === null ? [] : [{ series, x: lineX(chart, point.at), y: lineY(chart, value) }];
    }),
  );
}

function frame(readings: LinePoint[], keys: SeriesKeys, containerWidth: Px): LineChart {
  const width = Math.max(MIN_WIDTH, containerWidth || LINE_DEFAULT_WIDTH);
  const height = width < NARROW_WIDTH ? 220 : 260;
  const firstAt = readings[0]?.at ?? 0;
  const lastAt = readings.at(-1)?.at ?? 0;
  const duration = Math.max(MIN_SPAN_MS, lastAt - firstAt);
  return {
    width,
    height,
    ...PAD,
    plotWidth: width - PAD.left - PAD.right,
    plotHeight: height - PAD.top - PAD.bottom,
    startAt: lastAt - duration,
    duration,
    ceiling: niceCeiling(peak(readings, keys)),
    readings,
    latest: readings.at(-1),
    valueTicks: [],
    timeTicks: [],
    paths: [],
    dots: [],
    dotRadius: readings.length < 3 ? 3.5 : 1.5,
    sampling: readings.filter((point) => hasValue(point, keys)).length < 2,
  };
}

export function lineChart(points: readonly LinePoint[], keys: SeriesKeys, containerWidth: Px): LineChart {
  const readings = points.filter((point) => Number.isFinite(point.at)).toSorted((a, b) => a.at - b.at);
  const chart = frame(readings, keys, containerWidth);
  const timeCount = chart.width < NARROW_WIDTH ? 3 : 5;
  chart.valueTicks = Array.from({ length: VALUE_TICKS }, (_, index) => {
    const value = (chart.ceiling * index) / (VALUE_TICKS - 1);
    return { value, y: lineY(chart, value), label: axisValue(value) };
  });
  chart.timeTicks = readings.length
    ? Array.from({ length: timeCount }, (_, index) => {
        const at = chart.startAt + (chart.duration * index) / (timeCount - 1);
        return { at, x: lineX(chart, at), anchor: anchorOf(index, timeCount) };
      })
    : [];
  chart.paths = keys.map((key) => seriesPath(chart, key));
  chart.dots = seriesDots(chart, keys);
  return chart;
}

export function hoverTime(chart: LineChart, localX: Px): Millis {
  const fraction = Math.min(1, Math.max(0, (localX - chart.left) / chart.plotWidth));
  return chart.startAt + fraction * chart.duration;
}

export function nearestPoint(readings: readonly LinePoint[], at: Millis): LinePoint | null {
  let closest: LinePoint | null = null;
  for (const point of readings) {
    if (!closest || Math.abs(point.at - at) < Math.abs(closest.at - at)) closest = point;
  }
  return closest;
}

export function tooltipBox(chart: LineChart, x: Px, lines: number): { x: number; y: number; width: number; height: number } {
  return {
    x: Math.min(chart.width - 196, Math.max(chart.left + 5, x - 88)),
    y: chart.top + 7,
    width: TOOLTIP_WIDTH,
    height: 32 + TOOLTIP_LINE * lines,
  };
}
