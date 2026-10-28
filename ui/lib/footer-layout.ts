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
