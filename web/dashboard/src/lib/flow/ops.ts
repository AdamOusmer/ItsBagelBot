// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { StepRole } from '@bagel/kit/automations';
import { COLUMN_GAP, NODE_W, ROW_GAP, outputsFor, specFor, targetFieldFor, targetHasViewer, triggerProvidesViewer, type FieldSpec, type Flow, type FlowEdge, type FlowNode } from './model';
import { cycleExclusions, graphIndex, linkKey } from './graph-index';
import { nodeHeight } from './layout';

export type EdgeEnds = Pick<FlowEdge, 'from' | 'port' | 'to'>;

export type Placement =
  | { kind: 'canvas' }
  | { kind: 'port'; from: string; port: string; mode: 'insert' | 'parallel' }
  | { kind: 'edge'; edgeId: string };

export type InsertRequest = { type: string; at: { x: number; y: number }; placement: Placement; chatMessage?: string; plainMessage?: string };
type NodeType = FlowNode['type'];
type NodeId = FlowNode['id'];

function uniqueId(prefix: 'n' | 'e', existing: { id: string }[]): string {
  const tail = globalThis.crypto?.randomUUID?.() ?? Math.random().toString(36).slice(2);
  const base = `${prefix}${tail}`;
  const ids = new Set(existing.map((item) => item.id));
  let id = base;
  let suffix = 1;
  while (ids.has(id)) id = `${base}-${suffix++}`;
  return id;
}

function fieldDefaults(field: FieldSpec): [FieldSpec['key'], FlowNode['config'][string]][] {
  const value = field.initial ?? (field.type === 'select' ? field.options[0] : undefined);
  return value === undefined ? [] : [[field.key, value]];
}

function triggerDefaults(trigger: NodeType | undefined, request: Pick<InsertRequest, 'chatMessage' | 'plainMessage'>): Record<NodeType, FlowNode['config']> {
  const viewer = trigger !== 'twitch.unban_request' && triggerProvidesViewer(trigger);
  const target = trigger === 'twitch.chat.command' ? '{touser}' : viewer ? '{user}' : '';
  const plain = request.plainMessage ?? 'Thanks!';
  const message = viewer ? request.chatMessage : plain;
  return {
    'reply.shoutout': { target },
    'reply.whisper': { target },
    'reply.announce': { text: message ?? '{user}' }, 'reply.reply': { text: plain },
    'reply.chat': { message: trigger ? message ?? '' : '' }
  };
}

function targetDefaults(trigger: NodeType | undefined): FlowNode['config'] {
  if (trigger === 'twitch.chat.command') return { who: 'mention' };
  return { who: targetHasViewer(trigger) ? 'viewer' : 'user' };
}

function defaultConfig(flow: Flow, request: Pick<InsertRequest, 'type' | 'chatMessage' | 'plainMessage'>): FlowNode['config'] {
  const { type } = request;
  const spec = specFor(type);
  const defaults = Object.fromEntries(spec?.fields.flatMap(fieldDefaults) ?? []);
  const trigger = flow.nodes.find(isTrigger)?.type;
  const contextual = targetFieldFor(spec) ? targetDefaults(trigger) : triggerDefaults(trigger, request)[type];
  return { ...defaults, ...contextual };
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
  const source = index.nodes.get(ends.from);
  if (!source || !outputsFor(specFor(source.type), source).some(({ id }) => id === ends.port)) return false;
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

const continuationPort = (node: FlowNode): FlowEdge['port'] | undefined =>
  node.type === 'reply.loop' ? 'done' : outputsFor(specFor(node.type), node)[0]?.id;

export function updateNodeConfig(flow: Flow, node: FlowNode, patch: FlowNode['config']): void {
  const spec = specFor(node.type);
  const previous = outputsFor(spec, node);
  Object.assign(node.config, patch);
  if (!spec?.outputsFor) return;
  const active = new Set(outputsFor(spec, node).map(({ id }) => id));
  const removed = new Set(previous.filter(({ id }) => !active.has(id)).map(({ id }) => id));
  if (!removed.size) return;
  flow.edges = flow.edges.filter((edge) => edge.from !== node.id || !removed.has(edge.port));
}

function insertAtPort(flow: Flow, node: FlowNode, placement: Extract<Placement, { kind: 'port' }>): void {
  const children = placement.mode === 'insert'
    ? flow.edges.filter((edge) => edge.from === placement.from && edge.port === placement.port)
    : [];
  flow.edges = flow.edges.filter((edge) => !children.includes(edge));
  connect(flow, { from: placement.from, port: placement.port, to: node.id });
  const port = continuationPort(node);
  if (port) for (const child of children) connect(flow, { from: node.id, port, to: child.to });
}

function insertAtEdge(flow: Flow, node: FlowNode, edges: readonly FlowEdge[]): void {
  if (edges.length === 0) return;
  const inserted = new Set(edges);
  const targets = new Set(edges.map((edge) => edge.to));
  flow.edges = flow.edges.map((edge) => inserted.has(edge) ? { ...edge, to: node.id } : edge);
  const port = continuationPort(node);
  if (port) for (const to of targets) connect(flow, { from: node.id, port, to });
}

export function insertNode(flow: Flow, request: InsertRequest): FlowNode {
  if (specFor(request.type)?.kind === 'trigger') throw new Error('Use replaceTrigger to add a trigger');
  const node = createNode(flow, request);
  if (request.placement.kind === 'port') insertAtPort(flow, node, request.placement);
  if (request.placement.kind === 'edge') {
    const { edgeId } = request.placement;
    insertAtEdge(flow, node, flow.edges.filter((edge) => edge.id === edgeId));
  }
  return node;
}

const isRoleCheck = (node: FlowNode | undefined, level: StepRole): boolean => node?.type === 'cond.role' && node.config.level === level;

export function hasCheckBefore(flow: Flow, nodeId: NodeId, level: StepRole): boolean {
  const { nodes, incoming } = graphIndex(flow);
  const edges = incoming.get(nodeId) ?? [];
  const fromCheck = (edge: FlowEdge): boolean => edge.port === 'yes' && isRoleCheck(nodes.get(edge.from), level);
  return edges.length > 0 && edges.every(fromCheck);
}

export function insertCheckBefore(flow: Flow, nodeId: NodeId, level: StepRole): void {
  const { nodes, incoming } = graphIndex(flow);
  const target = nodes.get(nodeId);
  const edges = incoming.get(nodeId) ?? [];
  if (!target || edges.length === 0) return;
  const at = { x: target.x - NODE_W - COLUMN_GAP, y: target.y - nodeHeight(specFor('cond.role')) - ROW_GAP };
  const check = createNode(flow, { type: 'cond.role', at });
  check.config.level = level;
  insertAtEdge(flow, check, edges);
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
  const outputs = outputsFor(specFor(node.type), node);
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
    const links = children.map((child) => linkOf(flow, { from: parent.from, port: parent.port, to: child.to }));
    replacements.set(parent, links.filter((edge): edge is FlowEdge => edge !== null));
  }
  const surviving = new Set(flow.edges);
  flow.edges = [...new Set(edges.flatMap((edge) => replacements.get(edge) ?? (surviving.has(edge) ? [edge] : [])))];
}

const sameEnds = (edge: FlowEdge, ends: EdgeEnds) => edge.from === ends.from && edge.port === ends.port && edge.to === ends.to;

function linkOf(flow: Flow, ends: EdgeEnds): FlowEdge | null {
  return flow.edges.find((edge) => sameEnds(edge, ends)) ?? connect(flow, ends);
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
