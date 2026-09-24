// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const AREA_W = 640;
export const AREA_PLOT_TOP = 12;
export const AREA_PLOT_BOTTOM = 144;
export const AREA_TICK_TOP = 150;
export const AREA_TICK_BOTTOM = 160;
export const AREA_H = 178;

export type AreaGeometry = {
  fill: string;
  line: string;
  marks: number[];
  last: { x: number; y: number } | null;
};

export function areaGeometry(
  values: readonly number[],
  ticks: readonly number[] = [],
): AreaGeometry {
  if (values.length < 2) return { fill: '', line: '', marks: [], last: null };

  const top = Math.max(...values, 1);
  const step = AREA_W / (values.length - 1);
  const x = (i: number) => Math.round(i * step * 10) / 10;
  const y = (v: number) =>
    Math.round((AREA_PLOT_BOTTOM - (v / top) * (AREA_PLOT_BOTTOM - AREA_PLOT_TOP)) * 10) / 10;

  const points = values.map((v, i) => `${x(i)},${y(v)}`);
  const line = `M${points.join(' L')}`;
  return {
    line,
    fill: `${line} L${AREA_W},${AREA_PLOT_BOTTOM} L0,${AREA_PLOT_BOTTOM} Z`,
    marks: ticks.filter((i) => i >= 0 && i < values.length).map(x),
    last: { x: x(values.length - 1), y: y(values[values.length - 1]) },
  };
}
