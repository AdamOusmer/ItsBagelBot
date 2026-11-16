// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { outputsFor, specFor, type Flow, type FlowNode } from './model';
import { canConnect, deleteNode, insertNode, replaceTrigger, type InsertRequest, type Placement } from './ops';
import { readingOrder } from './keyboard';
import { graphIndex } from './graph-index';

type NodeId = FlowNode['id'];
type Port = { from: NodeId; port: string };

export function selectedAddPoint(flow: Flow, selectedId: NodeId | null): { x: number; y: number } {
  const node = flow.nodes.find((node) => node.id === selectedId);
  return node ? { x: node.x + 360, y: node.y } : { x: 0, y: 0 };
}

export function connectionTarget(flow: Flow, id: NodeId, source: Port | null | undefined): boolean {
  return source ? canConnect(flow, { ...source, to: id }) : false;
}

export function selectedPlacement(flow: Flow, selectedId: NodeId | null): Placement {
  const node = flow.nodes.find((node) => node.id === selectedId);
  const spec = node && specFor(node.type);
  if (!node || !spec) return { kind: 'canvas' };
  return { kind: 'port', from: node.id, port: outputsFor(spec)[0].id, mode: 'insert' };
}

export function addStep(flow: Flow, request: InsertRequest): { node: FlowNode; replaced: boolean } {
  const trigger = specFor(request.type)?.kind === 'trigger';
  const replaced = trigger && flow.nodes.some((node) => specFor(node.type)?.kind === 'trigger');
  const node = trigger ? replaceTrigger(flow, request.type) : insertNode(flow, request);
  if (trigger && !replaced) Object.assign(node, request.at);
  return { node, replaced };
}

export function removeStep(flow: Flow, id: NodeId): { parent: NodeId | null; count: number } {
  const parents = flow.edges.filter((edge) => edge.to === id).map((edge) => edge.from);
  const children = flow.edges.filter((edge) => edge.from === id).map((edge) => edge.to);
  const { removed } = deleteNode(flow, id);
  if (!removed.length) return { parent: null, count: 0 };
  const parent = [...parents, ...children].find((id) => flow.nodes.some((node) => node.id === id)) ?? readingOrder(flow)[0]?.id ?? null;
  return { parent, count: removed.length };
}

export function selectionAfterHistory(flow: Flow, before: ReadonlySet<NodeId>, fallback: NodeId | null): NodeId | null {
  const restored = flow.nodes.filter((node) => !before.has(node.id));
  const restoredIds = new Set(restored.map((node) => node.id));
  const root = restored.find((node) => !flow.edges.some((edge) => edge.to === node.id && restoredIds.has(edge.from)));
  if (root) return root.id;
  return flow.nodes.some((node) => node.id === fallback) ? fallback : null;
}

export function portOccupied(flow: Flow, link: Port | undefined): boolean {
  if (!link) return false;
  return flow.edges.some((edge) => edge.from === link.from && edge.port === link.port);
}

export function parallelPorts(flow: Flow, id: NodeId): string[] {
  const ports = (graphIndex(flow).outgoing.get(id) ?? []).map((edge) => edge.port);
  return [...new Set(ports)].filter((port) => ports.filter((value) => value === port).length > 1);
}
