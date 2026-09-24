// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Shard } from '@bagel/kit';
import type { StatusTone } from '@bagel/kit/status-tone';

const RESTARTING = new Set(['connecting', 'reconnecting', 'binding', 'migrating']);

export type ShardBadge = { label: string; tone: StatusTone };

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

export function loadTone(utilization: number, targetUtilization: number): StatusTone {
  if (utilization <= 0) return 'neutral';
  if (utilization >= targetUtilization) return 'error';
  if (utilization >= targetUtilization * 0.8) return 'warning';
  return 'success';
}

export function rateLabel(eps: number): string {
  if (eps <= 0) return '0';
  if (eps < 1) return eps.toFixed(2);
  return eps.toFixed(eps < 10 ? 1 : 0);
}

export function podIndex(nodes: readonly string[], raw?: string): string {
  if (!raw) return '';
  const index = nodes.findIndex((node) => node === String(raw));
  return index >= 0 ? `pod${index + 1}` : '';
}
