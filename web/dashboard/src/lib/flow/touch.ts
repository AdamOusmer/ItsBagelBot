// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
export type Pt = { x: number; y: number };
export type View = { x: number; y: number; k: number };
export type Box = { x0: number; y0: number; x1: number; y1: number };
export type Inset = { top: number; right: number; bottom: number; left: number };
export type Frame = { width: number; height: number; inset: Inset };
export type ZoomRange = { min: number; max: number };

const clamp =(v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));
const middle = (a: Pt, b: Pt): Pt => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 });

export type Pinch = { world: Pt; dist: number; k: number };

export function beginPinch(view: View, a: Pt, b: Pt): Pinch {
  const m = middle(a, b);
  return { world: { x: (m.x - view.x) / view.k, y: (m.y - view.y) / view.k }, dist: Math.max(1, Math.hypot(a.x - b.x, a.y - b.y)), k: view.k };
}

export function pinchView(pinch: Pinch, a: Pt, b: Pt, zoom: ZoomRange): View {
  const m = middle(a, b);
  const k = clamp((pinch.k * Math.hypot(a.x - b.x, a.y - b.y)) / pinch.dist, zoom.min, zoom.max);
  return { k, x: m.x - pinch.world.x * k, y: m.y - pinch.world.y * k };
}

export function fitBox(box: Box, frame: Frame, zoom: ZoomRange): View {
  const { inset } = frame;
  const roomW = frame.width - inset.left - inset.right;
  const roomH = frame.height - inset.top - inset.bottom;
  const k = clamp(Math.min(roomW / (box.x1 - box.x0), roomH / (box.y1 - box.y0), 1), zoom.min, zoom.max);
  return {
    k,
    x: inset.left + roomW / 2 - ((box.x0 + box.x1) / 2) * k,
    y: inset.top + roomH / 2 - ((box.y0 + box.y1) / 2) * k
  };
}

export function revealBox(view: View, box: Box, frame: Frame, readableZoom: number): View {
  const { width, height, inset } = frame;
  const roomW = width - inset.left - inset.right;
  const roomH = height - inset.top - inset.bottom;
  const boxW = box.x1 - box.x0;
  const boxH = box.y1 - box.y0;
  const k = Math.min(Math.max(view.k, readableZoom), roomW / boxW, roomH / boxH);
  const left = view.x + box.x0 * k;
  const top = view.y + box.y0 * k;
  const inside =
    k === view.k && left >= inset.left && top >= inset.top && left + boxW * k <= width - inset.right && top + boxH * k <= height - inset.bottom;
  if (inside) return view;
  return {
    k,
    x: inset.left + (roomW - boxW * k) / 2 - box.x0 * k,
    y: inset.top + (roomH - boxH * k) / 2 - box.y0 * k
  };
}
