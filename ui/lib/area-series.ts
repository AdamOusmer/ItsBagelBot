// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const AREA_W = 640;
export const AREA_PLOT_TOP = 12;
export const AREA_PLOT_BOTTOM = 144;
export const AREA_TICK_TOP = 150;
export const AREA_TICK_BOTTOM = 160;
export const AREA_H = 178;

const TIP_EDGE = 0.25;
const TIP_CEILING = 0.4;

export type AreaGeometry = {
  fill: string;
  line: string;
  marks: number[];
};

export type AreaFraction = number;
export type AreaIndex = number;
export type AreaCount = number;
export type AreaValue = number;
export type AreaUnit = string;

export type AreaPoint = { x: AreaFraction; y: AreaFraction };

export type AreaTipSide = 'start' | 'center' | 'end';

export type AreaKey = 'ArrowLeft' | 'ArrowRight' | 'Home' | 'End' | 'Escape';

function ceiling(values: readonly number[]): number {
  return Math.max(...values, 1);
}

function plotY(value: AreaValue, top: AreaValue): number {
  return AREA_PLOT_BOTTOM - (value / top) * (AREA_PLOT_BOTTOM - AREA_PLOT_TOP);
}

export function areaGeometry(
  values: readonly number[],
  ticks: readonly number[] = [],
): AreaGeometry {
  if (values.length < 2) return { fill: '', line: '', marks: [] };

  const top = ceiling(values);
  const step = AREA_W / (values.length - 1);
  const x = (i: AreaIndex) => Math.round(i * step * 10) / 10;
  const y = (v: AreaValue) => Math.round(plotY(v, top) * 10) / 10;

  const points = values.map((v, i) => `${x(i)},${y(v)}`);
  const line = `M${points.join(' L')}`;
  return {
    line,
    fill: `${line} L${AREA_W},${AREA_PLOT_BOTTOM} L0,${AREA_PLOT_BOTTOM} Z`,
    marks: ticks.filter((i) => i >= 0 && i < values.length).map(x),
  };
}

function inside(index: AreaIndex, count: AreaCount): boolean {
  return index >= 0 && index < count;
}

export function pointAt(values: readonly number[], index: AreaIndex): AreaPoint | null {
  if (values.length < 2 || !inside(index, values.length)) return null;
  return {
    x: index / (values.length - 1),
    y: plotY(values[index], ceiling(values)) / AREA_H,
  };
}

export function nearestIndex(fraction: AreaFraction, count: AreaCount): AreaIndex | null {
  if (count < 2 || Number.isNaN(fraction)) return null;
  const clamped = Math.min(1, Math.max(0, fraction));
  return Math.round(clamped * (count - 1));
}

export function stepIndex(current: AreaIndex | null, key: AreaKey, count: AreaCount): AreaIndex | null {
  if (count < 2 || key === 'Escape') return null;
  const last = count - 1;
  const from = current ?? last;
  const next: Record<Exclude<AreaKey, 'Escape'>, number> = {
    ArrowLeft: from - (current === null ? 0 : 1),
    ArrowRight: from + (current === null ? 0 : 1),
    Home: 0,
    End: last,
  };
  return Math.min(last, Math.max(0, next[key]));
}

export function isAreaKey(key: string): key is AreaKey {
  return ['ArrowLeft', 'ArrowRight', 'Home', 'End', 'Escape'].includes(key);
}

export function tipSide(fraction: AreaFraction): AreaTipSide {
  if (fraction < TIP_EDGE) return 'start';
  return fraction > 1 - TIP_EDGE ? 'end' : 'center';
}

export function tipRow(y: AreaFraction): 'high' | 'low' {
  return y < TIP_CEILING ? 'low' : 'high';
}

export function percent(fraction: AreaFraction): string {
  return `${Math.round(fraction * 100000) / 1000}%`;
}

export function plainValue(value: AreaValue): string {
  return value.toLocaleString('en-US');
}

export function valueText(value: AreaValue, format: (value: AreaValue) => string, unit: AreaUnit): string {
  return unit ? `${format(value)} ${unit}` : format(value);
}
