// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { specFor, type FieldSpec, type Flow, type FlowEdge, type FlowNode } from './model';
import { cycleExclusions, graphIndex, linkKey } from './graph-index';

export type EdgeEnds = Pick<FlowEdge, 'from' | 'port' | 'to'>;

export type Placement =
  | { kind: 'canvas' }
  | { kind: 'port'; from: string; port: string; mode: 'insert' | 'parallel' }
  | { kind: 'edge'; edgeId: string };

export type InsertRequest = { type: string; at: { x: number; y: number }; placement: Placement; chatMessage?: string; plainMessage?: string };
type NodeType = FlowNode['type'];
type NodeId = FlowNode['id'];

const NODE_PREFILLS: Record<NodeType, FlowNode['config']> = {
  'reply.timeout': { seconds: 60 },
  'reply.wait': { seconds: 5 },
  'cond.cooldown': { seconds: 30 },
  'cond.chance': { percent: 50 }
};

function uniqueId(prefix: 'n' | 'e', existing: { id: string }[]): string {
  const tail = globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2);
  const base = `${prefix}${tail}`;
  const ids = new Set(existing.map((item) => item.id));
  let id = base;
  let suffix = 1;
  while (ids.has(id)) id = `${base}-${suffix++}`;
  return id;
}

function fieldDefaults(field: FieldSpec): [string, string | boolean][] {
  if (field.type === 'select') return [[field.key, field.options[0]]];
  if (field.type === 'toggle' && field.defaultOn) return [[field.key, true]];
  return [];
}

function hasViewer(trigger: NodeType | undefined): boolean {
  const spec = specFor(trigger ?? '');
  return spec?.provides?.some((variable) => variable.token === '{user}') ?? false;
}

function triggerDefaults(trigger: NodeType | undefined, request: Pick<InsertRequest, 'chatMessage' | 'plainMessage'>): Record<NodeType, FlowNode['config']> {
  const viewer = hasViewer(trigger);
  const target = trigger === 'twitch.chat.command' ? '{touser}' : viewer ? '{user}' : '';
  const plain = request.plainMessage ?? 'Thanks!';
  const message = viewer ? request.chatMessage ?? '{user}' : plain;
  return {
    'reply.shoutout': { target }, 'reply.timeout': { target },
    'reply.announce': { text: message }, 'reply.reply': { text: plain },
    'reply.chat': { message: viewer ? request.chatMessage ?? '' : '' }
  };
}

function defaultConfig(flow: Flow, request: Pick<InsertRequest, 'type' | 'chatMessage' | 'plainMessage'>): FlowNode['config'] {
  const { type } = request;
  const defaults = specFor(type)?.fields.flatMap(fieldDefaults) ?? [];
  const trigger = flow.nodes.find(isTrigger)?.type;
  return { ...Object.fromEntries(defaults), ...NODE_PREFILLS[type], ...triggerDefaults(trigger, request)[type] };
}

function createNode(flow: Flow, request: Pick<InsertRequest, 'type' | 'at' | 'chatMessage' | 'plainMessage'>): FlowNode {
  const { type, at } = request;
  const node: FlowNode = { id: uniqueId('n', flow.nodes), type, x: at.x, y: at.y, config: defaultConfig(flow, request) };
  flow.nodes.push(node);
  return node;
}

const isTrigger = (node: FlowNode | undefined) => specFor(node?.type ?? '')?.kind === 'trigger';

function reachable(flow: Flow, roots: NodeId[]): Set<NodeId> {
  const { outgoing } = graphIndex(flow);
  const seen = new Set(roots);
  const pending = [...roots];
  for (const id of pending) {
    const next = (outgoing.get(id) ?? []).filter((edge) => !seen.has(edge.to)).map((edge) => edge.to);
    next.forEach((child) => seen.add(child));
    pending.push(...next);
  }
  return seen;
}

export function canConnect(flow: Flow, ends: EdgeEnds): boolean {
  const index = graphIndex(flow);
  if (!index.nodes.has(ends.from)) return false;
  const target = index.nodes.get(ends.to);
  if (!target || isTrigger(target)) return false;
  if (cycleExclusions(flow, ends.from).has(ends.to)) return false;
  return !index.links.has(linkKey(ends));
}

export function connect(flow: Flow, ends: EdgeEnds): FlowEdge | null {
  if (!canConnect(flow, ends)) return null;
  const edge: FlowEdge = { id: uniqueId('e', flow.edges), from: ends.from, port: ends.port, to: ends.to };
  flow.edges.push(edge);
  return edge;
}

const continuationPort = (node: FlowNode): string => node.type === 'reply.loop' ? 'done' : specFor(node.type)?.outputs?.[0]?.id ?? 'next';

function insertAtPort(flow: Flow, node: FlowNode, placement: Extract<Placement, { kind: 'port' }>): void {
  const children = placement.mode === 'insert'
    ? flow.edges.filter((edge) => edge.from === placement.from && edge.port === placement.port)
    : [];
  flow.edges = flow.edges.filter((edge) => !children.includes(edge));
  connect(flow, { from: placement.from, port: placement.port, to: node.id });
  for (const child of children) connect(flow, { from: node.id, port: continuationPort(node), to: child.to });
}

function insertAtEdge(flow: Flow, node: FlowNode, edgeId: FlowEdge['id']): void {
  const edge = flow.edges.find((edge) => edge.id === edgeId);
  if (!edge) return;
  const to = edge.to;
  flow.edges = flow.edges.map((candidate) => candidate === edge ? { ...edge, to: node.id } : candidate);
  connect(flow, { from: node.id, port: continuationPort(node), to });
}

export function insertNode(flow: Flow, request: InsertRequest): FlowNode {
  if (specFor(request.type)?.kind === 'trigger') throw new Error('Use replaceTrigger to add a trigger');
  const node = createNode(flow, request);
  if (request.placement.kind === 'port') insertAtPort(flow, node, request.placement);
  if (request.placement.kind === 'edge') insertAtEdge(flow, node, request.placement.edgeId);
  return node;
}

export function replaceTrigger(flow: Flow, type: NodeType): FlowNode {
  if (specFor(type)?.kind !== 'trigger') throw new Error('Choose a trigger type');
  const node = flow.nodes.find(isTrigger);
  if (!node) return createNode(flow, { type, at: { x: 0, y: 0 } });
  node.type = type;
  node.config = defaultConfig(flow, { type });
  return node;
}

function healingPort(flow: Flow, node: FlowNode): string | null {
  const outputs = specFor(node.type)?.outputs;
  if (!outputs) return 'next';
  const used = new Set(flow.edges.filter((edge) => edge.from === node.id).map((edge) => edge.port));
  const port = node.type === 'reply.loop' && !used.has('body') ? 'done' : outputs[0]?.id;
  return [...used].every((usedPort) => usedPort === port) ? port ?? null : null;
}

function healNode(flow: Flow, node: FlowNode, port: FlowEdge['port']): void {
  const edges = [...flow.edges];
  const children = flow.edges.filter((edge) => edge.from === node.id && edge.port === port);
  removeNode(flow, node.id);
  const replacements = new Map<FlowEdge, FlowEdge[]>();
  for (const parent of edges.filter((edge) => edge.to === node.id)) {
    const links = children.map((child) => connect(flow, { from: parent.from, port: parent.port, to: child.to }));
    replacements.set(parent, links.filter((edge): edge is FlowEdge => edge !== null));
  }
  const surviving = new Set(flow.edges);
  flow.edges = edges.flatMap((edge) => replacements.get(edge) ?? (surviving.has(edge) ? [edge] : []));
}

function pruneNode(flow: Flow, id: NodeId): NodeId[] {
  const roots = flow.nodes.filter(isTrigger).map((node) => node.id);
  const before = reachable(flow, roots);
  removeNode(flow, id);
  const after = reachable(flow, roots);
  const lost = flow.nodes.filter((node) => before.has(node.id) && !after.has(node.id)).map((node) => node.id);
  const removed = [id, ...lost];
  flow.nodes = flow.nodes.filter((node) => !removed.includes(node.id));
  flow.edges = flow.edges.filter((edge) => !removed.includes(edge.from) && !removed.includes(edge.to));
  return removed;
}

export function deleteNode(flow: Flow, id: NodeId): { removed: NodeId[]; healed: boolean } {
  const node = flow.nodes.find((node) => node.id === id);
  if (!node) return { removed: [], healed: false };
  if (isTrigger(node)) {
    removeNode(flow, id);
    return { removed: [id], healed: false };
  }
  const incoming = flow.edges.filter((edge) => edge.to === id).length;
  const port = incoming === 1 ? healingPort(flow, node) : null;
  if (port !== null) {
    healNode(flow, node, port);
    return { removed: [id], healed: true };
  }
  return { removed: pruneNode(flow, id), healed: false };
}

export function removeCheckKeepYes(flow: Flow, id: NodeId): { removed: NodeId[] } {
  const node = flow.nodes.find((node) => node.id === id);
  if (!node) return { removed: [] };
  const noRoots = flow.edges.filter((edge) => edge.from === id && edge.port === 'no').map((edge) => edge.to);
  const noPath = reachable(flow, noRoots);
  const yesRoots = flow.edges.filter((edge) => edge.from === id && edge.port === 'yes').map((edge) => edge.to);
  healNode(flow, node, 'yes');
  const roots = flow.nodes.filter((node) => !noPath.has(node.id)).map((node) => node.id);
  const kept = reachable(flow, [...roots, ...yesRoots]);
  const lost = flow.nodes.filter((node) => noPath.has(node.id) && !kept.has(node.id)).map((node) => node.id);
  flow.nodes = flow.nodes.filter((node) => !lost.includes(node.id));
  flow.edges = flow.edges.filter((edge) => !lost.includes(edge.from) && !lost.includes(edge.to));
  return { removed: [id, ...lost] };
}

function removeNode(flow: Flow, id: NodeId): void {
  flow.nodes = flow.nodes.filter((n) => n.id !== id);
  flow.edges = flow.edges.filter((e) => e.from !== id && e.to !== id);
}

export function removeEdge(flow: Flow, id: FlowEdge['id']): void {
  flow.edges = flow.edges.filter((e) => e.id !== id);
}

export function duplicateNode(flow: Flow, id: NodeId): FlowNode | null {
  const src = flow.nodes.find((n) => n.id === id);
  if (!src) return null;
  const copy: FlowNode = { ...src, id: uniqueId('n', flow.nodes), x: src.x + 24, y: src.y + 24, config: { ...src.config } };
  flow.nodes.push(copy);
  return copy;
}
