// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Shard, ShardSnapshot } from '@bagel/kit';
import type { TrialChannel, TrialSnapshot } from '$lib/server/services';
import { eventsPerSecond, resolveCapacity } from '../../throughput';

export interface ThroughputPoint {
  at: number;
  production: number | null;
  trials: number | null;
}

const HISTORY_MS = 10 * 60_000;
const MAX_SAMPLE_GAP_MS = 30_000;
type TrialBaseline = { at: number; counts: Map<string, number> };
type TrialSample = TrialBaseline & { activeIds: Set<string> };

function activeTrial(row: TrialChannel): boolean {
  return row.state !== 'removed' && row.state !== 'promoted';
}

function validLoad(value: number): boolean {
  return Number.isFinite(value) && value >= 0;
}

function hasCounter(row: TrialChannel): row is TrialChannel & { received: number } {
  return typeof row.received === 'number' && validLoad(row.received);
}

function trialId(row: TrialChannel): string {
  return `${row.broadcaster_id}/${row.generation ?? ''}`;
}

function trialSample(snapshot: TrialSnapshot | null, at: number): TrialSample | null {
  if (!snapshot) return null;
  const active = snapshot.trials.filter(activeTrial);
  if (!active.every(hasCounter)) return null;
  // Terminal rows retain the last events received before promotion/removal.
  const counts = new Map(snapshot.trials.filter(hasCounter).map((row) => [trialId(row), row.received]));
  return { at, counts, activeIds: new Set(active.map(trialId)) };
}

function freshBaseline(previous: TrialBaseline | null, at: number): previous is TrialBaseline {
  if (!previous) return false;
  return at > previous.at && at - previous.at <= MAX_SAMPLE_GAP_MS;
}

function channelDelta(count: number, before: number | undefined, active: boolean): number | null {
  if (before === undefined) return active ? null : 0;
  if (count < before) return null;
  return count - before;
}

function trialDelta(previous: TrialBaseline, sample: TrialSample): number | null {
  let total = 0;
  for (const [id, count] of sample.counts) {
    const delta = channelDelta(count, previous.counts.get(id), sample.activeIds.has(id));
    if (delta === null) return null;
    total += delta;
  }
  return total;
}

function counterRate(previous: TrialBaseline | null, sample: TrialSample | null): number | null {
  if (!sample) return null;
  if (!freshBaseline(previous, sample.at)) return sample.activeIds.size === 0 ? 0 : null;
  const delta = trialDelta(previous, sample);
  return delta === null ? null : delta * 1000 / (sample.at - previous.at);
}

function trialLoads(snapshot: ShardSnapshot): number[] | null {
  if (snapshot.trial_sockets !== undefined) return snapshot.trial_sockets.map((socket) => socket.load);
  if (snapshot.trial_loads !== undefined) return Object.values(snapshot.trial_loads);
  return null;
}

function rollingTrialRate(snapshot: ShardSnapshot | null, fallback: number | null): number | null {
  if (!snapshot) return fallback;
  const loads = trialLoads(snapshot);
  if (loads === null) return fallback;
  if (!loads.every(validLoad)) return null;
  const window = resolveCapacity(snapshot).load_window_seconds;
  return loads.reduce((sum, load) => sum + eventsPerSecond(load, window), 0);
}

function unavailableShard(shard: Shard): boolean {
  return shard.state === 'unresponsive' && shard.load == null;
}

function productionRate(snapshot: ShardSnapshot | null): number | null {
  if (!snapshot) return null;
  if (snapshot.shards.some(unavailableShard)) return null;
  const window = resolveCapacity(snapshot).load_window_seconds;
  return snapshot.shards.reduce((sum, shard) => sum + eventsPerSecond(shard.load, window), 0);
}

function appendPoint(points: ThroughputPoint[], point: ThroughputPoint): ThroughputPoint[] {
  // Bound memory even if visibility events cause unusually frequent reads.
  const recent = points.filter((row) => row.at > point.at - HISTORY_MS && row.at < point.at);
  return [...recent, point].slice(-600);
}

/** Current reporters supply rolling loads for both series. Older trial reporters
 * use cumulative counters; outages, resets and new generations need a fresh baseline. */
export class ThroughputHistory {
  private baseline: TrialBaseline | null = null;
  private points: ThroughputPoint[] = [];

  record(snapshot: ShardSnapshot | null, trials: TrialSnapshot | null, at = Date.now()): ThroughputPoint[] {
    const sample = trialSample(trials, at);
    const fallback = counterRate(this.baseline, sample);
    this.baseline = sample;
    const point = { at, production: productionRate(snapshot), trials: rollingTrialRate(snapshot, fallback) };
    this.points = appendPoint(this.points, point);
    return this.points;
  }
}
