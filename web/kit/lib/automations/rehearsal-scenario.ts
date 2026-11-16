// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lookupEventKind } from './event-kinds';
import type { Definition, Params } from './types';
import type { RehearsalScenario, ScenarioField, TriggerMatch } from './rehearsal-types';

const VIEWER: readonly ScenarioField[] = [
  { key: 'user', kind: 'text', default: 'PrincessBarney' },
  { key: 'userId', kind: 'text', default: '40000001', more: true },
  { key: 'role', kind: 'role', default: 'viewer', options: ['viewer', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'], more: true }
];
const LIVE: ScenarioField = { key: 'live', kind: 'toggle', default: true, more: true };
const TIER: ScenarioField = { key: 'tier', kind: 'select', default: '1000', options: ['1000', '2000', '3000'] };

export const SCENARIO_FIELDS: Readonly<Record<string, readonly ScenarioField[]>> = {
  'twitch.chat.command': [...VIEWER, { key: 'message', kind: 'text', default: '' }, LIVE],
  'twitch.chat.word': [...VIEWER, { key: 'message', kind: 'text', default: '' }, LIVE],
  'twitch.follow': [...VIEWER, LIVE],
  'twitch.sub.new': [...VIEWER, TIER, { key: 'is_gift', kind: 'toggle', default: false, more: true }, LIVE],
  'twitch.sub.resub': [...VIEWER, TIER, { key: 'months', kind: 'number', default: 14 }, LIVE],
  'twitch.sub.gift': [...VIEWER, TIER, { key: 'giftCount', kind: 'number', default: 5 }, { key: 'anonymous', kind: 'toggle', default: false }, LIVE],
  'twitch.cheer': [...VIEWER, { key: 'bits', kind: 'number', default: 500 }, { key: 'anonymous', kind: 'toggle', default: false, more: true }, LIVE],
  'twitch.raid': [...VIEWER, { key: 'viewers', kind: 'number', default: 42 }, LIVE],
  'twitch.reward': [...VIEWER, { key: 'input', kind: 'text', default: 'lofi beats' }, LIVE],
  'twitch.unban_request': [...VIEWER, { key: 'requestText', kind: 'text', default: 'please unban me' }, LIVE],
  'twitch.stream.online': [LIVE],
  'countdown.finished': [LIVE]
};

const EVENT_SCENARIOS: Readonly<Record<string, string>> = {
  'channel.follow': 'twitch.follow',
  'channel.subscribe': 'twitch.sub.new',
  'channel.subscription.message': 'twitch.sub.resub',
  'channel.subscription.gift': 'twitch.sub.gift',
  'channel.cheer': 'twitch.cheer',
  'channel.raid': 'twitch.raid',
  'channel.unban_request.create': 'twitch.unban_request',
  'stream.online': 'twitch.stream.online'
};

export function scenarioKind(definition: Definition): string {
  const { type, params = {} } = definition.trigger;
  if (type === 'command') return params.match === 'regex' ? 'twitch.chat.word' : 'twitch.chat.command';
  const direct: Record<string, string> = { reward: 'twitch.reward', countdown: 'countdown.finished' };
  return direct[type] ?? EVENT_SCENARIOS[String(params.kind)] ?? '';
}

function commandWord(params: Params): string {
  const body = String(params.name ?? '').replace(/^\(\?:\^\|\[\^\\p\{L\}\\p\{N\}_\]\)/, '').replace(/\(\?:\$\|\[\^\\p\{L\}\\p\{N\}_\]\)$/, '');
  return body.replace(/\\x5c|\\(.)/g, (_, char: string | undefined) => char ?? '\\');
}

export function defaultScenario(definition: Definition): RehearsalScenario {
  const kind = scenarioKind(definition);
  const fields = SCENARIO_FIELDS[kind] ?? [...VIEWER, LIVE];
  const scenario: RehearsalScenario = { live: true, ...Object.fromEntries(fields.map((field) => [field.key, field.default])) };
  const params = definition.trigger.params ?? {};
  Object.assign(scenario, DYNAMIC_DEFAULTS[kind]?.(params));
  scenario.args = scenarioArgs(definition, scenario);
  return scenario;
}

function thresholdDefault(params: Params, field: string, fallback: number): number {
  const filters = (params.filters ?? {}) as Record<string, string>;
  return Math.max(fallback, Number(filters[field]) || 0);
}

const DYNAMIC_DEFAULTS: Readonly<Record<string, (params: Params) => Partial<RehearsalScenario>>> = {
  'twitch.chat.command': (params) => ({ message: `${params.name ?? ''} @Lotti good luck` }),
  'twitch.chat.word': (params) => ({ message: `I love ${commandWord(params)}` }),
  'twitch.cheer': (params) => ({ bits: thresholdDefault(params, 'amount', 500) }),
  'twitch.raid': (params) => ({ viewers: thresholdDefault(params, 'viewers', 42) }),
  'twitch.reward': (params) => ({ reward_id: String(params.reward_id ?? '') }),
  'countdown.finished': (params) => ({ name: String(params.name ?? '') })
};

function commandMatch(params: Params, message: string): RegExpExecArray | null {
  const flags = params.case_sensitive === true ? 'u' : 'iu';
  const pattern = commandPattern(params);
  const raw = params.match === 'anywhere' || params.match === 'regex';
  return new RegExp(pattern, flags).exec(raw ? message : message.trim());
}

function commandPattern(params: Params): string {
  if (params.match === 'regex') return String(params.name ?? '');
  const names = [String(params.name ?? ''), ...((params.aliases ?? []) as string[])];
  const escape = (name: string) => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const body = names.map(escape).join('|');
  const patterns: Record<string, string> = { start: `^(?:${body})(?:$|\\s)`, exact: `^(?:${body})$`, anywhere: `(?:${body})` };
  return patterns[String(params.match ?? 'start')] ?? patterns.start!;
}

export function scenarioArgs(definition: Definition, scenario: RehearsalScenario): string {
  if (definition.trigger.type !== 'command') return scenario.args ?? '';
  const params = definition.trigger.params ?? {};
  const message = scenario.message ?? '';
  if (params.match === 'regex') return message;
  if (params.match === 'exact' || params.match === 'anywhere') return '';
  const match = commandMatch(params, message);
  return match ? message.trim().slice(match[0].length).trim() : '';
}

export function scenarioEvent(scenario: RehearsalScenario, kind: string): Record<string, string> {
  const fields: Record<string, string | number | boolean | undefined> = {
    user: scenario.user, login: scenario.user?.toLowerCase(), user_id: scenario.userId,
    tier: scenario.tier, months: scenario.months, viewers: scenario.viewers, input: scenario.input,
    text: scenario.requestText, anonymous: scenario.anonymous, is_gift: scenario.is_gift,
    message: scenario.message, reward_id: scenario.reward_id
  };
  if (kind === 'channel.cheer') fields.amount = scenario.bits;
  if (kind === 'channel.subscription.gift') fields.amount = scenario.giftCount;
  const allowed = new Set(lookupEventKind(kind)?.fields.map((field) => field.name) ?? []);
  const supplied = Object.entries(fields).filter(([key, value]) => allowed.has(key) && value !== undefined);
  return Object.fromEntries(supplied.map(([key, value]) => [key, String(value)]));
}

function filterMatch(entry: [string, string], event: Record<string, string>): boolean {
  const [key, expected] = entry;
  if (key === 'amount' || key === 'viewers') return Number(event[key]) >= Number(expected);
  return event[key]?.toLowerCase() === expected.toLowerCase();
}

export function matchTrigger(definition: Definition, scenario: RehearsalScenario): TriggerMatch {
  const { type, params = {} } = definition.trigger;
  const identityKeys: Record<string, 'name' | 'reward_id'> = { reward: 'reward_id', countdown: 'name' };
  if (identityKeys[type]) return matchIdentity(identityKeys[type]!, params, scenario);
  return type === 'command' ? matchCommand(params, scenario) : matchEvent(params, scenario);
}

function matchEvent(params: Params, scenario: RehearsalScenario): TriggerMatch {
  const event = scenarioEvent(scenario, String(params.kind));
  const filters = Object.entries((params.filters ?? {}) as Record<string, string>);
  const failed = filters.find((entry) => !filterMatch(entry, event));
  if (!failed) return { starts: true };
  return { starts: false, reason: 'trigger_filter', values: { field: failed[0], expected: failed[1], actual: event[failed[0]] ?? '' } };
}

function matchCommand(params: Params, scenario: RehearsalScenario): TriggerMatch {
  if (commandMatch(params, scenario.message ?? '')) return { starts: true };
  return { starts: false, reason: 'trigger_message', values: { expected: String(params.name), actual: scenario.message ?? '' } };
}

function matchIdentity(key: 'name' | 'reward_id', params: Params, scenario: RehearsalScenario): TriggerMatch {
  if (scenario[key] === String(params[key] ?? '')) return { starts: true };
  return { starts: false, reason: 'trigger_filter', values: { field: key, expected: String(params[key] ?? ''), actual: scenario[key] ?? '' } };
}
