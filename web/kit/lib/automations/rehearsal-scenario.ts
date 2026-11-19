// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lookupEventKind, type EventField, type EventKind } from './event-kinds';
import { unescapeWord } from './flow-graph';
import type { Definition, Params } from './types';
import type { RehearsalScenario, ScenarioField, TriggerMatch } from './rehearsal-types';

const VIEWER: readonly ScenarioField[] = [
  { key: 'user', kind: 'text', default: 'PrincessBarney' },
  { key: 'userId', kind: 'text', default: '40000001' }
];
const ROLE: ScenarioField = { key: 'role', kind: 'role', default: 'viewer', options: ['viewer', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'] };
const BROADCASTER = { role: 'broadcaster', userId: '40000000' } as const;
const LIVE: ScenarioField = { key: 'live', kind: 'toggle', default: true };
const TIER: ScenarioField = { key: 'tier', kind: 'select', default: '1000', options: ['1000', '2000', '3000'] };
const STREAM_INFO: readonly ScenarioField[] = [
  { key: 'title', kind: 'text', default: 'A cozy stream' },
  { key: 'category', kind: 'text', default: 'Just Chatting' }
];

export const SCENARIO_FIELDS: Readonly<Record<string, readonly ScenarioField[]>> = {
  'twitch.chat.command': [...VIEWER, ROLE, { key: 'message', kind: 'text', default: '' }, LIVE],
  'twitch.chat.word': [...VIEWER, ROLE, { key: 'message', kind: 'text', default: '' }, LIVE],
  'twitch.follow': [...VIEWER, LIVE],
  'twitch.sub.new': [...VIEWER, TIER, { key: 'isGift', kind: 'toggle', default: false }, LIVE],
  'twitch.sub.resub': [...VIEWER, TIER, { key: 'months', kind: 'number', default: 14 }, { key: 'streak', kind: 'number', default: 3 },
    { key: 'message', kind: 'text', default: 'Glad to be back!' }, LIVE],
  'twitch.sub.gift': [...VIEWER, TIER, { key: 'giftCount', kind: 'number', default: 5 }, { key: 'total', kind: 'number', default: 20 },
    { key: 'anonymous', kind: 'toggle', default: false }, LIVE],
  'twitch.cheer': [...VIEWER, { key: 'bits', kind: 'number', default: 500 }, { key: 'message', kind: 'text', default: 'GG!' },
    { key: 'anonymous', kind: 'toggle', default: false }, LIVE],
  'twitch.raid': [...VIEWER, { key: 'viewers', kind: 'number', default: 42 }, LIVE],
  'twitch.reward': [...VIEWER, { key: 'input', kind: 'text', default: 'lofi beats' }, { key: 'reward', kind: 'text', default: 'VIP' },
    { key: 'cost', kind: 'number', default: 500 }, LIVE],
  'twitch.unban_request': [...VIEWER, { key: 'requestText', kind: 'text', default: 'please unban me' }, LIVE],
  'twitch.stream.online': [...STREAM_INFO, LIVE],
  'twitch.stream.offline': [{ ...LIVE, default: false }],
  'twitch.channel_update': [...STREAM_INFO, LIVE],
  'twitch.ad_break': [{ key: 'seconds', kind: 'number', default: 90 }, { key: 'automatic', kind: 'toggle', default: false }, LIVE],
  'schedule.timer': [LIVE],
  'schedule.manual': [{ key: 'args', kind: 'text', default: '' }, LIVE],
  'countdown.finished': [LIVE]
};

export function scenarioPatches(fields: readonly ScenarioField[], patch: Partial<RehearsalScenario>): Partial<RehearsalScenario> {
  const keys = new Set<string>([...fields.map((field) => field.key), 'state']);
  return Object.fromEntries(Object.entries(patch).filter(([key]) => keys.has(key)));
}

const EVENT_SCENARIOS: Readonly<Record<string, string>> = {
  'channel.follow': 'twitch.follow',
  'channel.subscribe': 'twitch.sub.new',
  'channel.subscription.message': 'twitch.sub.resub',
  'channel.subscription.gift': 'twitch.sub.gift',
  'channel.cheer': 'twitch.cheer',
  'channel.raid': 'twitch.raid',
  'channel.unban_request.create': 'twitch.unban_request',
  'channel.update': 'twitch.channel_update',
  'channel.ad_break.begin': 'twitch.ad_break',
  'stream.online': 'twitch.stream.online',
  'stream.offline': 'twitch.stream.offline'
};

export function scenarioKind(definition: Definition): string {
  const { type, params = {} } = definition.trigger;
  if (type === 'command') return params.match === 'regex' || params.match === 'anywhere' ? 'twitch.chat.word' : 'twitch.chat.command';
  const direct: Record<string, string> = { reward: 'twitch.reward', countdown: 'countdown.finished', timer: 'schedule.timer', manual: 'schedule.manual' };
  return direct[type] ?? EVENT_SCENARIOS[String(params.kind)] ?? '';
}

function sampleWord(params: Params): string {
  if (params.match === 'anywhere') return String(params.name ?? '');
  const body = String(params.name ?? '').replace(/^\(\?:\^\|\[\^\\p\{L\}\\p\{N\}_\]\)/, '').replace(/\(\?:\$\|\[\^\\p\{L\}\\p\{N\}_\]\)$/, '');
  return unescapeWord(body);
}

export function defaultScenario(definition: Definition): RehearsalScenario {
  const kind = scenarioKind(definition);
  const fields = SCENARIO_FIELDS[kind] ?? [...VIEWER, LIVE];
  const scenario: RehearsalScenario = { live: true, ...Object.fromEntries(fields.map((field) => [field.key, field.default])) };
  const params = definition.trigger.params ?? {};
  Object.assign(scenario, DYNAMIC_DEFAULTS[kind]?.(params, scenario));
  scenario.args = scenarioArgs(definition, scenario);
  return scenario;
}

const eventFilters = (params: Params): Readonly<Record<string, string>> => (params.filters ?? {}) as Record<string, string>;

function thresholdDefault(filters: ReturnType<typeof eventFilters>, field: EventField['name'], fallback: number): number {
  return Math.max(fallback, Number(filters[field]) || 0);
}

const DYNAMIC_DEFAULTS: Readonly<Record<string, (params: Params, sample: RehearsalScenario) => Partial<RehearsalScenario>>> = {
  'twitch.chat.command': (params) => ({ message: params.match === 'exact' ? String(params.name ?? '') : `${params.name ?? ''} @Lotti good luck` }),
  'twitch.chat.word': (params) => ({ message: `I love ${sampleWord(params)}` }),
  'twitch.sub.new': (params, sample) => {
    const filters = eventFilters(params);
    return { tier: filters.tier ?? sample.tier, isGift: filters.is_gift?.toLowerCase() === 'true' };
  },
  'twitch.sub.resub': (params, sample) => {
    const filters = eventFilters(params);
    return { tier: filters.tier ?? sample.tier, months: thresholdDefault(filters, 'months', sample.months ?? 0), streak: thresholdDefault(filters, 'streak', sample.streak ?? 0) };
  },
  'twitch.sub.gift': (params, sample) => {
    const filters = eventFilters(params);
    const giftCount = thresholdDefault(filters, 'amount', sample.giftCount ?? 0);
    return { giftCount, total: Math.max(giftCount, sample.total ?? 0), tier: filters.tier ?? sample.tier, anonymous: filters.anonymous?.toLowerCase() === 'true' };
  },
  'twitch.cheer': (params, sample) => {
    const filters = eventFilters(params);
    return { bits: thresholdDefault(filters, 'amount', sample.bits ?? 0), anonymous: filters.anonymous?.toLowerCase() === 'true' };
  },
  'twitch.raid': (params, sample) => ({ viewers: thresholdDefault(eventFilters(params), 'viewers', sample.viewers ?? 0) }),
  'twitch.reward': (params, sample) => ({ rewardId: String(params.reward_id ?? ''), reward: String(params.title ?? sample.reward), cost: Number(params.cost ?? sample.cost) }),
  'twitch.channel_update': (params, sample) => ({ category: eventFilters(params).category ?? sample.category }),
  'twitch.ad_break': (params) => ({ automatic: eventFilters(params).automatic?.toLowerCase() === 'true' }),
  'countdown.finished': (params) => ({ name: String(params.name ?? ''), ...BROADCASTER })
};

function commandMatch(params: Params, message: string): RegExpExecArray | null {
  const flags = params.case_sensitive === true ? 'u' : 'iu';
  const pattern = commandPattern(params);
  const raw = params.match === 'anywhere' || params.match === 'regex';
  try {
    return new RegExp(pattern, flags).exec(raw ? message : message.trim());
  } catch (error) {
    if (!(error instanceof SyntaxError)) throw error;
    return null;
  }
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

export function scenarioEvent(scenario: RehearsalScenario, kind: EventKind['kind']): Record<string, string> {
  if (kind === 'countdown.finished') return { name: scenario.name ?? '' };
  const fields: Record<string, string | number | boolean | undefined> = {
    user: scenario.user, login: scenario.user?.toLowerCase(), user_id: scenario.userId,
    tier: scenario.tier, months: scenario.months, viewers: scenario.viewers, input: scenario.input,
    text: scenario.requestText, anonymous: scenario.anonymous, is_gift: scenario.isGift,
    message: scenario.message, reward_id: scenario.rewardId,
    streak: scenario.streak, total: scenario.total, reward: scenario.reward, cost: scenario.cost,
    title: scenario.title, category: scenario.category, seconds: scenario.seconds, automatic: scenario.automatic
  };
  if (kind === 'channel.cheer') fields.amount = scenario.bits;
  if (kind === 'channel.subscription.gift') fields.amount = scenario.giftCount;
  if (kind === 'channel.unban_request.create') fields.id = 'unban-request-1';
  const allowed = new Set(lookupEventKind(kind)?.fields.map((field) => field.name) ?? []);
  if (kind === 'stream.online') ['title', 'category'].forEach((key) => allowed.add(key));
  const supplied = Object.entries(fields).filter(([key, value]) => allowed.has(key) && value !== undefined);
  return Object.fromEntries(supplied.map(([key, value]) => [key, String(value)]));
}

type EventValues = ReturnType<typeof scenarioEvent>;

function eventInteger(value: EventValues[string]): bigint | null {
  if (!/^[+-]?\d+$/.test(value)) return null;
  const number = BigInt(value);
  return BigInt.asIntN(64, number) === number ? number : null;
}

function filterMatch(entry: [string, string], event: EventValues, fields: readonly EventField[]): boolean {
  const [key, expected] = entry;
  const field = fields.find((candidate) => candidate.name === key);
  if (!field) return false;
  const actual = event[key] ?? '';
  if (!field.numeric) return actual.toLowerCase() === expected.toLowerCase();
  const have = eventInteger(actual);
  const minimum = eventInteger(expected);
  return have !== null && minimum !== null && have >= minimum;
}

export function matchTrigger(definition: Definition, scenario: RehearsalScenario): TriggerMatch {
  const { type, params = {} } = definition.trigger;
  const identityKeys: Record<string, 'name' | 'rewardId'> = { reward: 'rewardId', countdown: 'name' };
  if (identityKeys[type]) return matchIdentity(identityKeys[type]!, params, scenario);
  if (type === 'command') return matchCommand(params, scenario);
  if (type === 'timer' && timerWaitsForLive(params, scenario)) return { starts: false, reason: 'trigger_offline', values: {} };
  return matchEvent(params, scenario);
}

function timerWaitsForLive(params: Params, scenario: RehearsalScenario): boolean {
  return !scenario.live && params.offline_ok !== true;
}

function matchEvent(params: Params, scenario: RehearsalScenario): TriggerMatch {
  const kind = String(params.kind ?? '');
  const event = scenarioEvent(scenario, kind);
  const fields = lookupEventKind(kind)?.fields ?? [];
  const failed = Object.entries(eventFilters(params)).find((entry) => !filterMatch(entry, event, fields));
  if (!failed) return { starts: true };
  return { starts: false, reason: 'trigger_filter', values: { field: failed[0], expected: failed[1], actual: event[failed[0]] ?? '' } };
}

function matchCommand(params: Params, scenario: RehearsalScenario): TriggerMatch {
  if (!String(params.name ?? '').trim()) return { starts: false, reason: 'no_name', values: {} };
  if (commandMatch(params, scenario.message ?? '')) return { starts: true };
  return { starts: false, reason: 'trigger_message', values: { expected: String(params.name), actual: scenario.message ?? '' } };
}

function matchIdentity(key: 'name' | 'rewardId', params: Params, scenario: RehearsalScenario): TriggerMatch {
  const field = key === 'rewardId' ? 'reward_id' : key;
  if (scenario[key] === String(params[field] ?? '')) return { starts: true };
  return { starts: false, reason: 'trigger_filter', values: { field, expected: String(params[field] ?? ''), actual: scenario[key] ?? '' } };
}

const BROADCASTER_TRIGGERS = new Set(['countdown', 'timer', 'manual']);

export function scenarioIdentity(definition: Definition, scenario: RehearsalScenario): RehearsalScenario {
  if (definition.trigger.type === 'command') return scenario;
  if (BROADCASTER_TRIGGERS.has(definition.trigger.type)) return { ...scenario, ...BROADCASTER, user: undefined };
  const hidden = hidesViewer(definition, scenario);
  return { ...scenario, role: 'viewer', user: hidden ? undefined : scenario.user, userId: hidden ? undefined : scenario.userId };
}

function hidesViewer(definition: Definition, scenario: RehearsalScenario): boolean {
  const fields = SCENARIO_FIELDS[scenarioKind(definition)];
  const viewer = fields ? fields.some((field) => field.key === 'user') : true;
  const anonymous = ['channel.cheer', 'channel.subscription.gift'].includes(String(definition.trigger.params?.kind)) && scenario.anonymous;
  return Boolean(anonymous || !viewer);
}
