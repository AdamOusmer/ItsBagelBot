// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { CommandView } from './types';

/**
 * A command's lifetime use counter as a number, for sorting and usage bars.
 * Coalesces the optional field so the deck's sort order and its bar widths
 * agree on one reading of a missing counter.
 */
export function usesCount(c: Pick<CommandView, 'uses'>): number {
  return typeof c.uses === 'number' && Number.isFinite(c.uses) ? c.uses : 0;
}
