// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { UiFooterColumn } from './nav-types';

export function stackColumns(columns: readonly UiFooterColumn[]): UiFooterColumn[][] {
  const stacks: UiFooterColumn[][] = [];
  for (const column of columns) {
    const previous = stacks[stacks.length - 1];
    if (column.stacked && previous) previous.push(column);
    else stacks.push([column]);
  }
  return stacks;
}

const RING_OUTER =
  'M400 400 Q 550 200 650 400 Q 750 600 550 700 Q 350 800 200 650 Q 50 500 150 300 Q 250 100 450 150 Q 650 200 700 400 Q 750 600 600 720 Q 450 840 280 760 Q 110 680 80 500 Q 50 320 180 200 Q 310 80 480 120';
const RING_INNER =
  'M400 400 Q 240 220 150 400 Q 60 580 240 680 Q 420 780 560 660 Q 700 540 680 360 Q 660 180 480 140 Q 300 100 180 240 Q 60 380 120 560 Q 180 740 360 780';

export const FOOTER_RING =
  '<svg viewBox="0 0 800 800" fill="none" focusable="false">' +
  `<path d="${RING_OUTER}" stroke="url(#bb-footer-ring-a)" stroke-width="2.5"/>` +
  `<path d="${RING_INNER}" stroke="url(#bb-footer-ring-b)" stroke-width="1.5"/>` +
  '<defs>' +
  '<linearGradient id="bb-footer-ring-a" x1="0" y1="0" x2="800" y2="800" gradientUnits="userSpaceOnUse">' +
  '<stop offset="0" style="stop-color:var(--bb-tan)"/><stop offset="1" style="stop-color:var(--bb-green)"/>' +
  '</linearGradient>' +
  '<linearGradient id="bb-footer-ring-b" x1="800" y1="0" x2="0" y2="800" gradientUnits="userSpaceOnUse">' +
  '<stop offset="0" style="stop-color:var(--bb-green-light)"/><stop offset="1" style="stop-color:var(--bb-tan)"/>' +
  '</linearGradient>' +
  '</defs></svg>';
