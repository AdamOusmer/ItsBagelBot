// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { clampBox, transformBox, type Box, type Handle } from '../lib/box-transform';

const canvas = { width: 1000, height: 500 };
const box: Box = { x: 100, y: 100, w: 200, h: 100 };
const neighbour: Box = { x: 600, y: 300, w: 100, h: 100 };

function run(handle: Handle, delta: { x: number; y: number }, threshold = 0) {
  return transformBox({ box, others: [neighbour], canvas }, delta, { handle, threshold });
}

test.each([
  { name: 'moves freely', handle: 'move' as const, delta: { x: 50, y: 20 }, want: { x: 150, y: 120, w: 200, h: 100 } },
  { name: 'stays inside the canvas', handle: 'move' as const, delta: { x: 5000, y: -5000 }, want: { x: 800, y: 0, w: 200, h: 100 } },
  { name: 'grows from the south east corner', handle: 'se' as const, delta: { x: 40, y: 30 }, want: { x: 100, y: 100, w: 240, h: 130 } },
  { name: 'shrinks from the north west corner', handle: 'nw' as const, delta: { x: 20, y: 10 }, want: { x: 120, y: 110, w: 180, h: 90 } },
  { name: 'never collapses below one pixel', handle: 'se' as const, delta: { x: -900, y: -900 }, want: { x: 100, y: 100, w: 1, h: 1 } },
])('transformBox $name', ({ handle, delta, want }) => {
  expect(run(handle, delta).box).toEqual(want);
});

test('transformBox snaps to the canvas centre inside the threshold and reports the guide', () => {
  const result = run('move', { x: 296, y: 0 }, 8);
  expect([result.box.x, result.guides.x]).toEqual([400, 500]);
});

test('transformBox ignores edges beyond the threshold', () => {
  expect(run('move', { x: 296, y: 0 }, 2).guides.x).toBeUndefined();
});

test('clampBox keeps a box inside the canvas and at least one pixel wide', () => {
  expect(clampBox({ x: 990, y: -5, w: 0, h: 9000 }, canvas)).toEqual({ x: 990, y: 0, w: 1, h: 500 });
});
