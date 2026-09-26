// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import type { Shard } from '@bagel/kit';
import { groupShardNodes } from '../src/lib/components/shards/shard-topology';
const socket = (shard_id: number, node: string): Shard => ({ shard_id, node, bound: true, state: 'connected' });
describe('shard topology', () => {
  test('groups and numerically orders sockets while retaining empty and unreported pods', () => {
    const shards = [socket(10, 'ingress@2'), socket(3, 'ingress@2'), socket(1, 'ingress@1')];
    const nodes = ['ingress@10', 'ingress@2', 'ingress@2'];
    expect(groupShardNodes(nodes, shards).map((group) => [group.node, group.pod, group.shards.map((shard) => shard.shard_id)]))
      .toEqual([['ingress@1', '1', [1]], ['ingress@2', '2', [3, 10]], ['ingress@10', '3', []]]);
    expect(shards.map((shard) => shard.shard_id)).toEqual([10, 3, 1]);
    expect(nodes).toEqual(['ingress@10', 'ingress@2', 'ingress@2']);
  });
  test('keeps sockets without a node visible after assigned pods', () => {
    expect(groupShardNodes(['ingress@1'], [socket(0, '')]).map((group) => [group.node, group.pod, group.shards.length]))
      .toEqual([['ingress@1', '1', 0], ['', '', 1]]);
  });
});
