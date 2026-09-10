// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The geometry behind the `.bb-area` contract: buckets in, SVG path strings
// out. Framework-free so the Svelte and Astro adapters draw the same curve
// from the same arithmetic rather than from two transcriptions of it.
//
// It lived inside svelte/AreaSeries.svelte, whose header said there would be
// no Astro twin precisely because "the geometry below is the element, and a
// second implementation of it in another language is exactly the drift this
// package exists to delete". That reasoning was right about the risk and
// wrong about the remedy: the geometry is arithmetic over numbers, so it
// moves here and BOTH adapters call it. There is still exactly one
// implementation.

/** viewBox width. Unitless; the SVG scales with its container. */
export const AREA_W = 640;
/** Top of the plot band. */
export const AREA_PLOT_TOP = 12;
/** Baseline of the plot band, and the gridline drawn on it. */
export const AREA_PLOT_BOTTOM = 144;
/** The event-tick gutter, kept clear of the curve. */
export const AREA_TICK_TOP = 150;
export const AREA_TICK_BOTTOM = 160;
/** viewBox height: plot band plus tick gutter plus the dot's radius. */
export const AREA_H = 178;

export type AreaGeometry = {
  /** Closed path for the filled area. Empty when there is nothing to draw. */
  fill: string;
  /** Open path for the line. Empty when there is nothing to draw. */
  line: string;
  /** X positions of the event ticks, already clamped to the series. */
  marks: number[];
  /** Head of series, for the dot. Null when there is nothing to draw. */
  last: { x: number; y: number } | null;
};

/**
 * Build the paths for one series.
 *
 * FEWER THAN TWO POINTS DRAWS NOTHING, and that is a contract rather than a
 * guard: one point has no line and no scale to draw it against, and the
 * arithmetic below would emit `NaN` into the `d` attribute. A browser reports
 * a malformed path by silently not rendering it, so the failure would look
 * exactly like "the chart is empty" — which is also what the correct empty
 * render looks like, and that is how it would survive review. The parity
 * suite asserts on the NaN case for this reason.
 *
 * Scaled to the observed peak and never to zero: a flat series would divide by
 * zero, and a quiet series should still use the full box.
 *
 * Coordinates are rounded to one decimal. At a 640-unit viewBox scaled into a
 * ~600px box that is well under a device pixel, and it keeps the emitted path
 * short enough to diff — an unrounded 180-point series writes a 4 KB `d`.
 */
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
