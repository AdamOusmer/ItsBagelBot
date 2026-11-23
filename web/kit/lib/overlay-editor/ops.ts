// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument, OverlayWidget } from '../overlay-document';
import { clampBox } from '@bagel/ui/lib/box-transform';
import type { Canvas } from './defaults';

const COPY_OFFSET = 18;
const NAME_LIMIT = 120;
const COPY_SUFFIX = ' copy';

export function replaceWidget(doc: OverlayDocument, widget: OverlayWidget): OverlayDocument {
  return { ...doc, widgets: doc.widgets.map((item) => (item.id === widget.id ? widget : item)) };
}

export function addWidget(doc: OverlayDocument, widget: OverlayWidget): OverlayDocument {
  return { ...doc, widgets: [...doc.widgets, widget] };
}

export function removeWidget(doc: OverlayDocument, id: string): OverlayDocument {
  return { ...doc, widgets: doc.widgets.filter((item) => item.id !== id) };
}

export function copyOf(doc: OverlayDocument, widget: OverlayWidget): OverlayWidget {
  const box = clampBox({ ...widget, x: widget.x + COPY_OFFSET, y: widget.y + COPY_OFFSET }, doc);
  const name = (widget.name + COPY_SUFFIX).slice(0, NAME_LIMIT);
  return { ...structuredClone(widget), ...box, id: crypto.randomUUID(), name };
}

function isMove(from: number, to: number, length: number): boolean {
  const inRange = from >= 0 && to >= 0 && to < length;
  return inRange && to !== from;
}

export function moveWidget(doc: OverlayDocument, id: string, to: number): OverlayDocument {
  const widgets = doc.widgets.slice();
  const from = widgets.findIndex((item) => item.id === id);
  if (!isMove(from, to, widgets.length)) return doc;
  const [moved] = widgets.splice(from, 1);
  widgets.splice(to, 0, moved!);
  return { ...doc, widgets };
}

export function centerPosition(widget: OverlayWidget, canvas: Canvas): { x: number; y: number } {
  return { x: Math.round((canvas.width - widget.w) / 2), y: Math.round((canvas.height - widget.h) / 2) };
}

export function reorderWidget(doc: OverlayDocument, id: string, offset: -1 | 1): OverlayDocument {
  const widgets = doc.widgets.slice();
  const index = widgets.findIndex((item) => item.id === id);
  const next = index + offset;
  if (index < 0 || next < 0 || next >= widgets.length) return doc;
  [widgets[index], widgets[next]] = [widgets[next]!, widgets[index]!];
  return { ...doc, widgets };
}
