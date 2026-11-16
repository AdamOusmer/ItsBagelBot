// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { FlowNode } from './model';

type Point = { x: number; y: number };
type Positions = Record<FlowNode['id'], Point>;

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
