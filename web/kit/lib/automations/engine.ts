// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { decodeStrict, type DecodeError, type Schema } from './decode';
import { Chk } from './check';
import { CODE, MAX_CONDITIONS, MAX_NESTING } from './limits';
import { PERM_LEVELS } from './values';

export type Widen<V> = V extends string ? string : V extends (infer U)[] ? Widen<U>[] : V;
export type Wide<T> = { [K in keyof T]-?: Widen<T[K]> };

export interface TypedNode {
  type: string;
  params: unknown;
}

export interface ActionNode extends TypedNode {
  enabled: boolean | null;
}

export interface Entry {
  check: (c: Chk, raw: unknown) => void;
  nested?: (raw: unknown) => ActionNode[][];
}

export type Table = Record<string, Entry>;

export const triggerRegistry = new Map<string, Entry>();
export const conditionRegistry = new Map<string, Entry>();
export const actionRegistry = new Map<string, Entry>();

const ROLE_MODERATOR = 3;

const REQUIRED_TRIGGER: Record<string, string> = {
  delete_message: 'command',
  redemption: 'reward',
};

const ACTION_ROLE: Record<string, number> = {
  timeout: ROLE_MODERATOR,
  ban: ROLE_MODERATOR,
  warn: ROLE_MODERATOR,
  shield: ROLE_MODERATOR,
  delete_message: ROLE_MODERATOR,
};

export function paramsCode(err: DecodeError): string {
  return err === 'unknown_field' ? CODE.unknownField : CODE.invalidFormat;
}

export function decodeParams<T>(schema: Schema, defaults: Record<string, unknown>, raw: unknown): { value: T; err?: DecodeError } {
  const { value, err } = decodeStrict(raw, schema, defaults);
  return { value: value as T, err };
}

export function register<T>(schema: Schema, check: (c: Chk, p: T) => void, defaults: Record<string, unknown> = {}): Entry {
  return {
    check: (c, raw) => {
      const { value, err } = decodeParams<T>(schema, defaults, raw);
      if (err) c.add('', paramsCode(err));
      else check(c, value);
    },
  };
}

export function registerNested<T>(
  schema: Schema,
  check: (c: Chk, p: T) => void,
  nested: (p: T) => ActionNode[][],
): Entry {
  return {
    ...register(schema, check),
    nested: (raw) => {
      const { value, err } = decodeParams<T>(schema, {}, raw);
      return err ? [] : nested(value);
    },
  };
}

export function typed(c: Chk, reg: Map<string, Entry>, node: TypedNode): void {
  const entry = reg.get(node.type);
  if (node.type === '') c.add('type', CODE.required);
  else if (!entry) c.add('type', CODE.unknownType);
  else entry.check(c.sub('params'), node.params);
}

export function conditions(c: Chk, list: TypedNode[]): void {
  if (!c.count('conditions', list.length, 0, MAX_CONDITIONS)) return;
  list.forEach((cond, i) => typed(c.sub('conditions').elem(i), conditionRegistry, cond));
}

export function actions(c: Chk, list: ActionNode[]): void {
  list.forEach((a, i) => action(c.elem(i), a));
}

function triggerMismatch(at: Chk, type: string): boolean {
  const want = REQUIRED_TRIGGER[type];
  return want !== undefined && at.trigger !== want;
}

function permMissing(at: Chk, type: string): boolean {
  const role = ACTION_ROLE[type];
  return role !== undefined && at.trigger === 'command' && at.perm < role;
}

export function permRank(list: TypedNode[]): number {
  let best = -1;
  for (const cond of list) {
    if (cond.type !== 'perm') continue;
    const { value, err } = decodeParams<{ level: string }>({ level: 'string' }, {}, cond.params);
    if (!err) best = Math.max(best, PERM_LEVELS.indexOf(value.level));
  }
  return best;
}

function action(at: Chk, a: ActionNode): void {
  if (triggerMismatch(at, a.type)) at.add('type', CODE.triggerMismatch);
  if (permMissing(at, a.type)) at.add('type', CODE.permRequired);
  if (at.depth >= MAX_NESTING && actionRegistry.get(a.type)?.nested) {
    at.add('type', CODE.nesting);
    return;
  }
  typed(at, actionRegistry, a);
}

export function nestedActions(a: ActionNode): ActionNode[][] {
  return actionRegistry.get(a.type)?.nested?.(a.params) ?? [];
}
