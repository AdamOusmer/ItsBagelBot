// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { beginPinch, fitBox, openBox, panelInsets, pinchView, revealBox } from './touch';

const ZOOM = { min: 0.25, max: 1.6 };
const screenOf =(view: { x: number; y: number; k: number }, p: { x: number; y: number }) => ({ x: view.x + p.x * view.k, y: view.y + p.y * view.k });

test.each([
  { width: 600, zoom: 1 },
  { width: 1400, zoom: 0.7 },
  { width: 1200, zoom: 0.7 },
  { width: 5000, zoom: 0.7 }
])('recipe opening uses whole-flow fit or the readable anchor at width $width', ({ width, zoom }) => {
  const frame = { width: 900, height: 900, inset: { top: 100, bottom: 100, left: 20, right: 40 } };
  const anchor = { x0: 0, y0: 0, x1: 336, y1: 154 };
  const view = openBox({ ...anchor, x1: width }, anchor, frame, ZOOM);
  expect(view.k).toBeCloseTo(zoom);
  expect(screenOf(view, { x: anchor.x0, y: anchor.y0 }).x).toBeGreaterThanOrEqual(20);
});

test('insets reserve both side panels and the toolbar', () => {
  const board = { top: 0, left: 240, right: 1680, bottom: 900, width: 1440, height: 900 };
  const bar = { ...board, bottom: 160, height: 160 };
  const rail = { ...board, left: 256, right: 328, width: 72, height: 240 };
  const panels = [
    { ...board, left: 340, right: 660, width: 320 },
    { ...board, left: 1304, right: 1664, width: 360 }
  ];
  expect(panelInsets({ board, bar, rail, panels })).toEqual({ top: 172, left: 432, right: 388, bottom: 76 });
});

const phoneBoard = { top: 0, left: 0, right: 320, bottom: 568, width: 320, height: 568 };
const phone = { board: phoneBoard, bar: { ...phoneBoard, bottom: 191, height: 191 }, rail: { ...phoneBoard, top: 403, bottom: 471, right: 232, width: 216, height: 68 } };
const tabletBoard = { top: 0, left: 0, right: 1024, bottom: 768, width: 1024, height: 768 };
const tablet = { board: tabletBoard, bar: { ...tabletBoard, bottom: 80, height: 80 }, rail: { ...tabletBoard, top: 96, bottom: 336, left: 16, right: 88, width: 72, height: 240 } };

test.each([
  { name: 'phone rail', frame: phone, sheet: 0, inset: { top: 203, left: 16, right: 16, bottom: 177 } },
  { name: 'phone sheet', frame: phone, sheet: 318, inset: { top: 203, left: 16, right: 16, bottom: 330 } },
  { name: 'tablet board', frame: tablet, sheet: 0, inset: { top: 92, left: 100, right: 16, bottom: 76 } },
  { name: 'tablet bottom panel keeps the zoom row above it', frame: tablet, sheet: 315, inset: { top: 92, left: 100, right: 16, bottom: 391 } }
])('sheet insets: $name', ({ frame, sheet, inset }) => {
  expect(panelInsets(frame, sheet)).toEqual(inset);
});

describe('pinchView', () => {
  const start = { x: 40, y: -20, k: 0.5 };
  const a = { x: 100, y: 200 };
  const b = { x: 200, y: 200 };

  test('keeps the world point between the fingers under them', () => {
    const pinch = beginPinch(start, a, b);
    const next = pinchView(pinch, { x: 50, y: 200 }, { x: 250, y: 200 }, ZOOM);
    expect(next.k).toBeCloseTo(1);
    const s = screenOf(next, pinch.world);
    expect(s.x).toBeCloseTo(150);
    expect(s.y).toBeCloseTo(200);
  });

  test('moving both fingers together pans at the same zoom', () => {
    const next = pinchView(beginPinch(start, a, b), { x: 130, y: 240 }, { x: 230, y: 240 }, ZOOM);
    expect(next.k).toBeCloseTo(0.5);
    expect(next.x).toBeCloseTo(70);
    expect(next.y).toBeCloseTo(20);
  });

  test('stops at the zoom limits and still tracks the fingers', () => {
    const pinch = beginPinch(start, a, b);
    const next = pinchView(pinch, { x: 149, y: 200 }, { x: 151, y: 200 }, ZOOM);
    expect(next.k).toBe(0.25);
    expect(screenOf(next, pinch.world).x).toBeCloseTo(150);
  });

  test('two fingers landing on one spot do not divide by zero', () => {
    const next = pinchView(beginPinch(start, a, a), a, b, ZOOM);
    expect(Number.isFinite(next.k)).toBe(true);
    expect(next.k).toBe(1.6);
  });
});

describe('fitBox', () => {
  const box = { x0: -152, y0: -48, x1: 1032, y1: 208 };
  const phone = { top: 146, right: 16, bottom: 170, left: 16 };

  test('fits the whole flow inside the room the toolbars leave', () => {
    const view = fitBox(box, { width: 375, height: 812, inset: phone }, ZOOM);
    const tl = screenOf(view, { x: box.x0, y: box.y0 });
    const br = screenOf(view, { x: box.x1, y: box.y1 });
    expect([tl.x >= 16, br.x <= 359, tl.y >= 146, br.y <= 642]).toEqual([true, true, true, true]);
  });

  test('never zooms in past 100% for a small flow', () => {
    expect(fitBox({ x0: 0, y0: 0, x1: 100, y1: 100 }, { width: 375, height: 812, inset: phone }, ZOOM).k).toBe(1);
  });
});

describe('revealBox', () => {
  const inset = { top: 140, right: 16, bottom: 470, left: 16 };
  const node = { x0: 744, y0: 96, x1: 1080, y1: 180 };
  const frame = { width: 375, height: 812, inset };

  test('brings an off screen step between the toolbar and the sheet', () => {
    const view = revealBox({ x: 10, y: 300, k: 0.3 }, node, frame, { ...ZOOM, readable: 0.75 });
    const tl = screenOf(view, { x: node.x0, y: node.y0 });
    const br = screenOf(view, { x: node.x1, y: node.y1 });
    expect([tl.x >= 16, br.x <= 359, tl.y >= 140, br.y <= 342, view.k]).toEqual([true, true, true, true, 1]);
  });

  test('shrinks when the step would not fit at the readable zoom', () => {
    const view = revealBox({ x: 0, y: 0, k: 1.5 }, node, frame, { ...ZOOM, readable: 0.75 });
    expect(view.k).toBe(1);
  });

  test('leaves the view alone when the step is already in the room', () => {
    const current = { x: 16 - 744 * 0.9, y: 150 - 96 * 0.9, k: 0.9 };
    expect(revealBox(current, node, frame, { ...ZOOM, readable: 0.75 })).toBe(current);
  });

  test.each([0.25, 0.4])('keeps the readable zoom in an obstructed frame with minimum %s', (min) => {
    const view = revealBox({ x: 0, y: 0, k: 1 }, node, { ...frame, inset: { ...inset, right: 800 } }, { ...ZOOM, min, readable: 0.75 });
    expect([view.k, Number.isFinite(view.x), Number.isFinite(view.y)]).toEqual([0.75, true, true]);
  });
});
