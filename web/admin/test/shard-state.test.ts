// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import { conduitStatus, shardBadge } from '../src/lib/components/shards/shard-state';

describe('shard badge', () => {
  test('an unknown shard is neither healthy nor degraded', () => {
    expect(shardBadge({ shard_id: 0, node: '', bound: false, state: 'unknown' })).toEqual({ label: 'admin.shards.stateUnknown', tone: 'neutral' });
  });
  test('a vacant unresponsive shard stays degraded', () => {
    expect(shardBadge({ shard_id: 0, node: '', bound: false, state: 'unresponsive', managed: true })).toEqual({ label: 'admin.shards.stateDegraded', tone: 'danger' });
  });
});

describe('conduit status', () => {
  test.each([
    ['running', 'conduitRunning', 'success'],
    ['leader', 'conduitRunning', 'success'],
    ['ready', 'conduitRunning', 'success'],
    ['down', 'conduitDown', 'danger'],
    ['unresponsive', 'conduitUnresponsive', 'warning'],
    [undefined, 'unknownState', 'neutral']
  ] as const)('maps %s to the displayed status', (state, key, tone) => {
    expect(conduitStatus(state)).toEqual({ label: `admin.shards.${key}`, tone });
  });
});
