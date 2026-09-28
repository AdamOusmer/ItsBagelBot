// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface AnchorRect {
  left: number;
  top: number;
  bottom: number;
  width: number;
}

export interface Viewport {
  width: number;
  height: number;
}

export interface DropdownPlacement {
  left: number;
  width: number;
  maxHeight: number;
  top?: number;
  bottom?: number;
}

const GAP_PX = 4;
const EDGE_PX = 8;
const MIN_WIDTH_PX = 200;

export function placeDropdown(
  anchor: AnchorRect,
  viewport: Viewport,
  maxHeight: number,
  contentHeight: (width: number) => number,
): DropdownPlacement {
  const width = Math.min(Math.max(anchor.width, MIN_WIDTH_PX), viewport.width - 2 * EDGE_PX);
  const left = Math.max(EDGE_PX, Math.min(anchor.left, viewport.width - EDGE_PX - width));
  const below = viewport.height - anchor.bottom - GAP_PX - EDGE_PX;
  const above = anchor.top - GAP_PX - EDGE_PX;
  const needed = Math.min(contentHeight(width), maxHeight);
  if (below >= needed || below >= above) {
    return { left, width, top: anchor.bottom + GAP_PX, maxHeight: Math.min(maxHeight, below) };
  }
  return { left, width, bottom: viewport.height - anchor.top + GAP_PX, maxHeight: Math.min(maxHeight, above) };
}

export function naturalHeight(panel: HTMLElement, width: number): number {
  const { width: placedWidth, maxHeight: placedMaxHeight } = panel.style;
  panel.style.width = `${width}px`;
  panel.style.maxHeight = 'none';
  const height = panel.offsetHeight;
  panel.style.width = placedWidth;
  panel.style.maxHeight = placedMaxHeight;
  return height;
}
