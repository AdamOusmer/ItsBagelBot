// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Chk } from './check';
import { decodeStrict, type Schema } from './decode';
import {
  actionRegistry,
  actions,
  type ActionNode,
  conditions,
  decodeParams,
  nestedActions,
  permRank,
  triggerRegistry,
  type TypedNode,
  typed,
} from './engine';
import { CODE, MAX_ACTIONS, MAX_DEFINITION_BYTES, MAX_DELAY_MS, MAX_NAME_LENGTH, QUEUE_NAME_PATTERN, VERSION } from './limits';
import { fillRegistries } from './registry';
import { byteLength, compareBytes, isNameChar, trimSpace } from './text';
import type { Definition, Issue } from './types';

// Called, not a bare import: the package's sideEffects list lets production bundles drop a bare one, emptying every registry.
fillRegistries();

const ROOT_SCHEMA: Schema = { version: 'int', queue: 'string', trigger: 'typed', conditions: 'typeds', actions: 'actions' };

interface Root {
  version: number;
  queue: string;
  trigger: TypedNode;
  conditions: TypedNode[];
  actions: ActionNode[];
}

function flatten(list: ActionNode[]): ActionNode[] {
  const out: ActionNode[] = [];
  for (const a of list) {
    out.push(a);
    for (const branch of nestedActions(a)) out.push(...flatten(branch));
  }
  return out;
}

function pathDelayMs(list: ActionNode[]): number {
  return list.reduce((sum, a) => {
    if (a.type === 'delay') return sum + decodeParams<{ ms: number }>({ ms: 'int' }, {}, a.params).value.ms;
    return sum + Math.max(0, ...nestedActions(a).map(pathDelayMs));
  }, 0);
}

function checkBudgets(c: Chk, list: ActionNode[]): void {
  const effects = flatten(list).filter((a) => {
    const entry = actionRegistry.get(a.type);
    return !entry?.nested || entry.effect;
  });
  c.count('actions', effects.length, 0, MAX_ACTIONS);
  if (pathDelayMs(list) > MAX_DELAY_MS) c.add('actions', CODE.delayBudget);
}

function finish(issues: Issue[]): Issue[] {
  const sorted = [...issues].sort((a, b) => compareBytes(a.path, b.path) || compareBytes(a.code, b.code));
  return sorted.filter((is, i) => i === 0 || is.path !== sorted[i - 1].path || is.code !== sorted[i - 1].code);
}

function check(root: Root): Issue[] {
  const out: Issue[] = [];
  const c = new Chk(out, '', root.trigger.type, 0, permRank(root.conditions));
  if (root.version !== VERSION) c.add('version', CODE.unsupportedVersion);
  if (root.queue !== '') c.simpleWord('queue', root.queue, QUEUE_NAME_PATTERN);
  typed(c.sub('trigger'), triggerRegistry, root.trigger);
  conditions(c, root.conditions);
  actions(c.sub('actions'), root.actions);
  checkBudgets(c, root.actions);
  checkLeftCommands(c, root);
  return finish(out);
}

export const normalizeCommandName = (name: string): string => name.trim().replace(/^!/, '').toLowerCase();

const COOLDOWN_NAME_SCHEMA: Schema = { left_command: 'string', global_s: 'int', user_s: 'int', left_reply: 'string', ready_reply: 'string' };
const IF_CONDITION_SCHEMA: Schema = { condition: 'typed', then: 'actions', else: 'actions' };

function cooldownLeftCommand(node: TypedNode): string {
  if (node.type !== 'cooldown') return '';
  return decodeParams<{ left_command: string }>(COOLDOWN_NAME_SCHEMA, {}, node.params).value?.left_command ?? '';
}

function ifCondition(action: ActionNode): TypedNode[] {
  if (action.type !== 'if') return [];
  const condition = decodeParams<{ condition: TypedNode }>(IF_CONDITION_SCHEMA, {}, action.params).value?.condition;
  return condition ? [condition] : [];
}

// A cooldown sits in the conditions or in an if's condition; nothing else can hold one.
export function leftCommandNames(definition: { conditions?: unknown; actions?: unknown }): string[] {
  const held = [...((definition.conditions ?? []) as TypedNode[]), ...flatten((definition.actions ?? []) as ActionNode[]).flatMap(ifCondition)];
  return held.map(cooldownLeftCommand).filter((name) => name !== '');
}

function triggerCommandNames(trigger: TypedNode): string[] {
  if (trigger.type !== 'command') return [];
  const params = decodeParams<{ name: string; aliases: string[]; match: string }>({ name: 'string', aliases: 'strings', match: 'string', case_sensitive: 'bool', sources: 'strings' }, {}, trigger.params).value;
  if (!params || params.match === 'regex') return [];
  return [params.name, ...(params.aliases ?? [])].map(normalizeCommandName);
}

function checkLeftCommands(c: Chk, root: Root): void {
  const taken = new Set(triggerCommandNames(root.trigger));
  for (const name of leftCommandNames(root).map(normalizeCommandName)) {
    if (taken.has(name)) c.add('left_command', CODE.commandExists);
    taken.add(name);
  }
}

export function validateDefinition(def: unknown): Issue[] {
  const { value, err } = decodeStrict(def, ROOT_SCHEMA);
  if (err) return [{ path: '', code: err === 'unknown_field' ? CODE.unknownField : CODE.invalidJson }];
  return check(value as unknown as Root);
}

export function parseDefinition(raw: string): { definition?: Definition; issues: Issue[] } {
  if (byteLength(raw) > MAX_DEFINITION_BYTES) return { issues: [{ path: '', code: CODE.tooLong }] };
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { issues: [{ path: '', code: CODE.invalidJson }] };
  }
  const issues = validateDefinition(parsed);
  const decodeFailed = issues.length === 1 && issues[0].path === '' && isDecodeCode(issues[0].code);
  return decodeFailed ? { issues } : { definition: parsed as Definition, issues };
}

function isDecodeCode(code: string): boolean {
  return code === CODE.invalidJson || code === CODE.unknownField;
}

export function validateName(name: string): Issue[] {
  const out: Issue[] = [];
  const c = new Chk(out);
  const s = trimSpace(name);
  if (c.text('name', s, 1, MAX_NAME_LENGTH)) c.chars('name', s, isNameChar);
  return finish(out);
}
