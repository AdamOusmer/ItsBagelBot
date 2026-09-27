// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ShardSnapshot } from '@bagel/kit';
import type { TrialSnapshot } from './services';
import { emptyShardSnapshot } from './fallback';

/** Stream fleet health independently of trial counters and their RPC timeouts. */
export function shardPageReads(
  readFleet: () => Promise<ShardSnapshot>,
  readTrials: () => Promise<TrialSnapshot | null>
) {
  return {
    fleet: readFleet()
      .then((snapshot) => ({ snapshot, degraded: false }))
      .catch(() => ({ snapshot: emptyShardSnapshot(), degraded: true })),
    trials: readTrials().catch(() => null)
  };
}
