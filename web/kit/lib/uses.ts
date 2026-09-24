// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { CommandView } from './types';

export function usesCount(c: Pick<CommandView, 'uses'>): number {
  return typeof c.uses === 'number' && Number.isFinite(c.uses) ? c.uses : 0;
}
