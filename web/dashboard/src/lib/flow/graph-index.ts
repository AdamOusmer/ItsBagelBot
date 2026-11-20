// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Flow, FlowEdge, FlowNode } from './model';

type Index = {
  nodes: Map<string, FlowNode>;
  incoming: Map<string, FlowEdge[]>;
  outgoing: Map<string, FlowEdge[]>;
  ports: Map<string, FlowEdge[]>;
  exclusions: Map<string, Set<string>>;
  links: Set<string>;
};
type Cached = { nodes: FlowNode[]; nodeCount: number; edgeCount: number; index: Index };
const indexes = new WeakMap<FlowEdge[], Cached>();

export const linkKey = (edge: Pick<FlowEdge, 'from' | 'port' | 'to'>): string => JSON.stringify([edge.from, edge.port, edge.to]);
export const portKey = (edge: Pick<FlowEdge, 'from' | 'port'>): string => JSON.stringify([edge.from, edge.port]);

function append(map: Map<string, FlowEdge[]>, key: string, edge: FlowEdge): void {
  const list = map.get(key) ?? [];
  list.push(edge);
  map.set(key, list);
}

export function graphIndex(flow: Flow): Index {
  const cached = indexes.get(flow.edges);
  const current = cached?.nodes === flow.nodes && cached.nodeCount === flow.nodes.length && cached.edgeCount === flow.edges.length;
  if (current) return cached.index;
  const index: Index = { nodes: new Map(flow.nodes.map((node) => [node.id, node])), incoming: new Map(), outgoing: new Map(), ports: new Map(), exclusions: new Map(), links: new Set() };
  for (const edge of flow.edges) {
    append(index.incoming, edge.to, edge);
    append(index.outgoing, edge.from, edge);
    append(index.ports, portKey(edge), edge);
    index.links.add(linkKey(edge));
  }
  indexes.set(flow.edges, { nodes: flow.nodes, nodeCount: flow.nodes.length, edgeCount: flow.edges.length, index });
  return index;
}

export function cycleExclusions(flow: Flow, source: string): Set<string> {
  const index = graphIndex(flow);
  const cached = index.exclusions.get(source);
  if (cached) return cached;
  const seen = new Set([source]);
  const pending = [source];
  for (const id of pending) {
    const parents = (index.incoming.get(id) ?? []).map((edge) => edge.from).filter((parent) => !seen.has(parent));
    parents.forEach((parent) => seen.add(parent));
    pending.push(...parents);
  }
  index.exclusions.set(source, seen);
  return seen;
}

export function descendants(flow: Flow, starts: string[]): Set<string> {
  const index = graphIndex(flow);
  const seen = new Set(starts);
  const pending = [...seen];
  for (const id of pending) {
    const next = (index.outgoing.get(id) ?? []).map((edge) => edge.to).filter((target) => !seen.has(target));
    next.forEach((target) => seen.add(target));
    pending.push(...next);
  }
  return seen;
}
