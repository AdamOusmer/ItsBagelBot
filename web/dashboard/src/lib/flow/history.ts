// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Flow, FlowEdge, FlowNode } from './model';

type Pt = { x: number; y: number };
type Graph = Pick<Flow, 'nodes' | 'edges'>;
type Snap = { nodes: FlowNode[]; edges: FlowEdge[] };

export type Move = { id: string; from: Pt; to: Pt };

type Entry =
  | { kind: 'edit'; key?: string; at: number; before: Snap; after: Snap }
  | { kind: 'move'; at: number; moves: Move[] };

export const COALESCE_MS = 1000;
const LIMIT = 100;

const clone = <T>(v: T): T => JSON.parse(JSON.stringify(v));
const snapOf = (g: Graph): Snap => clone({ nodes: g.nodes, edges: g.edges });
const shape = (s: Snap) => JSON.stringify([s.nodes.map((n) => [n.id, n.type, n.config]), s.edges]);
const samePt = (a: Pt, b: Pt) => a.x === b.x && a.y === b.y;

function editKey(a: Snap, b: Snap): string | undefined {
  if (a.nodes.length !== b.nodes.length || JSON.stringify(a.edges) !== JSON.stringify(b.edges)) return undefined;
  let changed = -1;
  for (let i = 0; i < a.nodes.length; i++) {
    const x = a.nodes[i];
    const y = b.nodes[i];
    if (x.id !== y.id || x.type !== y.type) return undefined;
    if (JSON.stringify(x.config) === JSON.stringify(y.config)) continue;
    if (changed >= 0) return undefined;
    changed = i;
  }
  if (changed < 0) return undefined;
  const before = a.nodes[changed].config;
  const after = b.nodes[changed].config;
  const fields = [...new Set([...Object.keys(before), ...Object.keys(after)])].filter((k) => before[k] !== after[k]);
  const id = b.nodes[changed].id;
  return fields.length === 1 ? `cfg:${id}:${fields[0]}` : `cfg:${id}`;
}

export class FlowHistory {
  #undo: Entry[] = [];
  #redo: Entry[] = [];
  #head: Snap;
  #sealed = false;

  constructor(flow: Graph) {
    this.#head = snapOf(flow);
  }

  get canUndo() {
    return this.#undo.length > 0;
  }

  get canRedo() {
    return this.#redo.length > 0;
  }

  sync(flow: Graph, now = Date.now()): boolean {
    const next = snapOf(flow);
    if (shape(next) === shape(this.#head)) {
      this.#head = next;
      return false;
    }
    const key = editKey(this.#head, next);
    const top = this.#undo.at(-1);
    if (key && !this.#sealed && top?.kind === 'edit' && top.key === key && now - top.at < COALESCE_MS) {
      top.after = next;
      top.at = now;
      if (shape(top.before) === shape(next)) this.#undo.pop();
    } else {
      this.#push({ kind: 'edit', key, at: now, before: this.#head, after: next });
    }
    this.#head = next;
    return true;
  }

  moved(moves: Move[], now = Date.now()): void {
    const real = moves.filter((m) => !samePt(m.from, m.to));
    if (real.length === 0) return;
    for (const m of real) {
      const n = this.#head.nodes.find((h) => h.id === m.id);
      if (n) Object.assign(n, m.to);
    }
    const top = this.#undo.at(-1);
    if (!this.#sealed && top?.kind === 'move' && now - top.at < COALESCE_MS && top.moves.length === 1 && real.length === 1 && top.moves[0].id === real[0].id) {
      top.moves[0].to = real[0].to;
      top.at = now;
      if (samePt(top.moves[0].from, top.moves[0].to)) this.#undo.pop();
      return;
    }
    this.#push({ kind: 'move', at: now, moves: real });
  }

  undo(flow: Graph): Entry['kind'] | null {
    const entry = this.#undo.pop();
    if (!entry) return null;
    this.#redo.push(entry);
    this.#apply(flow, entry, 'back');
    return entry.kind;
  }

  redo(flow: Graph): Entry['kind'] | null {
    const entry = this.#redo.pop();
    if (!entry) return null;
    this.#undo.push(entry);
    this.#apply(flow, entry, 'forward');
    return entry.kind;
  }

  #push(entry: Entry) {
    this.#undo.push(entry);
    if (this.#undo.length > LIMIT) this.#undo.shift();
    this.#redo = [];
    this.#sealed = false;
  }

  #apply(flow: Graph, entry: Entry, way: 'back' | 'forward') {
    if (entry.kind === 'edit') {
      const live = new Map(flow.nodes.map((n) => [n.id, n]));
      const target = clone(way === 'back' ? entry.before : entry.after);
      flow.nodes = target.nodes.map((n) => {
        const at = live.get(n.id);
        return at ? { ...n, x: at.x, y: at.y } : n;
      });
      flow.edges = target.edges;
    } else {
      for (const m of entry.moves) {
        const n = flow.nodes.find((c) => c.id === m.id);
        if (n) Object.assign(n, way === 'back' ? m.from : m.to);
      }
    }
    this.#head = snapOf(flow);
    this.#sealed = true;
  }
}
