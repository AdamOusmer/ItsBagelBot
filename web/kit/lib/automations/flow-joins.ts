// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { FlowEdge } from './flow-graph';

type NodeId = string;
type Boundary = { parent: NodeId; depth: number; merge: boolean };
export type JoinIndex = Map<NodeId, Boundary>;
const EXIT = '';

export function commonBoundary(index: JoinIndex, entries: NodeId[]): NodeId {
  return entries.reduce((shared, id) => intersect(index, shared, id), entries[0] ?? EXIT);
}

function intersect(index: JoinIndex, first: NodeId, second: NodeId): NodeId {
  let a = first;
  let b = second;
  while (a !== b) {
    const left = index.get(a)!;
    const right = index.get(b)!;
    if (left.depth >= right.depth) a = left.parent;
    else b = right.parent;
  }
  return a;
}

export function joinIndex(ids: NodeId[], outgoing: Map<NodeId, FlowEdge[]>): JoinIndex {
  const remaining = new Map(ids.map((id) => [id, outgoing.get(id)?.length ?? 0]));
  const incoming = new Map<NodeId, NodeId[]>();
  for (const [from, edges] of outgoing) for (const edge of edges) append(incoming, edge.to, from);
  const pending = ids.filter((id) => remaining.get(id) === 0);
  const index: JoinIndex = new Map([[EXIT, { parent: EXIT, depth: 0, merge: false }]]);
  for (const id of pending) {
    const successors = (outgoing.get(id) ?? []).map((edge) => edge.to);
    const parent = commonBoundary(index, successors);
    const merge = (incoming.get(id)?.length ?? 0) > 1 || successors.some((next) => index.get(next)!.merge);
    index.set(id, { parent, depth: index.get(parent)!.depth + 1, merge });
    for (const from of incoming.get(id) ?? []) advance(remaining, pending, from);
  }
  return index;
}

function append(incoming: Map<NodeId, NodeId[]>, to: NodeId, from: NodeId): void {
  const list = incoming.get(to) ?? [];
  list.push(from);
  incoming.set(to, list);
}

function advance(remaining: Map<NodeId, number>, pending: NodeId[], id: NodeId): void {
  const count = remaining.get(id)! - 1;
  remaining.set(id, count);
  if (count === 0) pending.push(id);
}
