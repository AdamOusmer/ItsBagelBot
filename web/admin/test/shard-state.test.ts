// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import { shardBadge } from '../src/lib/components/shards/shard-state';

describe('shard badge', () => {
  test('an unknown shard is neither healthy nor degraded', () => {
    expect(shardBadge({ shard_id: 0, node: '', bound: false, state: 'unknown' })).toEqual({ label: 'admin.shards.stateUnknown', tone: 'neutral' });
  });
  test('a vacant unresponsive shard stays degraded', () => {
    expect(shardBadge({ shard_id: 0, node: '', bound: false, state: 'unresponsive', managed: true })).toEqual({ label: 'admin.shards.stateDegraded', tone: 'danger' });
  });
});
