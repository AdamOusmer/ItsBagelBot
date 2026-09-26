// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Shard } from '@bagel/kit';

export interface ShardNode {
  node: string;
  pod: string;
  shards: Shard[];
}

/** Keep empty pods and orphaned sockets visible; never hide a socket because its registry node is missing. */
export function groupShardNodes(nodes: readonly string[], shards: readonly Shard[]): ShardNode[] {
  const names = [...new Set([...nodes, ...shards.map((shard) => shard.node || '')])].sort((a, b) => {
    if (!a) return 1;
    if (!b) return -1;
    return a.localeCompare(b, undefined, { numeric: true });
  });
  return names.map((node, index) => ({
    node,
    pod: node ? String(index + 1) : '',
    shards: shards.filter((shard) => (shard.node || '') === node).sort((a, b) => a.shard_id - b.shard_id)
  }));
}
