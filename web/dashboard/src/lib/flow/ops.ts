// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { specFor, type Flow, type FlowEdge, type FlowNode } from './model';

const uid = (prefix: string) => `${prefix}${Math.random().toString(36).slice(2, 9)}`;

export function addNode(
  flow: Flow,
  type: string,
  at: { x: number; y: number },
  link?: { from: string; port: string }
): FlowNode {
  const node: FlowNode = { id: uid('n'), type, x: at.x, y: at.y, config: {} };
  flow.nodes.push(node);
  if (link && specFor(type)?.kind !== 'trigger') connect(flow, link.from, link.port, node.id);
  return node;
}

export function connect(flow: Flow, from: string, port: string, to: string): FlowEdge | null {
  if (from === to || specFor(flow.nodes.find((n) => n.id === to)?.type ?? '')?.kind === 'trigger') return null;
  if (flow.edges.some((e) => e.from === from && e.port === port && e.to === to)) return null;
  const edge: FlowEdge = { id: uid('e'), from, port, to };
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
  const copy: FlowNode = { ...src, id: uid('n'), x: src.x + 24, y: src.y + 24, config: { ...src.config } };
  flow.nodes.push(copy);
  return copy;
}
