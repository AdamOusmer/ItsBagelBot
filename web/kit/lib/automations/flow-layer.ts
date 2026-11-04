// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { builderTokensToEvent, eventTokensToBuilder } from './flow-graph';
import { VERSION } from './limits';
import type { Action, Condition, Definition, Params, PermLevel } from './types';

export type AlertKey = 'follow' | 'sub' | 'gift' | 'cheer' | 'raid';

export const ALERT_EVENT_KINDS: Record<AlertKey, string> = {
  follow: 'channel.follow',
  sub: 'channel.subscribe',
  gift: 'channel.subscription.gift',
  cheer: 'channel.cheer',
  raid: 'channel.raid'
};

export const ALERT_KEYS = Object.keys(ALERT_EVENT_KINDS) as AlertKey[];

export interface SimpleCommand {
  name: string;
  aliases: string[];
  response: string;
  perm: PermLevel;
  cooldown: number;
  userCooldown: number;
}

export type FlowLayer =
  | { layer: 'command'; command: SimpleCommand }
  | { layer: 'alert'; alert: AlertKey; message: string }
  | { layer: 'automation' };

const AUTOMATION: FlowLayer = { layer: 'automation' };

function onlyChatLine(actions: Action[] | undefined): string | null {
  if (actions?.length !== 1) return null;
  const [action] = actions;
  if (action!.type !== 'chat' || action!.enabled === false) return null;
  const text = action!.params?.text;
  return typeof text === 'string' ? text : null;
}

type Gates = Pick<SimpleCommand, 'perm' | 'cooldown' | 'userCooldown'>;

function clashes(wanted: number, held: number): boolean {
  return wanted > 0 && held > 0;
}

function readCooldown(gates: Gates, params: Params): boolean {
  const global = Number(params.global_s ?? 0);
  const user = Number(params.user_s ?? 0);
  if (clashes(global, gates.cooldown) || clashes(user, gates.userCooldown)) return false;
  gates.cooldown ||= global;
  gates.userCooldown ||= user;
  return true;
}

function readGates(conditions: Condition[]): Gates | null {
  const gates: Gates = { perm: 'everyone', cooldown: 0, userCooldown: 0 };
  let seenPerm = false;
  for (const condition of conditions) {
    const params = condition.params ?? {};
    if (condition.type === 'perm' && !seenPerm) {
      seenPerm = true;
      gates.perm = params.level as PermLevel;
    } else if (condition.type !== 'cooldown' || !readCooldown(gates, params)) {
      return null;
    }
  }
  return gates;
}

function plainChatCommand(params: Params): boolean {
  const name = params.name;
  const sources = (params.sources ?? ['chat']) as string[];
  const routedName = typeof name === 'string' && /^![^\s]+$/.test(name);
  const chatOnly = sources.length === 1 && sources[0] === 'chat';
  return routedName && chatOnly && (params.match ?? 'start') === 'start' && params.case_sensitive !== true;
}

function classifyCommand(definition: Definition): FlowLayer {
  const params = definition.trigger.params ?? {};
  const response = onlyChatLine(definition.actions);
  const gates = readGates(definition.conditions ?? []);
  if (response === null || !gates) return AUTOMATION;
  if (!plainChatCommand(params)) return AUTOMATION;
  const bare = (s: string) => s.replace(/^!/, '').toLowerCase();
  const aliases = ((params.aliases ?? []) as string[]).map(bare);
  return { layer: 'command', command: { name: bare(params.name as string), aliases, response, ...gates } };
}

const ALERT_TOKENS: Record<AlertKey, readonly string[]> = {
  follow: ['user'],
  sub: ['user', 'tier'],
  gift: ['user', 'count', 'tier'],
  cheer: ['user', 'bits'],
  raid: ['user', 'viewers']
};

const PALETTE_PURE_TOKENS = ['random', 'choice'];

// An alert line only expands its own tokens, so any other token would reach chat as literal text.
function alertCanRender(alert: AlertKey, message: string): boolean {
  const heads = [...message.matchAll(/\{([^{}:|]+)/g)].map((match) => match[1]!);
  return heads.every((head) => ALERT_TOKENS[alert].includes(head) || PALETTE_PURE_TOKENS.includes(head));
}

function alertLine(definition: Definition): { alert: AlertKey; line: string } | null {
  const params = definition.trigger.params ?? {};
  const alert = ALERT_KEYS.find((key) => ALERT_EVENT_KINDS[key] === params.kind);
  const filtered = Object.keys((params.filters ?? {}) as object).length > 0;
  const line = onlyChatLine(definition.actions);
  if (!alert || filtered) return null;
  if (line === null || (definition.conditions ?? []).length > 0) return null;
  return { alert, line };
}

function classifyEvent(definition: Definition): FlowLayer {
  const found = alertLine(definition);
  if (!found) return AUTOMATION;
  const { alert, line } = found;
  const message = eventTokensToBuilder(ALERT_EVENT_KINDS[alert], plainAlertLine(line));
  return alertCanRender(alert, message) ? { layer: 'alert', alert, message } : AUTOMATION;
}

export function classifyFlow(definition: Definition): FlowLayer {
  if (definition.trigger.type === 'command') return classifyCommand(definition);
  if (definition.trigger.type === 'event') return classifyEvent(definition);
  return AUTOMATION;
}

const PREVIEW_GRID = 24;
const PREVIEW_COLUMN = 368;
const PREVIEW_BOX_CELLS = 4;
const PREVIEW_GATE_CELLS = 7;
const PREVIEW_GATE_DROP = 58;

// Commands and alerts are always a straight line, so this lays out the box shape the builder's tidy() gives one.
export function simpleFlowPreview(definition: Definition): string {
  const gates = definition.conditions?.length ?? 0;
  const kinds = [0, ...Array<number>(gates).fill(1), 2];
  const column = (i: number) => Math.round((i * PREVIEW_COLUMN) / PREVIEW_GRID);
  const row = (i: number) => Math.round((Math.max(0, i - 1) * PREVIEW_GATE_DROP) / PREVIEW_GRID);
  return JSON.stringify({
    n: kinds.map((kind, i) => [column(i), row(i), kind === 1 ? PREVIEW_GATE_CELLS : PREVIEW_BOX_CELLS, kind]),
    e: kinds.slice(1).map((_, i) => [i, i + 1, 0])
  });
}

function commandConditions(command: SimpleCommand): Condition[] {
  const conditions: Condition[] = [];
  if (command.perm !== 'everyone') conditions.push({ type: 'perm', params: { level: command.perm } });
  if (command.cooldown > 0) conditions.push({ type: 'cooldown', params: { global_s: command.cooldown } });
  if (command.userCooldown > 0) conditions.push({ type: 'cooldown', params: { user_s: command.userCooldown } });
  return conditions;
}

export function commandDefinition(command: SimpleCommand): Definition {
  const conditions = commandConditions(command);
  const params: Params = { name: `!${command.name}` };
  if (command.aliases.length > 0) params.aliases = command.aliases.map((alias) => `!${alias}`);
  const definition: Definition = {
    version: VERSION,
    trigger: { type: 'command', params },
    actions: [{ type: 'chat', params: { text: command.response } }]
  };
  return conditions.length > 0 ? { ...definition, conditions } : definition;
}

// Alert lines render {alerts:bits} and {bits} alike; default lines ship in the namespaced form.
export function plainAlertLine(message: string): string {
  return message.replace(/\{alerts:/g, '{');
}

export function alertDefinition(alert: AlertKey, message: string): Definition {
  return {
    version: VERSION,
    trigger: { type: 'event', params: { kind: ALERT_EVENT_KINDS[alert] } },
    actions: [{ type: 'chat', params: { text: builderTokensToEvent(ALERT_EVENT_KINDS[alert], plainAlertLine(message)) } }]
  };
}
