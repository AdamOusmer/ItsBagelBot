// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Shard, TrialSocket } from '@bagel/kit';
import type { TrialChannel } from '#lib/server/services';

interface ShardNode {
  node: string;
  pod: string;
  shards: Shard[];
}

const NODE_ORDER = new Intl.Collator(undefined, { numeric: true });

export function groupShardNodes(nodes: readonly string[], shards: readonly Shard[]): ShardNode[] {
  const byNode = new Map(nodes.map((node): [string, Shard[]] => [node, []]));
  for (const shard of shards) {
    const node = shard.node || '';
    const sockets = byNode.get(node) ?? [];
    sockets.push(shard);
    byNode.set(node, sockets);
  }

  const ordered = [...byNode].sort(([a], [b]) => {
    if (!a) return 1;
    if (!b) return -1;
    return NODE_ORDER.compare(a, b);
  });

  return ordered.map(([node, sockets], index) => ({
    node,
    pod: node ? String(index + 1) : '',
    shards: sockets.sort((a, b) => a.shard_id - b.shard_id)
  }));
}

export type TrialSocketGroup = { slot: number; socket?: TrialSocket; channels: TrialChannel[] };

function trialName(row: TrialChannel): string {
  return row.display_name?.trim() || row.broadcaster_id;
}

export function activeTrial(row: TrialChannel): boolean {
  return row.state !== 'removed' && row.state !== 'promoted';
}

export function groupTrialConnections(sockets: readonly TrialSocket[], trials: readonly TrialChannel[], locale?: string) {
  const order = new Intl.Collator(locale, { numeric: true });
  const groups = new Map<number, TrialSocketGroup>(sockets.map((socket) => [socket.slot, { slot: socket.slot, socket, channels: [] }]));
  const channels = trials.filter(activeTrial).sort((a, b) => order.compare(trialName(a), trialName(b)));

  for (const channel of channels) {
    const slot = channel.slot ?? 0;
    const group = groups.get(slot) ?? { slot, channels: [] };
    group.channels.push(channel);
    groups.set(slot, group);
  }

  return {
    groups: [...groups.values()]
      .filter((group) => group.channels.length > 0 || (group.socket?.channels ?? 0) > 0)
      .sort((a, b) => a.slot - b.slot),
    count: channels.length,
    received: channels.reduce((total, channel) => total + (channel.received ?? 0), 0)
  };
}
