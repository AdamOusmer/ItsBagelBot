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

type EditEntry = Extract<Entry, { kind: 'edit' }>;
type MoveEntry = Extract<Entry, { kind: 'move' }>;

const sameJson = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);
const sameIdentity = (a: FlowNode, b: FlowNode) => a.id === b.id && a.type === b.type;

function soleConfigChange(a: Snap, b: Snap): number {
  if (a.nodes.length !== b.nodes.length) return -1;
  if (!sameJson(a.edges, b.edges)) return -1;
  if (!a.nodes.every((node, i) => sameIdentity(node, b.nodes[i]))) return -1;
  const changed = a.nodes.flatMap((node, i) => (sameJson(node.config, b.nodes[i].config) ? [] : [i]));
  return changed.length === 1 ? changed[0] : -1;
}

function editKey(a: Snap, b: Snap): string | undefined {
  const at = soleConfigChange(a, b);
  if (at < 0) return undefined;
  const before = a.nodes[at].config;
  const after = b.nodes[at].config;
  const fields = [...new Set([...Object.keys(before), ...Object.keys(after)])].filter((k) => before[k] !== after[k]);
  const id = b.nodes[at].id;
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
    const merge = this.#editToMerge(key, now);
    if (merge) {
      merge.after = next;
      merge.at = now;
      if (shape(merge.before) === shape(next)) this.#undo.pop();
    } else {
      this.#push({ kind: 'edit', key, at: now, before: this.#head, after: next });
    }
    this.#head = next;
    return true;
  }

  moved(moves: Move[], now = Date.now()): void {
    const real = moves.filter((m) => !samePt(m.from, m.to));
    if (real.length === 0) return;
    this.#followHead(real);
    const merge = this.#moveToMerge(real, now);
    if (!merge) {
      this.#push({ kind: 'move', at: now, moves: real });
      return;
    }
    merge.moves[0].to = real[0].to;
    merge.at = now;
    if (samePt(merge.moves[0].from, merge.moves[0].to)) this.#undo.pop();
  }

  #followHead(moves: Move[]) {
    for (const m of moves) {
      const n = this.#head.nodes.find((h) => h.id === m.id);
      if (n) Object.assign(n, m.to);
    }
  }

  #recent(now: number): Entry | undefined {
    const top = this.#undo.at(-1);
    if (this.#sealed || !top) return undefined;
    return now - top.at < COALESCE_MS ? top : undefined;
  }

  #editToMerge(key: string | undefined, now: number): EditEntry | undefined {
    if (!key) return undefined;
    const top = this.#recent(now);
    return top?.kind === 'edit' && top.key === key ? top : undefined;
  }

  #moveToMerge(real: Move[], now: number): MoveEntry | undefined {
    const top = this.#recent(now);
    if (top?.kind !== 'move' || real.length !== 1) return undefined;
    return top.moves.length === 1 && top.moves[0].id === real[0].id ? top : undefined;
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
