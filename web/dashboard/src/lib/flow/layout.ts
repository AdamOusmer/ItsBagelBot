// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { COLUMN_GAP, GRID, NODE_W, ROW_GAP, outputsFor, specFor, type Flow, type NodeSpec } from './model';

export const HEADER_H = 84;
export const ROW_H = 32;
export const FOOT_H = 6;

const snap = (v: number) => Math.round(v / GRID) * GRID;

export function nodeHeight(spec: NodeSpec | undefined): number {
  const ports = spec?.outputs ? spec.outputs.length : 0;
  return HEADER_H + (ports ? ports * ROW_H + FOOT_H : 0);
}

export const inputY = HEADER_H / 2;

export function portY(spec: NodeSpec | undefined, port: string): number {
  const outs = spec ? outputsFor(spec) : [];
  if (!spec?.outputs) return HEADER_H / 2;
  const index = Math.max(0, outs.findIndex((o) => o.id === port));
  return HEADER_H + index * ROW_H + ROW_H / 2;
}

function columns(flow: Flow): Map<string, number> {
  const col = new Map<string, number>();
  const isTrigger = (type: string) => specFor(type)?.kind === 'trigger';
  for (const n of flow.nodes) if (isTrigger(n.type)) col.set(n.id, 0);
  for (let pass = 0; pass < flow.nodes.length; pass++) {
    let changed = false;
    for (const e of flow.edges) {
      const from = col.get(e.from);
      if (from === undefined) continue;
      const target = flow.nodes.find((n) => n.id === e.to);
      if (!target || isTrigger(target.type)) continue;
      if ((col.get(e.to) ?? -1) < from + 1) {
        col.set(e.to, from + 1);
        changed = true;
      }
    }
    if (!changed) break;
  }
  const deepest = Math.max(-1, ...col.values());
  const bound = flow.nodes.length;
  for (const n of flow.nodes) {
    if (!col.has(n.id)) col.set(n.id, deepest + 1);
    else if ((col.get(n.id) as number) > bound) col.set(n.id, bound);
  }
  return col;
}

export function tidy(flow: Flow): Map<string, { x: number; y: number }> {
  const col = columns(flow);
  const byId = new Map(flow.nodes.map((n) => [n.id, n]));
  const heightOf = (id: string) => nodeHeight(specFor(byId.get(id)?.type ?? ''));
  const maxCol = Math.max(0, ...col.values());
  const layers: string[][] = Array.from({ length: maxCol + 1 }, () => []);
  for (const n of flow.nodes) layers[col.get(n.id) as number].push(n.id);

  const centerY = new Map<string, number>();
  const result = new Map<string, { x: number; y: number }>();

  layers.forEach((layer, c) => {
    const key = new Map<string, number>();
    layer.forEach((id, i) => {
      const incoming = flow.edges.filter((e) => e.to === id && centerY.has(e.from));
      if (c === 0 || incoming.length === 0) {
        key.set(id, i);
        return;
      }
      const total = incoming.reduce((sum, e) => {
        const spec = specFor(byId.get(e.from)?.type ?? '');
        const ports = spec ? outputsFor(spec) : [];
        const idx = Math.max(0, ports.findIndex((p) => p.id === e.port));
        return sum + (centerY.get(e.from) as number) + idx * 0.001;
      }, 0);
      key.set(id, total / incoming.length);
    });
    layer.sort((a, b) => (key.get(a) as number) - (key.get(b) as number));

    const total = layer.reduce((sum, id) => sum + heightOf(id), 0) + ROW_GAP * Math.max(0, layer.length - 1);
    const feeds = flow.edges.flatMap((e) => {
      const from = result.get(e.from);
      return from && layer.includes(e.to) ? [from.y + portY(specFor(byId.get(e.from)?.type ?? ''), e.port)] : [];
    });
    const feed = feeds.length ? feeds.reduce((sum, v) => sum + v, 0) / feeds.length : 0;
    let y = layer.length === 1 && feeds.length ? feed - inputY : snap(feed - total / 2);
    for (const id of layer) {
      const h = heightOf(id);
      centerY.set(id, y + h / 2);
      result.set(id, { x: snap(c * (NODE_W + COLUMN_GAP)), y });
      y = Math.ceil((y + h + ROW_GAP) / GRID) * GRID;
    }
  });
  return result;
}
