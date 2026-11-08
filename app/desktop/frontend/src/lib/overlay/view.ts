// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type View = { x: number; y: number; k: number };
export type Room = { width: number; height: number; top: number };
export type CanvasSize = { width: number; height: number };

const MIN_ZOOM = 0.1;
const MAX_ZOOM = 3;
const PAN_MARGIN = 80;

const clamp = (value: number, low: number, high: number): number => Math.min(high, Math.max(low, value));

export function fitCanvas(room: Room, canvas: CanvasSize): View {
  const usableH = Math.max(1, room.height - room.top);
  const k = Math.max(MIN_ZOOM, Math.min(room.width / canvas.width, usableH / canvas.height));
  return {
    k,
    x: (room.width - canvas.width * k) / 2,
    y: room.top + (usableH - canvas.height * k) / 2,
  };
}

export function zoomAround(view: View, factor: number, px: number, py: number): View {
  const k = clamp(view.k * factor, MIN_ZOOM, MAX_ZOOM);
  const ratio = k / view.k;
  return { k, x: px - (px - view.x) * ratio, y: py - (py - view.y) * ratio };
}

export function clampPan(view: View, room: Room, canvas: CanvasSize): View {
  return {
    k: view.k,
    x: clamp(view.x, PAN_MARGIN - canvas.width * view.k, room.width - PAN_MARGIN),
    y: clamp(view.y, room.top + PAN_MARGIN - canvas.height * view.k, room.height - PAN_MARGIN),
  };
}
