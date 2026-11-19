// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { addStep } from './editor';
import type { Flow, FlowNode } from './model';

export function addNode(flow: Flow, type: FlowNode['type'], at: { x: number; y: number }, link?: { from: string; port: string }): FlowNode {
  const placement = link ? { kind: 'port' as const, ...link, mode: 'parallel' as const } : { kind: 'canvas' as const };
  return addStep(flow, { type, at, placement }).node;
}
