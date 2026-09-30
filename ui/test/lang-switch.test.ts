// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { placeMenu } from '../lib/lang-switch';

const viewport = { width: 1280, height: 800 };
const menu = { width: 208, height: 240 };
const rect = (top: number, left: number, width = 60, height = 32) => ({
  top,
  bottom: top + height,
  left,
  right: left + width,
});

test('opens below, right edges aligned, when there is room', () => {
  expect(placeMenu(rect(20, 1000), menu, viewport, 'end')).toEqual({
    x: 852,
    y: 60,
    side: 'bottom',
    maxHeight: 732,
  });
});

test('field trigger aligns left edges', () => {
  expect(placeMenu(rect(20, 100, 220, 40), menu, viewport, 'start').x).toBe(100);
});

test('flips above a trigger near the bottom of the viewport', () => {
  const placement = placeMenu(rect(700, 100), menu, viewport, 'start');
  expect(placement.side).toBe('top');
  expect(placement.y).toBe(700 - 8 - 240);
});

test('stays below when neither side fits and below has more room, capped to it', () => {
  const placement = placeMenu(rect(60, 100), { width: 208, height: 900 }, viewport, 'start');
  expect(placement.side).toBe('bottom');
  expect(placement.maxHeight).toBe(800 - 92 - 16);
});

test('clamps inside the viewport edges', () => {
  expect(placeMenu(rect(20, 10), menu, viewport, 'end').x).toBe(8);
  expect(placeMenu(rect(20, 1250), menu, viewport, 'start').x).toBe(1280 - 8 - 208);
});
