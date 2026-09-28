// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import { ThroughputHistory } from '../src/lib/components/shards/throughput-history';
import { emptyShardSnapshot } from '../src/lib/server/fallback';
import type { TrialChannel, TrialSnapshot } from '../src/lib/server/services';

const trials = (...rows: TrialChannel[]): TrialSnapshot => ({ version: 1, trials: rows });
const channel = (received: number, generation = '1'): TrialChannel => ({ broadcaster_id: '123', enabled: true, generation, state: 'receiving', received });
const last = <T>(rows: T[]) => rows[rows.length - 1];

describe('throughput history', () => {
  test('production uses rolling load while trials use cumulative deltas', () => {
    const history = new ThroughputHistory();
    const snapshot = { ...emptyShardSnapshot(), shards: [{ shard_id: 0, node: 'ingress@1', bound: true, state: 'connected', load: 600 }] };
    expect(last(history.record(snapshot, trials(channel(100)), 1000))).toEqual({ at: 1000, production: 10, trials: null });
    expect(last(history.record(snapshot, trials(channel(140)), 5000))).toEqual({ at: 5000, production: 10, trials: 10 });
  });
  test('counter reset or a new generation needs a fresh baseline', () => {
    const history = new ThroughputHistory();
    history.record(null, trials(channel(100)), 1000);
    expect(last(history.record(null, trials(channel(5)), 2000)).trials).toBeNull();
    expect(last(history.record(null, trials(channel(105, '2')), 3000)).trials).toBeNull();
    expect(last(history.record(null, trials(channel(115, '2')), 4000)).trials).toBe(10);
  });
  test('retains final trial events during promotion without subtracting vanished counters', () => {
    const history = new ThroughputHistory();
    history.record(null, trials(channel(100)), 1000);
    expect(last(history.record(null, trials({ ...channel(120), state: 'promoted' }), 3000)).trials).toBe(10);
    expect(last(history.record(null, trials(), 5000)).trials).toBe(0);
  });
  test('outages and hidden-tab gaps do not fabricate zero activity', () => {
    const history = new ThroughputHistory();
    history.record(null, trials(channel(100)), 1000);
    expect(last(history.record(null, null, 2000))).toEqual({ at: 2000, production: null, trials: null });
    expect(last(history.record(null, trials(channel(120)), 3000)).trials).toBeNull();
    expect(last(history.record(null, trials(channel(140)), 40_000)).trials).toBeNull();
  });
  test('known empty trial fleet is zero and history stays within ten minutes', () => {
    const history = new ThroughputHistory();
    expect(last(history.record(null, trials(), 1000)).trials).toBe(0);
    expect(history.record(null, trials(), 602_000)).toHaveLength(1);
  });
  test('server trial socket loads share the production rolling window and override counters', () => {
    const history = new ThroughputHistory();
    const snapshot = { ...emptyShardSnapshot(), trial_sockets: [{ slot: 1, node: 'ingress@1', state: 'connected' as const, channels: 1, load: 1200, burst: 200 }] };
    expect(last(history.record(snapshot, trials(channel(100)), 1000)).trials).toBe(20);
    expect(last(history.record({ ...snapshot, trial_sockets: [] }, null, 2000)).trials).toBe(0);
    expect(last(history.record({ ...snapshot, trial_sockets: [{ ...snapshot.trial_sockets[0], load: NaN }] }, trials(channel(120)), 3000)).trials).toBeNull();
  });
  test('server per-channel trial loads support reporters without socket telemetry', () => {
    const history = new ThroughputHistory();
    expect(last(history.record({ ...emptyShardSnapshot(), trial_loads: { one: 600, two: 300 } }, null, 1000)).trials).toBe(15);
  });
  test('unresponsive shard without a load leaves a production gap', () => {
    const history = new ThroughputHistory();
    const snapshot = { ...emptyShardSnapshot(), shards: [{ shard_id: 0, node: 'ingress@1', bound: false, state: 'unresponsive' }] };
    expect(last(history.record(snapshot, trials(), 1000)).production).toBeNull();
  });
  test('unknown inventory leaves a production gap instead of a zero rate', () => {
    const history = new ThroughputHistory();
    const snapshot = { ...emptyShardSnapshot(), inventory: 'unavailable' as const, shards: [{ shard_id: 0, node: '', bound: false, state: 'unknown' }] };
    expect(last(history.record(snapshot, trials(), 1000)).production).toBeNull();
  });
});
