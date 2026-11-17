// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
export type Pt = { x: number; y: number };
export type View = { x: number; y: number; k: number };
export type Box = { x0: number; y0: number; x1: number; y1: number };
export type Inset = { top: number; right: number; bottom: number; left: number };
export type Frame = { width: number; height: number; inset: Inset };
export type ZoomRange = { min: number; max: number };

type Rect = Pick<DOMRect, 'top' | 'right' | 'bottom' | 'left' | 'width' | 'height'>;
type Panels = { board: Rect; bar: Rect; rail: Rect; panels?: Rect[] };
const PANEL_GAP = 12;

export function panelInsets({ board, bar, rail, panels = [] }: Panels, panelHeight = 0): Inset {
  const railIsRow = rail.width > rail.height;
  const inset = {
    top: bar.bottom - board.top + PANEL_GAP,
    right: 16,
    bottom: Math.max(panelHeight, railIsRow ? board.bottom - rail.top : 0) + PANEL_GAP,
    left: railIsRow ? 16 : rail.right - board.left + PANEL_GAP
  };
  for (const panel of panels) reservePanel(inset, board, panel);
  return { ...inset, bottom: inset.bottom + (railIsRow ? 0 : 64) };
}

function reservePanel(inset: Inset, board: Rect, panel: Rect) {
  if (panel.width >= board.width - 32) {
    inset.bottom = Math.max(inset.bottom, board.bottom - panel.top + PANEL_GAP);
    return;
  }
  if (panel.right >= board.right - 32) inset.right = Math.max(inset.right, board.right - panel.left + PANEL_GAP);
  else inset.left = Math.max(inset.left, panel.right - board.left + PANEL_GAP);
}

export function keyboardSize(layoutHeight: number, viewport: Pick<VisualViewport, 'height' | 'offsetTop'> | null): { height: number; keyboard: number } {
  const height = viewport?.height ?? layoutHeight;
  return { height, keyboard: Math.max(0, layoutHeight - height - (viewport?.offsetTop ?? 0)) };
}

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

export function revealBox(view: View, box: Box, frame: Frame, zoom: ZoomRange & { readable: number }): View {
  const { width, height, inset } = frame;
  const roomW = width - inset.left - inset.right;
  const roomH = height - inset.top - inset.bottom;
  const boxW = box.x1 - box.x0;
  const boxH = box.y1 - box.y0;
  const ceiling = view.k >= zoom.readable ? Math.min(1, view.k) : 1;
  const k = clamp(Math.min(roomW / boxW, roomH / boxH), Math.max(zoom.min, zoom.readable), Math.min(ceiling, zoom.max));
  const left = view.x + box.x0 * k;
  const top = view.y + box.y0 * k;
  const horizontal = left >= inset.left && left + boxW * k <= width - inset.right;
  const vertical = top >= inset.top && top + boxH * k <= height - inset.bottom;
  const inside = k === view.k && horizontal && vertical;
  if (inside) return view;
  return {
    k,
    x: clamp(view.x, inset.left - box.x0 * k, Math.max(inset.left - box.x0 * k, width - inset.right - box.x1 * k)),
    y: clamp(view.y, inset.top - box.y0 * k, Math.max(inset.top - box.y0 * k, height - inset.bottom - box.y1 * k))
  };
}

export function openBox(box: Box, anchor: Box, frame: Frame, zoom: ZoomRange): View {
  const fitted = fitBox(box, frame, zoom);
  if (fitted.k >= 0.7) return fitted;
  const readable = fitBox(anchor, frame, { min: 0.7, max: 0.7 });
  return revealBox(readable, anchor, frame, { ...zoom, readable: 0.7 });
}
