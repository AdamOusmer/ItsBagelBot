// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ShardSnapshot } from '@bagel/kit';
import type { TrialSnapshot } from '$lib/server/services';
import { eventsPerSecond, resolveCapacity } from '../../throughput';

export interface ThroughputPoint {
  at: number;
  production: number | null;
  trials: number | null;
}

const HISTORY_MS = 10 * 60_000;
const MAX_SAMPLE_GAP_MS = 30_000;
type TrialBaseline = { at: number; counts: Map<string, number> };

/** Current reporters supply rolling loads for both series. Older trial reporters
 * use cumulative counters; outages, resets and new generations need a fresh baseline. */
export class ThroughputHistory {
  private baseline: TrialBaseline | null = null;
  private points: ThroughputPoint[] = [];

  record(snapshot: ShardSnapshot | null, trials: TrialSnapshot | null, at = Date.now()): ThroughputPoint[] {
    let trialRate: number | null = null;
    if (trials) {
      const active = trials.trials.filter((row) => row.state !== 'removed' && row.state !== 'promoted');
      const counts = new Map<string, number>();
      const activeIds = new Set(active.map((row) => `${row.broadcaster_id}/${row.generation ?? ''}`));
      const complete = active.every((row) => typeof row.received === 'number' && Number.isFinite(row.received) && row.received >= 0);
      if (complete) {
        // Terminal rows retain the last events received before promotion/removal.
        for (const row of trials.trials) {
          if (typeof row.received === 'number' && Number.isFinite(row.received) && row.received >= 0) {
            counts.set(`${row.broadcaster_id}/${row.generation ?? ''}`, row.received);
          }
        }
        const previous = this.baseline;
        if (previous && at > previous.at && at - previous.at <= MAX_SAMPLE_GAP_MS) {
          let delta = 0;
          let continuous = true;
          for (const [id, count] of counts) {
            const before = previous.counts.get(id);
            if (before === undefined) {
              if (activeIds.has(id)) { continuous = false; break; }
              continue;
            }
            if (count < before) { continuous = false; break; }
            delta += count - before;
          }
          if (continuous) trialRate = delta * 1000 / (at - previous.at);
        } else if (active.length === 0) trialRate = 0;
        this.baseline = { at, counts };
      } else this.baseline = null;
    } else this.baseline = null;

    const capacity = snapshot ? resolveCapacity(snapshot) : null;
    // Current ingress reports trial rolling loads, just like production sockets.
    // Counter deltas above are a compatibility fallback for older reporters.
    if (snapshot && capacity && snapshot.trial_sockets !== undefined) {
      trialRate = snapshot.trial_sockets.every((socket) => Number.isFinite(socket.load) && socket.load >= 0)
        ? snapshot.trial_sockets.reduce((sum, socket) => sum + eventsPerSecond(socket.load, capacity.load_window_seconds), 0)
        : null;
    } else if (snapshot && capacity && snapshot.trial_loads !== undefined) {
      const loads = Object.values(snapshot.trial_loads);
      trialRate = loads.every((load) => Number.isFinite(load) && load >= 0)
        ? loads.reduce((sum, load) => sum + eventsPerSecond(load, capacity.load_window_seconds), 0)
        : null;
    }
    const unavailableShard = snapshot?.shards.some((shard) => shard.state === 'unresponsive' && shard.load == null);
    const production = snapshot && capacity && !unavailableShard
      ? snapshot.shards.reduce((sum, shard) => sum + eventsPerSecond(shard.load, capacity.load_window_seconds), 0)
      : null;
    const point = { at, production, trials: trialRate };
    // Bound memory even if visibility events cause unusually frequent reads.
    this.points = [...this.points.filter((row) => row.at > at - HISTORY_MS && row.at < at), point].slice(-600);
    return this.points;
  }
}
