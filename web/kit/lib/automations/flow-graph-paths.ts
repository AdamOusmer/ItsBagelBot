// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lowerFlow } from './flow-graph-lower';
import { armsOf, isTriggerType, nodePath, type Flow, type FlowResult } from './flow-graph-model';
import { CODE } from './limits';
import type { Action, Definition, Issue } from './types';

export function compileFlow(flow: Flow): FlowResult<{ definition: Definition; nodeByPath: ReadonlyMap<string, string> }> {
  const { result, owners } = lowerFlow(flow);
  if (!result.ok) return result;
  const nodeByPath = stepPaths(result.value, owners).byPath;
  const trigger = flow.nodes.find((node) => isTriggerType(node.type))!;
  nodeByPath.set('trigger', trigger.id);
  return { ok: true, value: { definition: result.value, nodeByPath } };
}

type StepPaths = { byPath: Map<string, string>; forks: Map<string, string>; lastDelay: string | null };

const NESTED_OWNERS = [
  { param: 'condition', map: 'byPath' },
  { param: 'branches', map: 'forks' }
] as const;

function recordStep(owners: Map<object, string>, action: Action, path: string, into: StepPaths): void {
  const id = owners.get(action);
  if (id) into.byPath.set(path, id);
  if (id && action.type === 'delay') into.lastDelay = id;
  for (const { param, map } of NESTED_OWNERS) {
    const owner = owners.get(action.params?.[param] as object);
    if (owner) into[map].set(`${path}.params.${param}`, owner);
  }
}

function walkSteps(owners: Map<object, string>, list: Action[], base: string, into: StepPaths): void {
  list.forEach((action, i) => {
    const path = `${base}[${i}]`;
    recordStep(owners, action, path, into);
    for (const arm of armsOf(action)) walkSteps(owners, arm.list, `${path}.params.${arm.key}`, into);
  });
}

export type FlowStepRef = { index: number; type: string; node: string; arm: string };

type StepRefPlace = { top: number | null; path: string };

function collectStepRefs(owners: Map<object, string>, list: Action[], place: StepRefPlace, into: FlowStepRef[]): void {
  list.forEach((action, i) => {
    const index = place.top ?? i;
    const node = owners.get(action);
    if (node) into.push({ index, type: action.type, node, arm: place.path });
    for (const arm of armsOf(action)) collectStepRefs(owners, arm.list, { top: index, path: `${place.path}/${i}.${arm.key}` }, into);
  });
}

export function flowStepRefs(flow: Flow): FlowStepRef[] {
  const { result, owners } = lowerFlow(flow);
  if (!result.ok) return [];
  const into: FlowStepRef[] = [];
  collectStepRefs(owners, result.value.actions ?? [], { top: null, path: '' }, into);
  return into;
}

function stepPaths(definition: Definition, owners: Map<object, string>): StepPaths {
  const into: StepPaths = { byPath: new Map(), forks: new Map(), lastDelay: null };
  (definition.conditions ?? []).forEach((condition, i) => {
    const id = owners.get(condition);
    if (id) into.byPath.set(`conditions[${i}]`, id);
  });
  walkSteps(owners, definition.actions ?? [], 'actions', into);
  return into;
}

const within = (path: string, key: string) => path === key || path.startsWith(`${key}.`);

function deepestStep(path: string, found: StepPaths): string {
  return [...found.byPath.keys()].filter((key) => within(path, key)).reduce((best, key) => (key.length > best.length ? key : best), '');
}

function locateIssue(issue: Issue, found: StepPaths): Issue {
  const budget = issue.path === 'actions' && issue.code === CODE.delayBudget;
  if (budget && found.lastDelay) return { path: nodePath(found.lastDelay), code: issue.code };
  const source = issue.code === CODE.tooMany ? found.forks.get(issue.path) : undefined;
  if (source) return { path: nodePath(source), code: issue.code };
  const key = deepestStep(issue.path, found);
  if (!key) return issue;
  return { path: `${nodePath(found.byPath.get(key)!)}${issue.path.slice(key.length)}`, code: issue.code };
}

export function locateIssues(flow: Flow, issues: Issue[]): Issue[] {
  const { result, owners } = lowerFlow(flow);
  if (!result.ok) return issues;
  const found = stepPaths(result.value, owners);
  return issues.map((issue) => locateIssue(issue, found));
}
