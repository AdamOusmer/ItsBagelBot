// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { keyValue, pointerValue, snapValue } from '../lib/slider';

const RANGE = { min: 1, max: 60, step: 1 };
const TENTHS = { min: 0, max: 1, step: 0.1 };
const RAIL = { left: 100, width: 200 };

test.each([
  ['snaps to the nearest step', snapValue(7.6, RANGE), 8],
  ['clamps below min', snapValue(-4, RANGE), 1],
  ['clamps above max', snapValue(99, RANGE), 60],
  ['keeps decimal steps exact', snapValue(0.30000000000000004, TENTHS), 0.3],
  ['pointer at the left edge is min', pointerValue(100, RAIL, RANGE), 1],
  ['pointer past the right edge is max', pointerValue(400, RAIL, RANGE), 60],
  ['pointer in the middle lands on a step', pointerValue(200, RAIL, RANGE), 31],
  ['arrow right adds one step', keyValue('ArrowRight', 5, RANGE), 6],
  ['arrow down removes one step', keyValue('ArrowDown', 5, RANGE), 4],
  ['page up adds ten steps', keyValue('PageUp', 5, RANGE), 15],
  ['page down stops at min', keyValue('PageDown', 5, RANGE), 1],
  ['home jumps to min', keyValue('Home', 30, RANGE), 1],
  ['end jumps to max', keyValue('End', 30, RANGE), 60],
  ['other keys are ignored', keyValue('a', 30, RANGE), null],
])('%s', (_name, actual, expected) => {
  expect(actual).toBe(expected);
});
