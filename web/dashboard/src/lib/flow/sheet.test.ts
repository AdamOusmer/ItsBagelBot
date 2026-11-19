// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { fieldScrollDelta } from './sheet';

const view = { top: 64, bottom: 238 };

test.each([
  { name: 'a visible field stays put', field: { top: 120, bottom: 143 }, delta: 0 },
  { name: 'a field under the keyboard scrolls up to the middle', field: { top: 444, bottom: 467 }, delta: 304.5 },
  { name: 'a field above the visible area scrolls down to the middle', field: { top: 20, bottom: 43 }, delta: -119.5 },
  { name: 'a field cut by the keyboard edge scrolls fully into view', field: { top: 230, bottom: 253 }, delta: 90.5 }
])('$name', ({ field, delta }) => {
  expect(fieldScrollDelta(field, view)).toBe(delta);
});
