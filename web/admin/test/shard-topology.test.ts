// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, test } from 'bun:test';
import type { Shard, TrialSocket } from '@bagel/kit';
import type { TrialChannel } from '../src/lib/server/services';
import { groupShardNodes, groupTrialConnections } from '../src/lib/components/shards/shard-topology';
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

const trialSocket = (slot: number, channels: number): TrialSocket => ({
  slot, channels, load: 0, node: 'ingress@1', state: 'connected'
});
const trialChannel = (id: string, state: TrialChannel['state'], slot?: number): TrialChannel => ({
  broadcaster_id: id, display_name: `Viewer ${id}`, received: Number(id), enabled: true, state, slot
});

describe('trial topology', () => {
  const sockets = [trialSocket(2, 1), trialSocket(0, 0), trialSocket(3, 0)];

  const cases: {
    label: string;
    channels: TrialChannel[];
    expected: { groups: [number, string[]][]; count: number; received: number };
  }[] = [
    {
      label: 'reserves space only for sockets reporting channels during the trial read',
      channels: [],
      expected: { groups: [[2, []]], count: 0, received: 0 }
    },
    {
      label: 'merges active channels by slot and keeps unowned sockets visible',
      channels: [
        trialChannel('20', 'receiving'),
        trialChannel('3', 'pending'),
        trialChannel('9', 'promoted', 3),
        trialChannel('7', 'removed', 3),
        trialChannel('5', 'receiving', 1)
      ],
      expected: { groups: [[0, ['3', '20']], [1, ['5']], [2, []]], count: 3, received: 28 }
    }
  ];

  test.each(cases)('$label', ({ channels, expected }) => {
    const { groups, count, received } = groupTrialConnections(sockets, channels, 'en');
    expect({
      groups: groups.map(({ slot, channels }): [number, string[]] => [slot, channels.map((channel) => channel.broadcaster_id)]),
      count,
      received
    }).toEqual(expected);
  });
});
