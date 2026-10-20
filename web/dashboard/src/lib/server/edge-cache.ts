// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type EdgeOutcome = 'page' | 'skip' | 'hidden' | 'missing';

export function markEdge(locals: App.Locals, outcome: EdgeOutcome): void {
  locals.edgeOutcome = outcome;
}

export function pageOutcome(visibility: readonly (boolean | null)[], degraded: boolean): EdgeOutcome {
  return degraded || visibility.includes(null) ? 'skip' : 'page';
}

export function readVisibility(flag: Promise<boolean>): Promise<boolean | null> {
  return flag.catch(() => null);
}
