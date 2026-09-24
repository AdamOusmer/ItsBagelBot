// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CachePolicy } from './cache';

export type { CachePolicy } from './cache';

export const POLICY = {
  live: { freshMs: 1_000, swrMs: 2_000 },
  adminRead: { freshMs: 5_000, swrMs: 30_000 },
  adminPage: { freshMs: 3_000, swrMs: 30_000 },
  entity: { freshMs: 120_000, swrMs: 600_000, staleIfErrorMs: 600_000 },
  // Stale-if-error keeps enforcing the last known ban state during an outage instead of failing open.
  security: { freshMs: 60_000, swrMs: 120_000, staleIfErrorMs: 300_000 },
  projected: { freshMs: 600_000, swrMs: 1_800_000 },
  board: { freshMs: 1_000, swrMs: 2_000, staleIfErrorMs: 300_000 },
  govee: { freshMs: 60_000, swrMs: 600_000, staleIfErrorMs: 600_000 }
} as const satisfies Record<string, CachePolicy>;

export type PolicyName = keyof typeof POLICY;

export interface CacheKey {
  readonly prefix: string;
  readonly policy: CachePolicy;
  for(id: string): string;
}

export function defineKey(prefix: string, policy: CachePolicy): CacheKey {
  return {
    prefix,
    policy,
    for: (id: string) => `${prefix}:${id}`
  };
}
