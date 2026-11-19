// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { decodeStrict, type DecodeError, type Schema } from './decode';
import { Chk, TARGET_DEFAULTS, TARGET_SCHEMA, type TargetParams } from './check';
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

type StepType = TypedNode['type'];
export type StepRole = 'mod' | 'lead_mod' | 'broadcaster';
export type RequirementTrigger = { type: string; kind?: string; perm?: number };
type RequiredTrigger = 'command' | 'reward' | 'unban_request';
export type StepRequirement = { kind: 'trigger'; trigger: RequiredTrigger } | { kind: 'role'; level: StepRole };

const REQUIRED_TRIGGER: Partial<Record<StepType, RequiredTrigger>> = {
  delete_message: 'command',
  redemption: 'reward',
  unban_resolve: 'unban_request',
  arg_regex: 'command',
};

const VIEWERLESS_TRIGGERS = new Set([
  'countdown:', 'timer:', 'event:stream.online', 'event:stream.offline', 'event:channel.ad_break.begin', 'event:channel.update'
]);
const POINTS_ACTIONS: ReadonlySet<StepType> = new Set(['points_add', 'points_set', 'points_spend']);
export const POINTS_ACTION_SCHEMA: Schema = { ...TARGET_SCHEMA, amount: 'int' };
export const POINTS_SPEND_SCHEMA: Schema = { amount: 'int', on_ok: 'actions', on_failure: 'actions' };

const ACTION_ROLE: Partial<Record<StepType, StepRole>> = {
  poll: 'mod',
  poll_end: 'mod',
  prediction: 'mod',
  prediction_end: 'mod',
  raid: 'lead_mod',
  raid_cancel: 'lead_mod',
  mod: 'lead_mod',
  vip: 'lead_mod',
  chat_mode: 'mod',
  clear_chat: 'mod',
  ad_snooze: 'lead_mod',
  whisper: 'mod',
  unban_resolve: 'mod',
  branded_content: 'lead_mod',
  set_title: 'lead_mod',
  set_game: 'lead_mod',
  set_tags: 'lead_mod',
  commercial: 'lead_mod',
  marker: 'lead_mod',
  clip: 'lead_mod',
  timeout: 'mod',
  ban: 'mod',
  unban: 'mod',
  warn: 'mod',
  shield: 'mod',
  delete_message: 'mod',
  cooldown_add: 'mod',
  countdown_start: 'mod',
  countdown_add: 'mod',
  countdown_stop: 'mod',
  cooldown_reset: 'mod',
};

const SELF_TARGET_EXEMPT = new Map<StepType, (params: unknown) => boolean>([
  ['timeout', whoIsViewer],
  ['ban', whoIsViewer],
  ['delete_message', () => true],
]);

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
  if (reg === conditionRegistry && missingTrigger(node.type, { type: c.trigger, kind: c.kind })) c.add('type', CODE.triggerMismatch);
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

function missingTrigger(type: StepType, trigger: RequirementTrigger): RequiredTrigger | undefined {
  if (!Object.hasOwn(REQUIRED_TRIGGER, type)) return undefined;
  const required = REQUIRED_TRIGGER[type];
  if (required === 'unban_request') {
    return trigger.type === 'event' && trigger.kind === 'channel.unban_request.create' ? undefined : required;
  }
  return required === trigger.type ? undefined : required;
}

function targetsViewer({ who, login }: TargetParams): boolean {
  if (who === 'login') return trimSpace(login) === '';
  return who === 'viewer';
}

function viewerlessPoints(a: ActionNode, trigger: RequirementTrigger): boolean {
  if (!POINTS_ACTIONS.has(a.type) || !VIEWERLESS_TRIGGERS.has(`${trigger.type}:${trigger.kind}`)) return false;
  if (a.type === 'points_spend') return !decodeStrict(a.params, POINTS_SPEND_SCHEMA, {}).err;
  const { value, err } = decodeParams<TargetParams>(POINTS_ACTION_SCHEMA, TARGET_DEFAULTS, a.params);
  return !err && targetsViewer(value);
}

function whoIsViewer(params: unknown): boolean {
  return (params as { who?: unknown } | null)?.who === 'viewer';
}

function missingRole(type: StepType, trigger: RequirementTrigger, params: unknown): StepRole | undefined {
  if (trigger.type !== 'command' || !Object.hasOwn(ACTION_ROLE, type)) return undefined;
  if (params !== undefined && SELF_TARGET_EXEMPT.get(type)?.(params)) return undefined;
  const level = ACTION_ROLE[type]!;
  return (trigger.perm ?? -1) < PERM_LEVELS.indexOf(level) ? level : undefined;
}

export function stepRequirement(type: StepType, trigger: RequirementTrigger, params?: unknown): StepRequirement | null {
  const required = missingTrigger(type, trigger);
  if (required) return { kind: 'trigger', trigger: required };
  const level = missingRole(type, trigger, params);
  return level ? { kind: 'role', level } : null;
}

export function permRank(list: readonly { type: StepType; params?: unknown }[]): number {
  let best = -1;
  for (const cond of list) {
    if (cond.type !== 'perm') continue;
    const { value, err } = decodeParams<{ level: string }>({ level: 'string' }, {}, cond.params);
    if (!err) best = Math.max(best, PERM_LEVELS.indexOf(value.level));
  }
  return best;
}

function action(at: Chk, a: ActionNode): void {
  const trigger = { type: at.trigger, kind: at.kind, perm: at.perm };
  if (missingTrigger(a.type, trigger) || viewerlessPoints(a, trigger)) at.add('type', CODE.triggerMismatch);
  if (missingRole(a.type, trigger, a.params ?? null)) at.add('type', CODE.permRequired);
  typed(at, actionRegistry, a);
}

export function nestedActions(a: ActionNode): ActionNode[][] {
  return actionRegistry.get(a.type)?.nested?.(a.params) ?? [];
}
