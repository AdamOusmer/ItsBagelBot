// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument, OverlayWidget } from '../overlay-document';
import { clampBox } from '@bagel/ui/lib/box-transform';

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

export function reorderWidget(doc: OverlayDocument, id: string, offset: -1 | 1): OverlayDocument {
  const widgets = doc.widgets.slice();
  const index = widgets.findIndex((item) => item.id === id);
  const next = index + offset;
  if (index < 0 || next < 0 || next >= widgets.length) return doc;
  [widgets[index], widgets[next]] = [widgets[next]!, widgets[index]!];
  return { ...doc, widgets };
}
