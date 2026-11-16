// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { COLUMN_GAP, GRID, NODE_W, ROW_GAP, outputsFor, specFor, type Flow, type FlowEdge, type FlowNode, type NodeSpec } from './model';
import { descendants, graphIndex, portKey } from './graph-index';

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

const isTriggerType = (type: string) => specFor(type)?.kind === 'trigger';

const LOOP_TYPE = 'reply.loop';

const portTargets = (flow: Flow, from: string, port: string) => (graphIndex(flow).ports.get(portKey({ from, port })) ?? []).map((edge) => edge.to);
const tailsByGraph = new WeakMap<ReturnType<typeof graphIndex>, { loop: string; tail: string }[]>();

export function loopTails(flow: Flow): { loop: string; tail: string }[] {
  const index = graphIndex(flow);
  const cached = tailsByGraph.get(index);
  if (cached) return cached;
  const pairs = flow.nodes
    .filter((node) => node.type === LOOP_TYPE)
    .flatMap((node) => {
      const after = descendants(flow, portTargets(flow, node.id, 'done'));
      const body = [...descendants(flow, portTargets(flow, node.id, 'body'))];
      const ends = body.filter((id) => !after.has(id) && !graphIndex(flow).outgoing.has(id));
      return ends.map((tail) => ({ loop: node.id, tail }));
    });
  tailsByGraph.set(index, pairs);
  return pairs;
}

function relax(flow: Flow, col: Map<string, number>, edge: FlowEdge): boolean {
  const from = col.get(edge.from);
  if (from === undefined) return false;
  const target = graphIndex(flow).nodes.get(edge.to);
  if (!target || isTriggerType(target.type)) return false;
  if ((col.get(edge.to) ?? -1) >= from + 1) return false;
  col.set(edge.to, from + 1);
  return true;
}

function settleColumns(flow: Flow, col: Map<string, number>): Map<string, number> {
  const deepest = Math.max(-1, ...col.values());
  const bound = flow.nodes.length;
  for (const n of flow.nodes) col.set(n.id, col.has(n.id) ? Math.min(col.get(n.id) as number, bound) : deepest + 1);
  return col;
}

function columns(flow: Flow): Map<string, number> {
  const index = graphIndex(flow);
  const col = new Map<string, number>();
  for (const n of flow.nodes) if (isTriggerType(n.type)) col.set(n.id, 0);
  const remaining = new Map(flow.nodes.map((node) => [node.id, index.incoming.get(node.id)?.length ?? 0]));
  const pending = flow.nodes.filter((node) => remaining.get(node.id) === 0).map((node) => node.id);
  for (const id of pending) {
    for (const edge of index.outgoing.get(id) ?? []) advanceColumn({ flow, col, remaining, pending }, edge);
  }
  return settleColumns(flow, col);
}

function advanceColumn(state: { flow: Flow; col: Map<string, number>; remaining: Map<string, number>; pending: string[] }, edge: FlowEdge): void {
  relax(state.flow, state.col, edge);
  const count = state.remaining.get(edge.to)! - 1;
  state.remaining.set(edge.to, count);
  if (count === 0) state.pending.push(edge.to);
}

type Pos = { x: number; y: number };
type Board = { flow: Flow; byId: Map<string, FlowNode>; centerY: Map<string, number>; result: Map<string, Pos> };

type Layer = { ids: string[]; column: number };

const specOf = (node: FlowNode | undefined) => specFor(node?.type ?? '');
const heightOf = (board: Board, id: string) => nodeHeight(specOf(board.byId.get(id)));

function portIndex(board: Board, edge: FlowEdge): number {
  const spec = specOf(board.byId.get(edge.from));
  const ports = spec ? outputsFor(spec) : [];
  return Math.max(0, ports.findIndex((p) => p.id === edge.port));
}

function layersOf(flow: Flow, col: Map<string, number>): Layer[] {
  const maxCol = Math.max(0, ...col.values());
  const layers: Layer[] = Array.from({ length: maxCol + 1 }, (_, column) => ({ ids: [], column }));
  for (const n of flow.nodes) layers[col.get(n.id) as number].ids.push(n.id);
  return layers;
}

function orderKey(board: Board, layer: Layer, index: number): number {
  const id = layer.ids[index];
  const incoming = (graphIndex(board.flow).incoming.get(id) ?? []).filter((edge) => board.centerY.has(edge.from));
  if (layer.column === 0 || incoming.length === 0) return index;
  const total = incoming.reduce((sum, e) => sum + (board.centerY.get(e.from) as number) + portIndex(board, e) * 0.001, 0);
  return total / incoming.length;
}

function sortLayer(board: Board, layer: Layer): void {
  const key = new Map(layer.ids.map((id, i) => [id, orderKey(board, layer, i)]));
  layer.ids.sort((a, b) => (key.get(a) as number) - (key.get(b) as number));
}

function feedsInto(board: Board, layer: Layer): number[] {
  const incoming = graphIndex(board.flow).incoming;
  return layer.ids.flatMap((id) => incoming.get(id) ?? []).flatMap((e) => {
    const from = board.result.get(e.from);
    return from ? [from.y + portY(specOf(board.byId.get(e.from)), e.port)] : [];
  });
}

function startY(board: Board, layer: Layer): number {
  const { ids } = layer;
  const total = ids.reduce((sum, id) => sum + heightOf(board, id), 0) + ROW_GAP * Math.max(0, ids.length - 1);
  const feeds = feedsInto(board, layer);
  const feed = feeds.length ? feeds.reduce((sum, v) => sum + v, 0) / feeds.length : 0;
  return ids.length === 1 && feeds.length ? feed - inputY : snap(feed - total / 2);
}

function placeLayer(board: Board, layer: Layer): void {
  let y = startY(board, layer);
  for (const id of layer.ids) {
    const h = heightOf(board, id);
    board.centerY.set(id, y + h / 2);
    board.result.set(id, { x: snap(layer.column * (NODE_W + COLUMN_GAP)), y });
    y = Math.ceil((y + h + ROW_GAP) / GRID) * GRID;
  }
}

export function tidy(flow: Flow): Map<string, Pos> {
  const board: Board = { flow, byId: new Map(flow.nodes.map((n) => [n.id, n])), centerY: new Map(), result: new Map() };
  for (const layer of layersOf(flow, columns(flow))) {
    sortLayer(board, layer);
    placeLayer(board, layer);
  }
  return board.result;
}
