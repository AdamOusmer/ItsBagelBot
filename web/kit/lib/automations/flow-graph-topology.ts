// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { FLOW_ISSUE, isTriggerType, nodePath, str, type Flow, type FlowEdge, type FlowNode, type FlowResult } from './flow-graph-model';
import { outputPorts } from './flow-graph-writers';
import type { Issue } from './types';

export function outgoing(flow: Flow): Map<string, FlowEdge[]> {
  const outs = new Map<string, FlowEdge[]>();
  for (const edge of flow.edges) {
    const list = outs.get(edge.from) ?? [];
    list.push(edge);
    outs.set(edge.from, list);
  }
  return outs;
}

type CounterGraph = { outs: Map<string, FlowEdge[]>; byId: Map<string, FlowNode> };

export function priorCounters(ctx: CounterGraph): Map<string, string[]> {
  const counts = new Map<string, number>();
  for (const edges of ctx.outs.values()) for (const edge of edges) counts.set(edge.to, (counts.get(edge.to) ?? 0) + 1);
  const pending = [...ctx.byId.keys()].filter((id) => !counts.has(id));
  const prior = new Map<string, string[]>();
  for (const id of pending) {
    const node = ctx.byId.get(id)!;
    const names = prior.get(id) ?? [];
    const after = node.type === 'reply.counter' ? [...new Set([...names, str(node.config.counter)])].slice(0, 2) : names;
    for (const edge of ctx.outs.get(id) ?? []) propagateCounters({ prior, counts, pending }, edge.to, after);
  }
  return prior;
}

function propagateCounters(state: { prior: Map<string, string[]>; counts: Map<string, number>; pending: string[] }, id: string, names: string[]): void {
  state.prior.set(id, [...new Set([...(state.prior.get(id) ?? []), ...names])].slice(0, 2));
  const remaining = state.counts.get(id)! - 1;
  state.counts.set(id, remaining);
  if (remaining === 0) state.pending.push(id);
}

function findCycleNode(outs: Map<string, FlowEdge[]>, start: string): string | null {
  const done = new Set<string>();
  const onPath = new Set<string>();
  const walk = (id: string): string | null => {
    if (onPath.has(id)) return id;
    if (done.has(id)) return null;
    onPath.add(id);
    for (const edge of outs.get(id) ?? []) {
      const hit = walk(edge.to);
      if (hit) return hit;
    }
    onPath.delete(id);
    done.add(id);
    return null;
  };
  return walk(start);
}

export function reachable(outs: Map<string, FlowEdge[]>, id: string): Set<string> {
  const seen = new Set<string>([id]);
  const pending = [id];
  for (const current of pending) {
    const next = (outs.get(current) ?? []).map((edge) => edge.to).filter((target) => !seen.has(target));
    next.forEach((target) => seen.add(target));
    pending.push(...next);
  }
  return seen;
}

export function triggerNode(flow: Flow): FlowResult<FlowNode> {
  const triggers = flow.nodes.filter((n) => isTriggerType(n.type));
  if (triggers.length === 0) return { ok: false, issues: [{ path: 'nodes', code: FLOW_ISSUE.noTrigger }] };
  if (triggers.length > 1) {
    return { ok: false, issues: triggers.map((n) => ({ path: nodePath(n.id), code: FLOW_ISSUE.multipleTriggers })) };
  }
  return { ok: true, value: triggers[0]! };
}

function unknownPortIssues(flow: Flow, byId: CounterGraph['byId']): Issue[] {
  const sources = new Set<string>();
  for (const edge of flow.edges) {
    const source = byId.get(edge.from);
    if (source && !outputPorts(source).includes(edge.port)) sources.add(source.id);
  }
  return [...sources].map((id) => ({ path: nodePath(id), code: FLOW_ISSUE.unknownPort }));
}

export function shapeIssues(flow: Flow, trigger: FlowNode, ctx: CounterGraph): Issue[] {
  const ports = unknownPortIssues(flow, ctx.byId);
  if (ports.length > 0) return ports;
  const cycle = findCycleNode(ctx.outs, trigger.id);
  if (cycle) return [{ path: nodePath(cycle), code: FLOW_ISSUE.cycle }];
  const attached = reachable(ctx.outs, trigger.id);
  const known = new Set(flow.nodes.map((n) => n.id));
  const dangling = flow.edges.filter((e) => !known.has(e.from) || !known.has(e.to));
  return [
    ...flow.nodes.filter((n) => !attached.has(n.id)).map((n) => n.id),
    ...dangling.map((e) => (known.has(e.from) ? e.to : e.from))
  ].map((id) => ({ path: nodePath(id), code: FLOW_ISSUE.unconnectedNode }));
}
