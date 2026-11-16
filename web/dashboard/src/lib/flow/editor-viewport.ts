// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Inset } from './touch';

type Rect = Pick<DOMRect, 'top' | 'right' | 'bottom' | 'left' | 'width' | 'height'>;
type Panels = { board: Rect; bar: Rect; rail: Rect };
const GAP = 12;

export function panelInsets({ board, bar, rail }: Panels, panelHeight: number): Inset {
  const railIsRow = rail.width > rail.height;
  const toolbars = railIsRow ? board.bottom - rail.top : 72;
  return {
    top: bar.bottom - board.top + GAP,
    right: 16,
    bottom: (panelHeight || toolbars) + GAP,
    left: railIsRow ? 16 : rail.right - board.left + GAP
  };
}

export function keyboardSize(layoutHeight: number, viewport: Pick<VisualViewport, 'height' | 'offsetTop'> | null): { height: number; keyboard: number } {
  const height = viewport?.height ?? layoutHeight;
  return { height, keyboard: Math.max(0, layoutHeight - height - (viewport?.offsetTop ?? 0)) };
}
