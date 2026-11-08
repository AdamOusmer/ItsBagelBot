// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { specFor, type Flow, type FlowEdge, type FlowNode } from './model';

export type EdgeEnds = Pick<FlowEdge, 'from' | 'port' | 'to'>;

function uniqueId(prefix: string, existing: { id: string }[]): string {
  const tail = globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2);
  const base = `${prefix}${tail}`;
  const ids = new Set(existing.map((item) => item.id));
  let id = base;
  let suffix = 1;
  while (ids.has(id)) id = `${base}-${suffix++}`;
  return id;
}

export function addNode(
  flow: Flow,
  type: string,
  at: { x: number; y: number },
  link?: { from: string; port: string }
): FlowNode {
  const node: FlowNode = { id: uniqueId('n', flow.nodes), type, x: at.x, y: at.y, config: {} };
  flow.nodes.push(node);
  if (link && specFor(type)?.kind !== 'trigger') connect(flow, { ...link, to: node.id });
  return node;
}

const EDGE_ENDS = ['from', 'port', 'to'] as const;

const isTrigger = (node: FlowNode | undefined) => specFor(node?.type ?? '')?.kind === 'trigger';

export function connect(flow: Flow, ends: EdgeEnds): FlowEdge | null {
  if (ends.from === ends.to) return null;
  if (isTrigger(flow.nodes.find((n) => n.id === ends.to))) return null;
  if (flow.edges.some((e) => EDGE_ENDS.every((end) => e[end] === ends[end]))) return null;
  const edge: FlowEdge = { id: uniqueId('e', flow.edges), from: ends.from, port: ends.port, to: ends.to };
  flow.edges.push(edge);
  return edge;
}

export function removeNode(flow: Flow, id: string): void {
  flow.nodes = flow.nodes.filter((n) => n.id !== id);
  flow.edges = flow.edges.filter((e) => e.from !== id && e.to !== id);
}

export function removeEdge(flow: Flow, id: string): void {
  flow.edges = flow.edges.filter((e) => e.id !== id);
}

export function duplicateNode(flow: Flow, id: string): FlowNode | null {
  const src = flow.nodes.find((n) => n.id === id);
  if (!src) return null;
  const copy: FlowNode = { ...src, id: uniqueId('n', flow.nodes), x: src.x + 24, y: src.y + 24, config: { ...src.config } };
  flow.nodes.push(copy);
  return copy;
}
