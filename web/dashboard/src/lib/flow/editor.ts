// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { outputsFor, specFor, type Flow, type FlowNode } from './model';
import { deleteNode, insertNode, replaceTrigger, type Placement } from './ops';

type NodeId = FlowNode['id'];
type Port = { from: NodeId; port: string };
type AddRequest = { type: FlowNode['type']; at: { x: number; y: number }; placement: Placement };

export function selectedAddPoint(flow: Flow, selectedId: NodeId | null): { x: number; y: number } {
  const node = flow.nodes.find((node) => node.id === selectedId);
  return node ? { x: node.x + 360, y: node.y } : { x: 0, y: 0 };
}

export function connectionTarget(flow: Flow, id: NodeId, source: Port | null | undefined): boolean {
  const node = flow.nodes.find((node) => node.id === id);
  if (!node || !source) return false;
  if (id === source.from) return false;
  if (specFor(node.type)?.kind === 'trigger') return false;
  const ends = { ...source, to: id };
  const keys = ['from', 'port', 'to'] as const;
  return !flow.edges.some((edge) => keys.every((key) => edge[key] === ends[key]));
}

export function selectedPlacement(flow: Flow, selectedId: NodeId | null): Placement {
  const node = flow.nodes.find((node) => node.id === selectedId);
  const spec = node && specFor(node.type);
  if (!node || !spec) return { kind: 'canvas' };
  if (spec.kind === 'trigger') return { kind: 'canvas' };
  return { kind: 'port', from: node.id, port: outputsFor(spec)[0].id, mode: 'insert' };
}

export function addStep(flow: Flow, request: AddRequest): { node: FlowNode; replaced: boolean } {
  const trigger = specFor(request.type)?.kind === 'trigger';
  const replaced = trigger && flow.nodes.some((node) => specFor(node.type)?.kind === 'trigger');
  const node = trigger ? replaceTrigger(flow, request.type) : insertNode(flow, request);
  if (trigger && !replaced) Object.assign(node, request.at);
  return { node, replaced };
}

export function removeStep(flow: Flow, id: NodeId): { parent: NodeId | null; count: number } {
  const parents = flow.edges.filter((edge) => edge.to === id).map((edge) => edge.from);
  const { removed } = deleteNode(flow, id);
  const parent = parents.find((id) => flow.nodes.some((node) => node.id === id)) ?? null;
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
  const ports = flow.edges.filter((edge) => edge.from === id).map((edge) => edge.port);
  return [...new Set(ports)].filter((port) => ports.filter((value) => value === port).length > 1);
}
