// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { beginPinch, fitBox, pinchView, revealBox } from './touch';

const screenOf = (view: { x: number; y: number; k: number }, p: { x: number; y: number }) => ({ x: view.x + p.x * view.k, y: view.y + p.y * view.k });

describe('pinchView', () => {
  const start = { x: 40, y: -20, k: 0.5 };
  const a = { x: 100, y: 200 };
  const b = { x: 200, y: 200 };

  test('keeps the world point between the fingers under them', () => {
    const pinch = beginPinch(start, a, b);
    const next = pinchView(pinch, { x: 50, y: 200 }, { x: 250, y: 200 }, 0.25, 1.6);
    expect(next.k).toBeCloseTo(1);
    const s = screenOf(next, pinch.world);
    expect(s.x).toBeCloseTo(150);
    expect(s.y).toBeCloseTo(200);
  });

  test('moving both fingers together pans at the same zoom', () => {
    const next = pinchView(beginPinch(start, a, b), { x: 130, y: 240 }, { x: 230, y: 240 }, 0.25, 1.6);
    expect(next.k).toBeCloseTo(0.5);
    expect(next.x).toBeCloseTo(70);
    expect(next.y).toBeCloseTo(20);
  });

  test('stops at the zoom limits and still tracks the fingers', () => {
    const pinch = beginPinch(start, a, b);
    const next = pinchView(pinch, { x: 149, y: 200 }, { x: 151, y: 200 }, 0.25, 1.6);
    expect(next.k).toBe(0.25);
    expect(screenOf(next, pinch.world).x).toBeCloseTo(150);
  });

  test('two fingers landing on one spot do not divide by zero', () => {
    const next = pinchView(beginPinch(start, a, a), a, b, 0.25, 1.6);
    expect(Number.isFinite(next.k)).toBe(true);
    expect(next.k).toBe(1.6);
  });
});

describe('fitBox', () => {
  const box = { x0: -152, y0: -48, x1: 1032, y1: 208 };
  const phone = { top: 146, right: 16, bottom: 170, left: 16 };

  test('fits the whole flow inside the room the toolbars leave', () => {
    const view = fitBox(box, 375, 812, phone, 0.25, 1.6);
    const tl = screenOf(view, { x: box.x0, y: box.y0 });
    const br = screenOf(view, { x: box.x1, y: box.y1 });
    expect(tl.x).toBeGreaterThanOrEqual(16);
    expect(br.x).toBeLessThanOrEqual(375 - 16);
    expect(tl.y).toBeGreaterThanOrEqual(146);
    expect(br.y).toBeLessThanOrEqual(812 - 170);
  });

  test('never zooms in past 100% for a small flow', () => {
    expect(fitBox({ x0: 0, y0: 0, x1: 100, y1: 100 }, 375, 812, phone, 0.25, 1.6).k).toBe(1);
  });
});

describe('revealBox', () => {
  const inset = { top: 140, right: 16, bottom: 470, left: 16 };
  const node = { x0: 744, y0: 96, x1: 1080, y1: 180 };

  test('brings an off screen step between the toolbar and the sheet', () => {
    const view = revealBox({ x: 10, y: 300, k: 0.3 }, node, 375, 812, inset, 0.75);
    const tl = screenOf(view, { x: node.x0, y: node.y0 });
    const br = screenOf(view, { x: node.x1, y: node.y1 });
    expect(tl.x).toBeGreaterThanOrEqual(16);
    expect(br.x).toBeLessThanOrEqual(359);
    expect(tl.y).toBeGreaterThanOrEqual(140);
    expect(br.y).toBeLessThanOrEqual(342);
    expect(view.k).toBeCloseTo(0.75);
  });

  test('shrinks when the step would not fit at the readable zoom', () => {
    const view = revealBox({ x: 0, y: 0, k: 1.5 }, node, 375, 812, inset, 0.75);
    expect(view.k).toBeCloseTo(343 / 336);
  });

  test('leaves the view alone when the step is already in the room', () => {
    const current = { x: 16 - 744 * 0.9, y: 150 - 96 * 0.9, k: 0.9 };
    expect(revealBox(current, node, 375, 812, inset, 0.75)).toBe(current);
  });
});
