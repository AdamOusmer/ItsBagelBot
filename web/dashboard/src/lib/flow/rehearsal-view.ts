// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Flow } from './model';
import type { RehearsalStep } from './rehearse';

type Row = { step: RehearsalStep; more: number; pass: number; total: number };
type Count = { count: number; last: Row };
const loopPath = (step: RehearsalStep): string => step.iteration.length ? step.path.slice(0, step.path.lastIndexOf('.params.actions')) : '';

function visibleRow(row: Row, counts: Map<string, Count>): boolean {
  if (!row.pass) return true;
  const key = loopPath(row.step);
  const count = counts.get(key) ?? { count: 0, last: row };
  counts.set(key, count);
  count.count++;
  if (count.count > 20) {
    count.last.more++;
    return false;
  }
  count.last = row;
  return true;
}

export function rehearsalTimeline(events: RehearsalStep[]): { atMs: number; rows: Row[] }[] {
  const loops = new Map(events.filter((step) => step.detail?.times).map((step) => [step.path, Number(step.detail?.times)]));
  const counts = new Map<string, Count>();
  const groups = new Map<number, Row[]>();
  for (const step of events) {
    const row = { step, more: 0, pass: step.iteration.at(-1) ?? 0, total: loops.get(loopPath(step)) ?? 0 };
    if (!visibleRow(row, counts)) continue;
    const group = groups.get(step.atMs) ?? [];
    group.push(row);
    groups.set(step.atMs, group);
  }
  return [...groups].map(([atMs, rows]) => ({ atMs, rows }));
}

export function rehearsalTrace(flow: Flow, steps: RehearsalStep[]) {
  const nodes = new Set<string>();
  const ports = new Map<string, Set<string>>();
  for (const step of steps) {
    if (!step.id) continue;
    if (step.kind !== 'skip' && step.status !== 'skipped') nodes.add(step.id);
    const taken = ports.get(step.id) ?? new Set<string>();
    taken.add(step.port ?? '*');
    ports.set(step.id, taken);
  }
  return { nodes, edges: tracedEdges(flow, ports) };
}

function tracedEdges(flow: Flow, ports: Map<string, Set<string>>): Set<string> {
  const edges = flow.edges.filter((edge) => {
    const source = ports.get(edge.from);
    return ports.has(edge.to) && (source?.has('*') || source?.has(edge.port));
  }).map((edge) => edge.id);
  return new Set(edges);
}
