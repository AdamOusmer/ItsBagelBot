// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Shard } from '@bagel/kit';
import type { StatusTone } from '@bagel/kit/status-tone';

// derive_state/1 in ingress emits connecting | binding | migrating while a
// socket is coming up or being handed over. All three are transient and heal
// on their own, so none is a fault worth waking an operator for; only backoff
// and unresponsive mean the shard is actually stuck.
const RESTARTING = new Set(['connecting', 'reconnecting', 'binding', 'migrating']);

export type ShardBadge = { label: string; tone: StatusTone };

/**
 * The one verdict a shard row shows.
 *
 * `unregistered` means no process at all answers for the slot, which is a
 * different failure from a session ingress is running but cannot steer
 * (managed === false). Conflating the two is what let a shard serving live
 * events and a zombie shard both read as "degraded".
 *
 * The tone is a StatusTone, not the `green|warn|err` triple the old page used:
 * the dot beside this label is the shared StatusDot, and a second tone
 * vocabulary is how the health panel's green and this page's green drifted.
 */
export function shardBadge(shard: Shard): ShardBadge {
  if (shard.state === 'unregistered') {
    return { label: 'admin.shards.stateMissing', tone: 'error' };
  }
  if (shard.managed === false) return { label: 'admin.shards.stateUnmanaged', tone: 'warning' };
  if (shard.state === 'connected') return { label: 'admin.shards.stateHealthy', tone: 'success' };
  if (RESTARTING.has(shard.state)) {
    return { label: 'admin.shards.stateRestarting', tone: 'warning' };
  }
  return { label: 'admin.shards.stateDegraded', tone: 'error' };
}

/**
 * Utilization -> StatusTone, on the same two thresholds utilizationTone uses
 * (at target = error, within 80% of it = warning). Kept beside shardBadge so a
 * row's dot and its load bar cannot disagree about what "hot" looks like; the
 * numeric thresholds themselves stay in $lib/throughput, which owns them.
 */
export function loadTone(utilization: number, targetUtilization: number): StatusTone {
  if (utilization <= 0) return 'neutral';
  if (utilization >= targetUtilization) return 'error';
  if (utilization >= targetUtilization * 0.8) return 'warning';
  return 'success';
}

/** A rate, at the precision the number deserves: sub-1/s needs two decimals. */
export function rateLabel(eps: number): string {
  if (eps <= 0) return '0';
  if (eps < 1) return eps.toFixed(2);
  return eps.toFixed(eps < 10 ? 1 : 0);
}

/**
 * The `podN` position of a shard's node within the snapshot's node list, or ''
 * when the node is not one the reporter knows. Positional, not a name: the
 * pod names are generated and change every rollout, while "which of the three"
 * is the thing an operator is actually reading for.
 */
export function podIndex(nodes: readonly string[], raw?: string): string {
  if (!raw) return '';
  const index = nodes.findIndex((node) => node === String(raw));
  return index >= 0 ? `pod${index + 1}` : '';
}
