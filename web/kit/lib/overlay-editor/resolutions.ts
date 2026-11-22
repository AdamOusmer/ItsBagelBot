// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument } from '../overlay-document';
import type { Canvas } from './defaults';

export const resolutions: readonly Canvas[] = [
  { width: 1920, height: 1080 }, { width: 1280, height: 720 }, { width: 2560, height: 1440 },
  { width: 3840, height: 2160 }, { width: 1080, height: 1920 }
];

export function resizeCanvas(doc: OverlayDocument, canvas: Canvas): OverlayDocument {
  const xScale = canvas.width / doc.width;
  const yScale = canvas.height / doc.height;
  const widgets = doc.widgets.map((widget) => ({
    ...widget,
    x: Math.round(widget.x * xScale),
    y: Math.round(widget.y * yScale),
    w: Math.max(1, Math.round(widget.w * xScale)),
    h: Math.max(1, Math.round(widget.h * yScale))
  }));
  return { ...doc, ...canvas, widgets };
}
