// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { nodeHeight, tidy } from './layout';
import { GRID, isSecondPort, specFor, type Flow, type FlowKind } from './model';

export const PREVIEW_KINDS: readonly FlowKind[] = ['trigger', 'condition', 'reply'];

export type FlowPreview = {
  /** Step boxes as [x, y, height, kind], in grid cells. */
  n: [number, number, number, number][];
  /** Wires as [from box, to box, 1 when it is the second branch]. */
  e: [number, number, number][];
};

export function flowPreview(flow: Flow): FlowPreview {
  const at = tidy(flow);
  const placed = flow.nodes.flatMap((node) => {
    const spec = specFor(node.type);
    const pos = at.get(node.id);
    return spec && pos ? [{ id: node.id, spec, x: pos.x, y: pos.y }] : [];
  });
  const left = Math.min(...placed.map((p) => p.x));
  const top = Math.min(...placed.map((p) => p.y));
  const index = new Map(placed.map((p, i) => [p.id, i]));
  return {
    n: placed.map((p) => [
      Math.round((p.x - left) / GRID),
      Math.round((p.y - top) / GRID),
      Math.ceil(nodeHeight(p.spec) / GRID),
      PREVIEW_KINDS.indexOf(p.spec.kind)
    ]),
    e: flow.edges.flatMap((edge) => {
      const from = index.get(edge.from);
      const to = index.get(edge.to);
      if (from === undefined || to === undefined) return [];
      return [[from, to, isSecondPort(placed[from]!.spec, edge.port) ? 1 : 0]];
    })
  };
}
