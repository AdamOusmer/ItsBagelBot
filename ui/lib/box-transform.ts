// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Box = { x: number; y: number; w: number; h: number };
export type Size = { width: number; height: number };
export type Point = { x: number; y: number };
export type Guides = { x?: number; y?: number };
export const HANDLES = ['nw', 'ne', 'sw', 'se'] as const;
export type Handle = (typeof HANDLES)[number] | 'move';
export type Subject = { box: Box; others: readonly Box[]; canvas: Size };
export type Motion = { handle: Handle; threshold: number };

type Axis = { start: number; size: number; span: number; targets: number[] };
type Move = { delta: number; threshold: number; handle: Handle };
type Target = { position: number; guide?: number };
type Snapped = { delta: number; guide?: number };
type Moved = { start: number; size: number; guide?: number };

export const clamp = (value: number, min: number, max: number): number => Math.max(min, Math.min(max, value));

export function clampBox(box: Box, canvas: Size): Box {
  const w = clamp(box.w, 1, canvas.width);
  const h = clamp(box.h, 1, canvas.height);
  return { x: clamp(box.x, 0, canvas.width - w), y: clamp(box.y, 0, canvas.height - h), w, h };
}

function edges(start: number, size: number): number[] {
  return [start, start + size / 2, start + size];
}

function closest(position: number, targets: number[], threshold: number): Target {
  return targets.reduce<Target>(
    (best, target) => {
      if (Math.abs(target - position) >= Math.abs(best.position - position)) return best;
      return { position: target, guide: target };
    },
    { position: position + threshold },
  );
}

function snap(candidates: number[], targets: number[], threshold: number): Snapped {
  return candidates.reduce<Snapped>(
    (best, edge) => {
      const target = closest(edge, targets, Math.abs(best.delta));
      return target.guide === undefined ? best : { delta: target.position - edge, guide: target.guide };
    },
    { delta: threshold },
  );
}

function moveAxis(axis: Axis, motion: Move, lowHandle: 'w' | 'n'): Moved {
  const { start, size, span, targets } = axis;
  const edge = start + size;
  const leading = motion.handle.includes(lowHandle);
  const moving = motion.handle === 'move';
  const origin = moving || leading ? start : edge;
  const candidates = moving ? edges(origin + motion.delta, size) : [origin + motion.delta];
  const snapped = snap(candidates, targets, motion.threshold);
  const position = origin + motion.delta + (snapped.guide === undefined ? 0 : snapped.delta);
  if (moving) return { start: clamp(position, 0, span - size), size, guide: snapped.guide };
  if (leading) {
    const next = clamp(position, 0, edge - 1);
    return { start: next, size: edge - next, guide: snapped.guide };
  }
  return { start, size: clamp(position, start + 1, span) - start, guide: snapped.guide };
}

export function transformBox(subject: Subject, delta: Point, motion: Motion): { box: Box; guides: Guides } {
  const { box, others, canvas } = subject;
  const x = moveAxis(
    { start: box.x, size: box.w, span: canvas.width, targets: [...edges(0, canvas.width), ...others.flatMap((item) => edges(item.x, item.w))] },
    { ...motion, delta: delta.x },
    'w',
  );
  const y = moveAxis(
    { start: box.y, size: box.h, span: canvas.height, targets: [...edges(0, canvas.height), ...others.flatMap((item) => edges(item.y, item.h))] },
    { ...motion, delta: delta.y },
    'n',
  );
  return {
    box: { x: Math.round(x.start), y: Math.round(y.start), w: Math.round(x.size), h: Math.round(y.size) },
    guides: { x: x.guide, y: y.guide },
  };
}
