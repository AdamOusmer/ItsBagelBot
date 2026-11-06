// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { GRID, outputsFor, specFor, type Flow, type FlowNode } from './model';

export type Dir = 'left' | 'right' | 'up' | 'down';
export type Scope = 'canvas' | 'editor' | 'node';

export type ShortcutId =
  | 'delete'
  | 'escape'
  | 'walk'
  | 'nudge'
  | 'open'
  | 'undo'
  | 'redo'
  | 'duplicate'
  | 'save'
  | 'zoom-in'
  | 'zoom-out'
  | 'fit'
  | 'add'
  | 'add-other';

export type Shortcut = {
  id: ShortcutId;
  keys: readonly string[];
  scope: Scope;
  mod?: boolean;
  shift?: boolean | 'any';
  dir?: Dir;
  typing?: boolean;
  repeat?: boolean;
};

export type KeyLike = { key: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean };
export type Target = { closest(selector: string): unknown; tagName?: string; id?: string } | null;

const ARROWS: readonly [string, Dir][] = [
  ['arrowleft', 'left'],
  ['arrowright', 'right'],
  ['arrowup', 'up'],
  ['arrowdown', 'down']
];

export const SHORTCUTS: readonly Shortcut[] = [
  { id: 'delete', keys: ['delete', 'backspace'], scope: 'canvas' },
  { id: 'escape', keys: ['escape'], scope: 'editor', typing: true },
  ...ARROWS.map(([key, dir]): Shortcut => ({ id: 'walk', keys: [key], dir, scope: 'canvas', repeat: true })),
  ...ARROWS.map(([key, dir]): Shortcut => ({ id: 'nudge', keys: [key], dir, shift: true, scope: 'canvas', repeat: true })),
  { id: 'open', keys: ['enter', ' '], scope: 'node' },
  { id: 'undo', keys: ['z'], mod: true, scope: 'editor', repeat: true },
  { id: 'redo', keys: ['z'], mod: true, shift: true, scope: 'editor', repeat: true },
  { id: 'redo', keys: ['y'], mod: true, scope: 'editor', repeat: true },
  { id: 'duplicate', keys: ['d'], mod: true, scope: 'editor' },
  { id: 'save', keys: ['s'], mod: true, scope: 'editor', typing: true },
  { id: 'zoom-in', keys: ['+', '='], shift: 'any', scope: 'canvas', repeat: true },
  { id: 'zoom-out', keys: ['-'], scope: 'canvas', repeat: true },
  { id: 'fit', keys: ['0'], scope: 'canvas' },
  { id: 'add', keys: ['a'], scope: 'canvas' },
  { id: 'add-other', keys: ['a'], shift: true, scope: 'canvas' }
];

const TYPING =
  'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="combobox"], [role="listbox"], [role="spinbutton"], [role="slider"]';

function matches(s: Shortcut, e: KeyLike): boolean {
  if (e.altKey) return false;
  if ((e.metaKey || e.ctrlKey) !== Boolean(s.mod)) return false;
  if (s.shift !== 'any' && e.shiftKey !== Boolean(s.shift)) return false;
  return s.keys.includes(e.key.toLowerCase());
}

export const resolveShortcut = (e: KeyLike): Shortcut | null => SHORTCUTS.find((s) => matches(s, e)) ?? null;

const isBody = (t: Target) => !t || t.tagName === 'BODY' || t.tagName === 'HTML' || t.id === 'main-content';

export function isTyping(t: Target): boolean {
  return Boolean(t?.closest(TYPING));
}

const within = (t: Target, selector: string) => Boolean(t?.closest(selector));

const SCOPE_HOST = { canvas: '[data-flow-canvas]', editor: '[data-flow-editor]' } as const;

function inScope(s: Shortcut, t: Target): boolean {
  if (s.scope === 'node') return within(t, '[data-node-id]') && !within(t, 'button, a');
  return isBody(t) || within(t, SCOPE_HOST[s.scope]);
}

export function allowed(s: Shortcut, t: Target): boolean {
  if (within(t, '[data-overlay]')) return false;
  if (isTyping(t) && !s.typing) return false;
  return inScope(s, t);
}

const DELTAS: Record<Dir, { dx: number; dy: number }> = {
  left: { dx: -GRID, dy: 0 },
  right: { dx: GRID, dy: 0 },
  up: { dx: 0, dy: -GRID },
  down: { dx: 0, dy: GRID }
};

export const stepDelta = (dir: Dir) => ({ ...DELTAS[dir] });

type Graph = Pick<Flow, 'nodes' | 'edges'>;

const byPosition = (a: FlowNode, b: FlowNode) => a.y - b.y || a.x - b.x;
const find = (g: Graph, id: string) => g.nodes.find((n) => n.id === id);

function portRank(g: Graph, from: string, port: string): number {
  const node = find(g, from);
  const spec = node && specFor(node.type);
  return spec ? Math.max(0, outputsFor(spec).findIndex((o) => o.id === port)) : 0;
}

function unique(nodes: FlowNode[]): FlowNode[] {
  return nodes.filter((n, i) => nodes.indexOf(n) === i);
}

export function childrenOf(g: Graph, id: string): FlowNode[] {
  const ranked = g.edges
    .filter((e) => e.from === id)
    .flatMap((e) => {
      const to = find(g, e.to);
      return to ? [{ to, rank: portRank(g, id, e.port) }] : [];
    })
    .sort((a, b) => a.rank - b.rank || byPosition(a.to, b.to));
  return unique(ranked.map((r) => r.to));
}

export function parentsOf(g: Graph, id: string): FlowNode[] {
  return unique(g.edges.filter((e) => e.to === id).flatMap((e) => find(g, e.from) ?? []).sort(byPosition));
}

export const readingOrder = (g: Graph): FlowNode[] => [...g.nodes].sort((a, b) => a.x - b.x || a.y - b.y);

function siblingsOf(g: Graph, node: FlowNode): FlowNode[] {
  const parents = parentsOf(g, node.id);
  const group = parents.length
    ? unique(parents.flatMap((p) => childrenOf(g, p.id))).sort(byPosition)
    : g.nodes.filter((n) => !g.edges.some((e) => e.to === n.id)).sort(byPosition);
  if (group.length > 1) return group;
  return g.nodes.filter((n) => Math.abs(n.x - node.x) < GRID).sort(byPosition);
}

function siblingAt(g: Graph, node: FlowNode, step: number): string | null {
  const sibs = siblingsOf(g, node);
  const at = sibs.findIndex((n) => n.id === node.id);
  return at < 0 ? null : (sibs[at + step]?.id ?? null);
}

export function neighbor(g: Graph, id: string, dir: Dir): string | null {
  const node = find(g, id);
  if (!node) return null;
  if (dir === 'right') return childrenOf(g, id)[0]?.id ?? null;
  if (dir === 'left') return parentsOf(g, id)[0]?.id ?? null;
  return siblingAt(g, node, dir === 'down' ? 1 : -1);
}

export function neighborAfterRemove(g: Graph, id: string): string | null {
  const near = neighbor(g, id, 'right') ?? neighbor(g, id, 'left') ?? neighbor(g, id, 'down') ?? neighbor(g, id, 'up');
  return near ?? readingOrder(g).find((n) => n.id !== id)?.id ?? null;
}

const FOCUS_KEYS = new Set(['ArrowDown', 'ArrowUp', 'Home', 'End']);
const EDGE_KEYS = new Set(['Home', 'End']);

export function stepFocus(e: KeyboardEvent, items: readonly HTMLElement[]): boolean {
  const target = e.target as HTMLElement;
  if (!FOCUS_KEYS.has(e.key)) return false;
  if (items.length === 0) return false;
  if (EDGE_KEYS.has(e.key) && target.matches('input, textarea')) return false;
  e.preventDefault();
  items[stepIndex(items.indexOf(target), items.length, e.key)]?.focus();
  return true;
}

const INDEX_STEPS = new Map<string, (current: number, count: number) => number>([
  ['Home', () => 0],
  ['End', (_, count) => count - 1],
  ['ArrowDown', (current, count) => (current + 1) % count],
  ['ArrowUp', (current, count) => (current <= 0 ? count - 1 : current - 1)]
]);

export function stepIndex(current: number, count: number, key: string): number {
  if (count <= 0) return -1;
  return INDEX_STEPS.get(key)?.(current, count) ?? current;
}
