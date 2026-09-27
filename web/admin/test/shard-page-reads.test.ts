// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import { shardPageReads } from '../src/lib/server/shard-page-reads';
import { emptyShardSnapshot } from '../src/lib/server/fallback';
import type { TrialSnapshot } from '../src/lib/server/services';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

describe('independent shard page reads', () => {
  test('fleet renders while trial counters are still pending', async () => {
    const fleet = deferred<ReturnType<typeof emptyShardSnapshot>>();
    const trials = deferred<TrialSnapshot | null>();
    const reads = shardPageReads(() => fleet.promise, () => trials.promise);
    let trialReady = false;
    reads.trials.then(() => { trialReady = true; });
    const snapshot = emptyShardSnapshot();
    fleet.resolve(snapshot);
    expect(await reads.fleet).toEqual({ snapshot, degraded: false });
    expect(trialReady).toBe(false);
    trials.resolve({ version: 1, trials: [] });
    expect(await reads.trials).toEqual({ version: 1, trials: [] });
  });

  test('trial failure does not degrade available fleet health', async () => {
    const snapshot = emptyShardSnapshot();
    const reads = shardPageReads(() => Promise.resolve(snapshot), () => Promise.reject(new Error('timeout')));
    expect(await reads.fleet).toEqual({ snapshot, degraded: false });
    expect(await reads.trials).toBeNull();
  });

  test('fleet failure does not hide available trial state', async () => {
    const trials: TrialSnapshot = { version: 1, trials: [] };
    const reads = shardPageReads(() => Promise.reject(new Error('timeout')), () => Promise.resolve(trials));
    expect((await reads.fleet).degraded).toBe(true);
    expect(await reads.trials).toEqual(trials);
  });
});
