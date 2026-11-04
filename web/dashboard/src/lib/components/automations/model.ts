// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  ACTION_TYPES,
  CONDITION_TYPES,
  TRIGGER_TYPES,
  VERSION,
  eventKinds,
  type Action,
  type Condition,
  type Definition,
  type FieldDef,
  type Issue,
  type Params,
  type Trigger,
  type TypeDef
} from '@bagel/kit/automations';

const TYPE_OVERRIDES: Record<string, Params> = {
  command: { sources: ['chat'] }
};

const NUMERIC_ENUM = /^\d+$/;

export const MANUAL_RUN_TRIGGERS: readonly string[] = ['manual'];

export function isNumericEnum(field: FieldDef): boolean {
  return field.kind === 'enum' && !!field.values?.length && field.values.every((v) => NUMERIC_ENUM.test(v));
}

function clampToRange(field: FieldDef): number {
  const lo = field.min ?? Number.NEGATIVE_INFINITY;
  const hi = field.max ?? Number.POSITIVE_INFINITY;
  return Math.min(Math.max(0, lo), hi);
}

const NESTED_DEFAULTS: Partial<Record<FieldDef['kind'], () => unknown>> = {
  list: () => [],
  map: () => ({}),
  actions: () => [],
  bool: () => false,
  text: () => '',
  template: () => '',
  login: () => '',
  branches: () => [
    { weight: 1, actions: [] },
    { weight: 1, actions: [] }
  ],
  condition: () => makeCondition(CONDITION_TYPES[0].type)
};

function defaultValue(field: FieldDef): unknown {
  if (field.kind === 'int') return clampToRange(field);
  if (field.kind === 'enum') return isNumericEnum(field) ? Number(field.values![0]) : (field.values?.[0] ?? '');
  return NESTED_DEFAULTS[field.kind]?.();
}

export function defaultParams(def: TypeDef): Params {
  const params: Params = {};
  for (const field of def.fields) params[field.name] = defaultValue(field);
  return { ...params, ...structuredClone(TYPE_OVERRIDES[def.type] ?? {}) };
}

type TypeName = TypeDef['type'];

const find = (defs: TypeDef[], type: TypeName): TypeDef | undefined => defs.find((d) => d.type === type);

export const triggerDef = (type: TypeName) => find(TRIGGER_TYPES, type);
export const conditionDef = (type: TypeName) => find(CONDITION_TYPES, type);
export const actionDef = (type: TypeName) => find(ACTION_TYPES, type);

export function makeTrigger(type: TypeName): Trigger {
  const def = triggerDef(type) ?? TRIGGER_TYPES[0];
  return { type: def.type, params: defaultParams(def) };
}

export function makeCondition(type: TypeName): Condition {
  const def = conditionDef(type) ?? CONDITION_TYPES[0];
  return { type: def.type, params: defaultParams(def) };
}

export function makeAction(type: TypeName): Action {
  const def = actionDef(type) ?? ACTION_TYPES[0];
  return { type: def.type, enabled: true, params: defaultParams(def) };
}

export function blankDefinition(): Definition {
  return { version: VERSION, trigger: makeTrigger('manual'), conditions: [], actions: [] };
}

export function issueExact(index: IssueIndex, path: IssuePath): string | undefined {
  return index.get(path)?.[0];
}

export type IssueIndex = Map<string, string[]>;

export function issueIndex(issues: readonly Issue[]): IssueIndex {
  const index: IssueIndex = new Map();
  for (const { path, code } of issues) index.set(path, [...(index.get(path) ?? []), code]);
  return index;
}

export function issueAt(index: IssueIndex, path: IssuePath): string | undefined {
  const exact = index.get(path)?.[0];
  if (exact) return exact;
  for (const [at, codes] of index) if (at.startsWith(`${path}[`)) return codes[0];
  return undefined;
}

type IssuePath = string;

function isUnder(path: IssuePath, prefix: IssuePath): boolean {
  return path === prefix || path.startsWith(`${prefix}.`) || path.startsWith(`${prefix}[`);
}

export function countUnder(index: IssueIndex, prefix: IssuePath): number {
  let n = 0;
  for (const [path, codes] of index) if (isUnder(path, prefix)) n += codes.length;
  return n;
}

export function eventFieldNames(kind: ReturnType<typeof eventKinds>[number]['kind']): string[] {
  return eventKinds().find((k) => k.kind === kind)?.fields.map((f) => f.name) ?? [];
}

export function movedWithin<T>(list: T[], { from, to }: { from: number; to: number }): T[] {
  const outOfRange = to < 0 || to >= list.length;
  if (outOfRange || from === to) return list;
  const next = [...list];
  [next[from], next[to]] = [next[to], next[from]];
  return next;
}

export function triggerSummary(trigger: Trigger): string {
  const p = trigger.params ?? {};
  if (typeof p.interval_s === 'number') return `${p.interval_s}s`;
  return String(p.name ?? p.kind ?? p.slug ?? p.reward_id ?? '');
}

function paramsOf(node: { params?: Params }): Params {
  node.params ??= {};
  return node.params;
}

function normalizeActions(list: Action[] | undefined): Action[] {
  const actions = list ?? [];
  for (const action of actions) normalizeAction(action);
  return actions;
}

function normalizeAction(action: Action): void {
  const params = paramsOf(action);
  if (action.type === 'if') {
    params.then = normalizeActions(params.then as Action[] | undefined);
    params.else = normalizeActions(params.else as Action[] | undefined);
    params.condition ??= makeCondition(CONDITION_TYPES[0].type);
    paramsOf(params.condition as Condition);
  }
  if (action.type !== 'random_pick') return;
  const branches = (params.branches ?? []) as Array<{ weight: number; actions?: Action[] }>;
  for (const branch of branches) branch.actions = normalizeActions(branch.actions);
  params.branches = branches;
}

export function normalizeDefinition(source: Definition): Definition {
  const def = structuredClone(source);
  paramsOf(def.trigger);
  def.conditions = def.conditions ?? [];
  def.conditions.forEach(paramsOf);
  def.actions = normalizeActions(def.actions);
  return def;
}

type RunTone = 'live' | 'quiet' | 'warning' | 'danger' | 'neutral';

export const OUTCOME_TONE: Record<string, RunTone> = {
  ok: 'live',
  skipped: 'quiet',
  partial: 'warning',
  failed: 'danger',
  limited: 'warning'
};

export const STATUS_TONE: Record<string, RunTone> = {
  ok: 'live',
  skipped: 'quiet',
  unsupported: 'neutral',
  failed: 'danger',
  limited: 'warning'
};
