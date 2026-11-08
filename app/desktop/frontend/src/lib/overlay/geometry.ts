// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { MIN_LAYER_SIZE } from './clamp';
import type { Layer } from './types';

export type Box = { x: number; y: number; w: number; h: number };
export type Handle = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw';
export type SnapTargets = { xs: number[]; ys: number[] };
export type Threshold = { x: number; y: number };
export type Snapped = { box: Box; guideX: number | null; guideY: number | null };

export const HANDLES: readonly Handle[] = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'];

const clamp = (value: number, low: number, high: number): number => Math.min(high, Math.max(low, value));

export function snapTargets(layers: readonly Layer[], movingId: string): SnapTargets {
  const others = layers.filter((layer) => layer.id !== movingId);
  return {
    xs: [0, 50, 100, ...others.flatMap((l) => [l.x, l.x + l.w / 2, l.x + l.w])],
    ys: [0, 50, 100, ...others.flatMap((l) => [l.y, l.y + l.h / 2, l.y + l.h])],
  };
}

function nearest(edges: readonly number[], targets: readonly number[], threshold: number) {
  let best: { delta: number; guide: number } | null = null;
  let bestGap = threshold;
  for (const edge of edges) {
    for (const target of targets) {
      const gap = Math.abs(target - edge);
      if (gap >= bestGap) continue;
      bestGap = gap;
      best = { delta: target - edge, guide: target };
    }
  }
  return best;
}

export function moveBox(box: Box, dx: number, dy: number, targets: SnapTargets, threshold: Threshold): Snapped {
  const x = clamp(box.x + dx, 0, 100 - box.w);
  const y = clamp(box.y + dy, 0, 100 - box.h);
  const snapX = nearest([x, x + box.w / 2, x + box.w], targets.xs, threshold.x);
  const snapY = nearest([y, y + box.h / 2, y + box.h], targets.ys, threshold.y);
  return {
    box: {
      x: clamp(x + (snapX?.delta ?? 0), 0, 100 - box.w),
      y: clamp(y + (snapY?.delta ?? 0), 0, 100 - box.h),
      w: box.w,
      h: box.h,
    },
    guideX: snapX?.guide ?? null,
    guideY: snapY?.guide ?? null,
  };
}

function moveEdge(value: number, targets: readonly number[], threshold: number, low: number, high: number) {
  const moved = clamp(value, low, high);
  const snap = nearest([moved], targets, threshold);
  return { value: clamp(moved + (snap?.delta ?? 0), low, high), guide: snap?.guide ?? null };
}

export function resizeBox(
  box: Box,
  handle: Handle,
  dx: number,
  dy: number,
  targets: SnapTargets,
  threshold: Threshold,
): Snapped {
  let left = box.x;
  let right = box.x + box.w;
  let top = box.y;
  let bottom = box.y + box.h;
  let guideX: number | null = null;
  let guideY: number | null = null;

  if (handle.includes('w')) {
    const edge = moveEdge(left + dx, targets.xs, threshold.x, 0, right - MIN_LAYER_SIZE);
    left = edge.value;
    guideX = edge.guide;
  }
  if (handle.includes('e')) {
    const edge = moveEdge(right + dx, targets.xs, threshold.x, left + MIN_LAYER_SIZE, 100);
    right = edge.value;
    guideX = edge.guide;
  }
  if (handle.includes('n')) {
    const edge = moveEdge(top + dy, targets.ys, threshold.y, 0, bottom - MIN_LAYER_SIZE);
    top = edge.value;
    guideY = edge.guide;
  }
  if (handle.includes('s')) {
    const edge = moveEdge(bottom + dy, targets.ys, threshold.y, top + MIN_LAYER_SIZE, 100);
    bottom = edge.value;
    guideY = edge.guide;
  }
  return { box: { x: left, y: top, w: right - left, h: bottom - top }, guideX, guideY };
}
