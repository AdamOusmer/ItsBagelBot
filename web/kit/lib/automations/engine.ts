// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { decodeStrict, type DecodeError, type Schema } from './decode';
import { Chk } from './check';
import { CODE } from './limits';
import { trimSpace } from './text';
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
  effect?: boolean;
}

export type Table = Record<string, Entry>;

export const triggerRegistry = new Map<string, Entry>();
export const conditionRegistry = new Map<string, Entry>();
export const actionRegistry = new Map<string, Entry>();

const ROLE_MODERATOR = 3;
const ROLE_LEAD_MODERATOR = 4;

const REQUIRED_TRIGGER: Record<string, string> = {
  delete_message: 'command',
  redemption: 'reward',
};

const VIEWERLESS_TRIGGERS = new Set(['countdown:', 'timer:', 'event:stream.online', 'event:stream.offline']);
export const POINTS_ACTION_SCHEMA: Schema = { login: 'string', amount: 'int' };

const ACTION_ROLE: Record<string, number> = {
  poll: ROLE_MODERATOR,
  poll_end: ROLE_MODERATOR,
  prediction: ROLE_MODERATOR,
  prediction_end: ROLE_MODERATOR,
  raid: ROLE_LEAD_MODERATOR,
  raid_cancel: ROLE_LEAD_MODERATOR,
  mod: ROLE_LEAD_MODERATOR,
  vip: ROLE_LEAD_MODERATOR,
  chat_mode: ROLE_MODERATOR,
  clear_chat: ROLE_MODERATOR,
  ad_snooze: ROLE_LEAD_MODERATOR,
  whisper: ROLE_MODERATOR,
  unban_resolve: ROLE_MODERATOR,
  branded_content: ROLE_LEAD_MODERATOR,
  set_title: ROLE_LEAD_MODERATOR,
  set_game: ROLE_LEAD_MODERATOR,
  set_tags: ROLE_LEAD_MODERATOR,
  commercial: ROLE_LEAD_MODERATOR,
  marker: ROLE_LEAD_MODERATOR,
  clip: ROLE_LEAD_MODERATOR,
  timeout: ROLE_MODERATOR,
  ban: ROLE_MODERATOR,
  warn: ROLE_MODERATOR,
  shield: ROLE_MODERATOR,
  delete_message: ROLE_MODERATOR,
  cooldown_add: ROLE_MODERATOR,
  countdown_start: ROLE_MODERATOR,
  countdown_add: ROLE_MODERATOR,
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
  list.forEach((cond, i) => typed(c.sub('conditions').elem(i), conditionRegistry, cond));
}

export function actions(c: Chk, list: ActionNode[]): void {
  list.forEach((a, i) => action(c.elem(i), a));
}

function pointsTargetViewer(raw: unknown): boolean {
  const { value, err } = decodeParams<{ login: string }>(POINTS_ACTION_SCHEMA, {}, raw);
  return !err && trimSpace(value.login) === '';
}

function triggerMismatch(at: Chk, a: ActionNode): boolean {
  if (a.type === 'unban_resolve') return at.trigger !== 'event' || at.kind !== 'channel.unban_request.create';
  if (a.type === 'points_add' || a.type === 'points_set') {
    return VIEWERLESS_TRIGGERS.has(`${at.trigger}:${at.kind}`) && pointsTargetViewer(a.params);
  }
  const want = REQUIRED_TRIGGER[a.type];
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
  if (triggerMismatch(at, a)) at.add('type', CODE.triggerMismatch);
  if (permMissing(at, a.type)) at.add('type', CODE.permRequired);
  typed(at, actionRegistry, a);
}

export function nestedActions(a: ActionNode): ActionNode[][] {
  return actionRegistry.get(a.type)?.nested?.(a.params) ?? [];
}
