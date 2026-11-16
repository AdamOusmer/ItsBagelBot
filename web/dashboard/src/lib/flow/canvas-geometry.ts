// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { NODE_W, isSecondPort, specFor, type Flow, type FlowNode } from './model';
import { inputY, loopTails, nodeHeight, portY } from './layout';
import { previewPath, returnPath, wirePath } from './wire';
import { graphIndex } from './graph-index';
import type { Box } from './touch';

type Positions = Record<FlowNode['id'], { x: number; y: number }>;
const ARROW_GAP = 8;
const RETURN_DROP = 20;
const ADD_ROOM = 48;

export function focusBounds(flow: Flow, node: FlowNode, availableWidth: number): Box {
  const box = { x0: node.x, y0: node.y, x1: node.x + NODE_W + ADD_ROOM, y1: node.y + nodeHeight(specFor(node.type)) };
  const trigger = flow.nodes.find((entry) => specFor(entry.type)?.kind === 'trigger');
  if (!trigger) return box;
  const anchored = { ...box, x0: Math.min(trigger.x, box.x0), x1: Math.max(trigger.x + NODE_W, node.x + NODE_W) };
  return (anchored.x1 - anchored.x0) * 0.7 <= availableWidth ? anchored : box;
}

export function wirePreview(flow: Flow, displayed: Positions, wire: { from: FlowNode['id']; port: string; x: number; y: number } | null, targetId: FlowNode['id'] | null): string {
  if (!wire) return '';
  const node = flow.nodes.find((node) => node.id === wire.from);
  if (!node) return '';
  const a = displayed[node.id] ?? node;
  const target = flow.nodes.find((node) => node.id === targetId);
  const position = target && (displayed[target.id] ?? target);
  const end = position ? { x: position.x - ARROW_GAP, y: position.y + inputY } : { x: wire.x, y: wire.y };
  return previewPath({ x: a.x + NODE_W, y: a.y + portY(specFor(node.type), wire.port) }, end);
}

export function flowBounds(flow: Flow, start: { x0: number; y0: number; x1: number; y1: number }) {
  return flow.nodes.reduce((box, node) => ({
    x0: Math.min(box.x0, node.x),
    y0: Math.min(box.y0, node.y),
    x1: Math.max(box.x1, node.x + NODE_W),
    y1: Math.max(box.y1, node.y + nodeHeight(specFor(node.type)))
  }), start);
}

export function startPaths(flow: Flow, displayed: Positions, start: { x: number; y: number }) {
  return flow.nodes.filter((node) => specFor(node.type)?.kind === 'trigger').map((node) => {
    const b = displayed[node.id] ?? node;
    return { id: node.id, d: wirePath(start, { x: b.x - ARROW_GAP, y: b.y + inputY }) };
  });
}

export function edgePaths(flow: Flow, displayed: Positions) {
  const { nodes } = graphIndex(flow);
  return flow.edges.flatMap((edge) => {
    const from = nodes.get(edge.from);
    const to = nodes.get(edge.to);
    if (!from || !to) return [];
    const spec = specFor(from.type);
    const a = displayed[from.id] ?? from;
    const b = displayed[to.id] ?? to;
    const p1 = { x: a.x + NODE_W, y: a.y + portY(spec, edge.port) };
    const p2 = { x: b.x - ARROW_GAP, y: b.y + inputY };
    return [{ id: edge.id, d: wirePath(p1, p2), tone: isSecondPort(spec, edge.port) ? 'no' : 'yes' }];
  });
}

export function loopPaths(flow: Flow, displayed: Positions) {
  const { nodes } = graphIndex(flow);
  return loopTails(flow).flatMap(({ loop, tail }) => {
    const loopNode = nodes.get(loop);
    const tailNode = nodes.get(tail);
    if (!loopNode || !tailNode) return [];
    const l = displayed[loop] ?? loopNode;
    const a = displayed[tail] ?? tailNode;
    const loopBottom = l.y + nodeHeight(specFor(loopNode.type));
    const bottomY = Math.max(loopBottom, a.y + nodeHeight(specFor(tailNode.type))) + RETURN_DROP;
    return [{ id: `${loop}:${tail}`, d: returnPath({ x: a.x + NODE_W, y: a.y + inputY }, { x: l.x + NODE_W / 2, y: loopBottom }, bottomY) }];
  });
}

export function currentPositions(nodes: readonly FlowNode[], displayed: Positions = {}): Positions {
  return Object.fromEntries(nodes.map((node) => [node.id, displayed[node.id] ?? { x: node.x, y: node.y }]));
}

export function tweenPositions(nodes: readonly FlowNode[], from: Positions, progress: number): Positions {
  const eased = progress === 1 ? 1 : 1 - 2 ** (-10 * progress);
  return Object.fromEntries(nodes.map((node) => {
    const start = from[node.id];
    return [node.id, { x: start.x + (node.x - start.x) * eased, y: start.y + (node.y - start.y) * eased }];
  }));
}
