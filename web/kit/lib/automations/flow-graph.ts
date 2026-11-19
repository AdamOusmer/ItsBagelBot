// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { streamActionReaders, streamActionWriters, streamTargetNodes, usesLegacyTarget } from './stream-flow';
import { COMPARE_OPS } from './compare';
import { SETUP_LOGIN, SETUP_REWARD_ID } from './flow-setup';
import { CODE, MAX_DELAY_MS, VERSION } from './limits';
import type { Action, Condition, Definition, Issue, Params, Trigger } from './types';

export type FlowConfig = Record<string, string | number | boolean>;
export type FlowNode = { id: string; type: string; x: number; y: number; config: FlowConfig };
export type FlowEdge = { id: string; from: string; port: string; to: string };
export type Flow = { id: string; name: string; enabled: boolean; nodes: FlowNode[]; edges: FlowEdge[]; queue?: string };
export type FlowMeta = Pick<Flow, 'id' | 'name' | 'enabled'>;

export type FlowResult<T> = { ok: true; value: T } | { ok: false; issues: Issue[] };

export const FLOW_ISSUE = {
  noTrigger: 'no_trigger',
  multipleTriggers: 'multiple_triggers',
  cycle: 'cycle',
  unconnectedNode: 'unconnected_node',
  unstructuredMerge: 'unstructured_merge',
  unsupportedNode: 'unsupported_node',
  unsupportedTrigger: 'unsupported_trigger',
  unsupportedCondition: 'unsupported_condition',
  unsupportedAction: 'unsupported_action',
  unsupportedDisabledControlFlow: 'unsupported_disabled_control_flow',
  waitTooLong: 'wait_too_long',
  unmappableToken: 'unmappable_token',
  userNotResolved: 'user_not_resolved'
} as const;

const REWARD_KIND = 'channel.channel_points_custom_reward_redemption.add';

const EVENT_TOKENS: Record<string, Record<string, string>> = {
  'channel.cheer': { bits: 'amount', message: 'message' },
  'channel.raid': { viewers: 'viewers' },
  'channel.subscribe': { tier: 'tier' },
  'channel.subscription.message': { tier: 'tier', months: 'months', streak: 'streak', message: 'message' },
  'channel.subscription.gift': { tier: 'tier', count: 'amount', total: 'total' },
  [REWARD_KIND]: { input: 'input', reward: 'reward', cost: 'cost' },
  'channel.unban_request.create': { text: 'text' },
  'channel.ad_break.begin': { seconds: 'seconds' },
  'channel.update': { title: 'title', game: 'category' }
};

const EVENT_ONLY_TOKENS = new Set(['bits', 'viewers', 'tier', 'months', 'input', 'message', 'streak', 'total', 'reward', 'cost', 'text', 'seconds']);

const BARE_TOKEN = /\{([a-z]+)\}/g;
const EVENT_TOKEN = /\{event\.([a-z_]+)\}/g;

export function builderTokensToEvent(kind: string, text: string): string {
  const tokens = EVENT_TOKENS[kind] ?? {};
  return text.replace(BARE_TOKEN, (token, name: string) => (tokens[name] ? `{event.${tokens[name]}}` : token));
}

export function eventTokensToBuilder(kind: string, text: string, keepCount = false): string {
  const tokens = Object.entries(EVENT_TOKENS[kind] ?? {}).filter(([name]) => !keepCount || name !== 'count');
  return text.replace(EVENT_TOKEN, (token, field: string) => {
    const match = tokens.find(([, mapped]) => mapped === field);
    return match ? `{${match[0]}}` : token;
  });
}

const SUB_KINDS: Record<string, string> = {
  any: 'channel.subscribe',
  new: 'channel.subscribe',
  gifted: 'channel.subscribe',
  resub: 'channel.subscription.message',
  gift: 'channel.subscription.gift'
};

type ThresholdSpec = { type: string; key: string; filter: string; actor?: 'cheerer' | 'gifter'; tier?: true };

const THRESHOLD_FILTERS: Record<string, ThresholdSpec> = {
  'channel.cheer': { type: 'twitch.cheer', key: 'minBits', filter: 'amount', actor: 'cheerer' },
  'channel.raid': { type: 'twitch.raid', key: 'minViewers', filter: 'viewers' },
  [SUB_KINDS.gift!]: { type: 'twitch.sub_gift', key: 'minGifts', filter: 'amount', actor: 'gifter', tier: true }
};

const SUB_MONTHS = { months: 'minMonths', streak: 'minStreak' } as const;
const SUB_VARIANTS = new Map<string | undefined, string>([[undefined, 'any'], ['false', 'new'], ['true', 'gifted']]);
const ANONYMOUS = new Map<string, string>([['named', 'false'], ['anonymous', 'true']]);
const ANONYMOUS_OPTIONS = new Map<string | undefined, string>([[undefined, 'anyone'], ['false', 'named'], ['true', 'anonymous']]);
const AD_BREAK_KIND = 'channel.ad_break.begin';
const CHANNEL_UPDATE_KIND = 'channel.update';
const AD_STARTED_BY = new Map<string, string>([['automatic', 'true'], ['manual', 'false']]);
const AD_STARTED_BY_OPTIONS = new Map<string | undefined, string>([[undefined, 'any'], ['true', 'automatic'], ['false', 'manual']]);
const AD_MINIMUMS = { seconds: 'minLength' } as const;
const CHAT_SOURCES = 'chat,sub_message';
const COMMAND_KEYS = new Set(['name', 'aliases', 'match', 'case_sensitive', 'sources']);

const FIXED_EVENT_NODES: Record<string, string> = {
  'twitch.follow': 'channel.follow',
  'twitch.unban_request': 'channel.unban_request.create',
  'twitch.stream.online': 'stream.online',
  'twitch.stream.offline': 'stream.offline',
  'twitch.ad_break': AD_BREAK_KIND,
  'twitch.channel_update': CHANNEL_UPDATE_KIND
};

const isTriggerType = (type: string) => type.startsWith('twitch.') || type.startsWith('schedule.') || type === 'countdown.finished';
const isConditionType = (type: string) => type.startsWith('cond.');

const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown): number => (typeof v === 'number' ? v : Number(v) || 0);

function eventTrigger(kind: string, filters?: Record<string, string>): Trigger {
  return { type: 'event', params: filters ? { kind, filters } : { kind } };
}

function rewardTrigger(config: FlowConfig): Trigger {
  const title = str(config.title).trim();
  if (title === '') return { type: 'reward', params: { reward_id: str(config.reward) } };
  const params = {
    reward_id: str(config.reward) || SETUP_REWARD_ID,
    title,
    cost: num(config.cost),
    color: str(config.color),
    cooldown_s: num(config.cooldown),
    on_redeem: str(config.onRedeem) || 'fulfill',
    input_required: config.userInput === true
  };
  return { type: 'reward', params };
}

function thresholdTrigger(kind: string, config: FlowConfig): Trigger {
  const threshold = THRESHOLD_FILTERS[kind]!;
  const filters = threshold.tier ? tierFilter(config) : {};
  const anonymous = threshold.actor && ANONYMOUS.get(str(config[threshold.actor]));
  if (anonymous) filters.anonymous = anonymous;
  writeMinimums(filters, config, { [threshold.filter]: threshold.key });
  return filteredEvent(kind, filters);
}

function tierFilter(config: FlowConfig): Record<string, string> {
  const tier = str(config.tier);
  return tier && tier !== 'any' ? { tier } : {};
}

function writeMinimums(filters: Record<string, string>, config: FlowConfig, fields: Record<string, string>): void {
  for (const [filter, key] of Object.entries(fields)) {
    const value = config[key];
    if (typeof value === 'boolean') continue;
    const minimum = Number(value);
    if (Number.isInteger(minimum) && minimum >= 1) filters[filter] = String(minimum);
  }
}

function filteredEvent(kind: string, filters: Record<string, string>): Trigger {
  return eventTrigger(kind, Object.keys(filters).length > 0 ? filters : undefined);
}

function bangName(name: string): string {
  return name === '' || name.startsWith('!') ? name : `!${name}`;
}

function aliasList(text: string): string[] {
  return text.split(',').map((alias) => alias.trim()).filter(Boolean);
}

function storedAliases(config: FlowConfig): string[] | undefined {
  if (typeof config.importedAliases !== 'string') return undefined;
  const aliases = config.importedAliases.split('\n');
  return config.aliases === aliases.join(', ') ? aliases : undefined;
}

function chatTrigger(config: FlowConfig, params: Params, aliases: string[] = []): Trigger {
  if (config.caseSensitive === true) params.case_sensitive = true;
  if (config.subMessages === true) params.sources = (str(config.importedSources) || CHAT_SOURCES).split(',');
  if (aliases.length > 0) params.aliases = aliases;
  return { type: 'command', params };
}

function commandTrigger(config: FlowConfig): Trigger {
  const name = str(config.trigger);
  const params: Params = { name: config.importedName === name ? name : bangName(name.trim()) };
  if (config.match === 'exact') params.match = 'exact';
  return chatTrigger(config, params, storedAliases(config) ?? aliasList(str(config.aliases)).map(bangName));
}

const WORD_START = '(?:^|[^\\p{L}\\p{N}_])';
const WORD_END = '(?:$|[^\\p{L}\\p{N}_])';

const escapeWord = (word: string) => word.replace(/[\\.*+?^$()[\]{}|]/g, (char) => (char === '\\' ? '\\x5c' : `\\${char}`));
export const unescapeWord = (body: string) => body.replace(/\\x5c|\\(.)/g, (_, char?: string) => char ?? '\\');

function wordPattern(word: string): string {
  return word === '' ? '' : `${WORD_START}${escapeWord(word)}${WORD_END}`;
}

function wordTrigger(config: FlowConfig): Trigger {
  const word = str(config.word);
  if (config.match === 'anywhere') {
    const aliases = storedAliases(config) ?? aliasList(str(config.aliases));
    return chatTrigger(config, { name: word, match: 'anywhere' }, aliases);
  }
  const literal = config.importedWord === word ? word : word.trim();
  const name = config.match === 'pattern' ? word : wordPattern(literal);
  return chatTrigger(config, { name, match: 'regex' });
}

function subTrigger(config: FlowConfig): Trigger | null {
  const variant = str(config.variant) || 'any';
  const kind = SUB_KINDS[variant];
  if (!Object.hasOwn(SUB_KINDS, variant)) return null;
  if (variant === 'gift') return thresholdTrigger(kind!, config);
  const filters = tierFilter(config);
  if (variant === 'new' || variant === 'gifted') filters.is_gift = String(variant === 'gifted');
  if (variant === 'resub') writeMinimums(filters, config, SUB_MONTHS);
  return filteredEvent(kind!, filters);
}

function timerTrigger(config: FlowConfig): Trigger {
  const params: Params = { interval_s: num(config.every), min_chat_lines: num(config.chatLines) };
  const jitter = num(config.jitter);
  if (jitter !== 0) params.random_jitter_s = jitter;
  if (config.offline === true) params.offline_ok = true;
  return { type: 'timer', params };
}

function adBreakTrigger(config: FlowConfig): Trigger {
  const filters: Record<string, string> = {};
  const automatic = AD_STARTED_BY.get(str(config.startedBy));
  if (automatic) filters.automatic = automatic;
  writeMinimums(filters, config, AD_MINIMUMS);
  return filteredEvent(AD_BREAK_KIND, filters);
}

function channelUpdateTrigger(config: FlowConfig): Trigger {
  const category = str(config.category);
  return filteredEvent(CHANNEL_UPDATE_KIND, category ? { category } : {});
}

const TRIGGER_WRITERS: Record<string, (config: FlowConfig) => Trigger | null> = {
  'countdown.finished': (config) => ({ type: 'countdown', params: { name: recipeName(config.name) } }),
  'twitch.chat.command': commandTrigger,
  'twitch.chat.word': wordTrigger,
  'twitch.follow': () => eventTrigger('channel.follow'),
  'twitch.unban_request': () => eventTrigger('channel.unban_request.create'),
  'twitch.stream.online': () => eventTrigger('stream.online'),
  'twitch.stream.offline': () => eventTrigger('stream.offline'),
  'twitch.sub': subTrigger,
  'twitch.sub_gift': (config) => thresholdTrigger(SUB_KINDS.gift!, config),
  'twitch.cheer': (config) => thresholdTrigger('channel.cheer', config),
  'twitch.raid': (config) => thresholdTrigger('channel.raid', config),
  'twitch.reward': rewardTrigger,
  'twitch.ad_break': adBreakTrigger,
  'twitch.channel_update': channelUpdateTrigger,
  'schedule.timer': timerTrigger,
  'schedule.manual': () => ({ type: 'manual' })
};

const TEMPLATE_FIELDS: Record<string, string[]> = {
  'reply.shoutout': ['target'],
  'reply.announce': ['text'],
  'reply.reply': ['text'],
  'reply.timeout': ['target', 'reason'],
  'reply.ban': ['target', 'reason'],
  'reply.poll': ['title', 'choices'],
  'reply.prediction': ['title', 'outcomes'],
  'reply.raid': ['target'],
  'reply.mod': ['target'],
  'reply.vip': ['target'],
  'reply.points': ['target'],
  'reply.points_set': ['target'],
  'reply.whisper': ['target', 'text'],
  'reply.unban_resolve': ['resolution'],
  'reply.chat': ['message'],
  'cond.compare': ['left', 'right'],
  'reply.var_set': ['value'],
  'reply.math': ['expression'],
  'reply.loop': ['times'],
  'reply.http_get': ['url'],
  'reply.http_post': ['url', 'body']
};

const recipeName = (v: unknown) => str(v).trim().toLowerCase();

const varIdentity = (config: FlowConfig): Params => ({ scope: str(config.scope) || 'channel', name: recipeName(config.name) });

export const TEXT_ARG_FIELDS = new Map<string, readonly string[]>([
  ['replace', ['find', 'replaceWith']],
  ['substring', ['start', 'count']],
  ['split_pick', ['separator', 'pick']],
  ['contains', ['needle']]
]);

export const VAR_OPS_WITH_VALUE: readonly string[] = ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'contains'];

const textOp = (config: FlowConfig) => str(config.op) || 'upper';
const varConditionOp = (config: FlowConfig) => str(config.op) || 'eq';

function templateFields(node: FlowNode): readonly string[] {
  if (node.type === 'reply.text') return ['input', ...TEXT_ARG_FIELDS.get(textOp(node.config)) ?? []];
  if (node.type === 'cond.var') return VAR_OPS_WITH_VALUE.includes(varConditionOp(node.config)) ? ['value'] : [];
  const fields = TEMPLATE_FIELDS[node.type] ?? [];
  if (!streamTargetNodes.has(node.type)) return fields;
  const legacy = usesLegacyTarget(node.config.who);
  const active = (field: string): boolean => field !== 'target' || legacy;
  return fields.filter((field) => node.config[field] !== undefined && active(field));
}

function textAction(config: FlowConfig): Action {
  const op = textOp(config);
  const [first, second] = TEXT_ARG_FIELDS.get(op) ?? [];
  const arg = (key: string | undefined) => (key ? str(config[key]) : '');
  return { type: 'text', params: { op, input: str(config.input), arg1: arg(first), arg2: arg(second), save_as: recipeName(config.saveAs) } };
}

const REWARD_ENABLED = new Map<string, boolean | null>([
  ['keep', null],
  ['on', true],
  ['off', false]
]);

function rewardUpdateAction(config: FlowConfig): Action {
  const cost = config.cost === undefined || config.cost === '' ? null : num(config.cost);
  const enabled = REWARD_ENABLED.get(str(config.setEnabled)) ?? null;
  return { type: 'reward_update', params: { reward_id: str(config.reward), enabled, cost, reset_on_stream_end: config.reset === true } };
}

function withOptional(params: Params, optional: Params): Params {
  for (const [key, value] of Object.entries(optional)) if (value) params[key] = value;
  return params;
}

const JSON_PATH_SEPARATOR = '.';

function jsonPathOf(config: FlowConfig): string[] | undefined {
  const segments = str(config.jsonPath).trim().split(JSON_PATH_SEPARATOR).filter(Boolean);
  return segments.length > 0 ? segments : undefined;
}

const API_WRITERS: Record<string, (config: FlowConfig) => Action> = {
  'reply.http_get': (config) => ({
    type: 'http_get',
    params: withOptional({ url: str(config.url) }, { json_path: jsonPathOf(config), save_as: recipeName(config.saveAs) })
  }),
  'reply.http_post': (config) => ({
    type: 'http_post',
    params: withOptional({ url: str(config.url) }, { body: str(config.body), save_as: recipeName(config.saveAs) })
  })
};

const API_PORTS = ['ok', 'failure'] as const;
const LOOP_TYPE = 'reply.loop';
const LOOP_PORTS = ['body', 'done'] as const;

const DEFAULT_LIGHT_COLOR = '#ffffff';

function lightParams(config: FlowConfig): Params {
  const params: Params = { device: str(config.device), sku: str(config.sku) };
  if (str(config.deviceName)) params.device_name = str(config.deviceName);
  const extra = str(config.extraLights).split(',').filter(Boolean);
  if (extra.length) params.extra_lights = extra;
  if (config.liveOnly === false) params.any_time = true;
  return params;
}

function sceneParams(config: FlowConfig): Params {
  const picked = str(config.scene) !== '';
  const keep = (key: string) => (picked ? str(config[key]) : '');
  const scene: Params = { scene_id: str(config.scene), scene_type: keep('sceneType'), scene_instance: keep('sceneInstance'), scene_value: keep('sceneValue') };
  if (keep('sceneName')) scene.scene_name = keep('sceneName');
  return scene;
}

function lightsAction(config: FlowConfig): Action {
  const params = lightParams(config);
  if (config.mode === 'scene') return { type: 'lights', params: { ...params, mode: 'scene', ...sceneParams(config) } };
  return { type: 'lights', params: { ...params, mode: 'color', color: str(config.color) || DEFAULT_LIGHT_COLOR } };
}

const awaitingUser = (config: FlowConfig) => config.login === SETUP_LOGIN;

const COOLDOWN_PASSTHROUGH_FIELDS = [
  ['name', 'name'],
  ['leftCommand', 'left_command'],
  ['leftReply', 'left_reply'],
  ['readyReply', 'ready_reply']
] as const;

function timeLeftParams(config: FlowConfig): Params {
  return Object.fromEntries(COOLDOWN_PASSTHROUGH_FIELDS.filter(([key]) => str(config[key]).trim() !== '').map(([key, param]) => [param, str(config[key]).trim()]));
}

function timeLeftConfig(params: Params): FlowConfig {
  return Object.fromEntries(COOLDOWN_PASSTHROUGH_FIELDS.filter(([, param]) => str(params[param]) !== '').map(([key, param]) => [key, str(params[param])]));
}

const CONDITION_WRITERS: Record<string, (config: FlowConfig) => Condition> = {
  'cond.compare': (config) => ({ type: 'compare', params: { left: str(config.left), op: str(config.op) || 'gte', right: str(config.right) } }),
  'cond.role': (config) => ({ type: 'perm', params: { level: str(config.level) } }),
  'cond.chance': (config) => ({ type: 'chance', params: { percent: num(config.percent) } }),
  'cond.var': (config) => {
    const op = varConditionOp(config);
    return { type: 'var', params: { ...varIdentity(config), op, value: VAR_OPS_WITH_VALUE.includes(op) ? str(config.value) : '' } };
  },
  'cond.user': (config) => ({ type: 'users', params: awaitingUser(config) ? { allow: [SETUP_LOGIN] } : { allow_ids: [str(config.userId)] } }),
  'cond.cooldown': cooldownConditionAction
};

function cooldownConditionAction(config: FlowConfig): Condition {
  const perUser = Boolean(config.perUser);
  const params: Params = { [perUser ? 'user_s' : 'global_s']: num(config.seconds), ...timeLeftParams(config) };
  if (perUser) delete params.name;
  return { type: 'cooldown', params };
}

const ACTION_WRITERS: Record<string, (config: FlowConfig) => Action> = {
  ...streamActionWriters,
  'reply.chat': (config) => ({ type: 'chat', params: { text: str(config.message) } }),
  'reply.lights': lightsAction,
  'reply.counter': (config) => ({ type: 'counter_add', params: { name: str(config.counter), amount: 1 } }),
  'reply.wait': (config) => ({ type: 'delay', params: { ms: num(config.seconds) * 1000 } }),
  'reply.var_set': (config) => ({ type: 'var_set', params: { ...varIdentity(config), value: str(config.value) } }),
  'reply.var_add': (config) => ({ type: 'var_add', params: { ...varIdentity(config), amount: num(config.amount) } }),
  'reply.var_delete': (config) => ({ type: 'var_delete', params: varIdentity(config) }),
  'reply.text': textAction,
  'reply.math': (config) => ({ type: 'math', params: { expression: str(config.expression), save_as: recipeName(config.saveAs) } }),
  'reply.reward_update': rewardUpdateAction,
  'reply.countdown_start': (config) => ({ type: 'countdown_start', params: { ...timeLeftParams(config), name: recipeName(config.name), seconds: num(config.seconds) } }),
  'reply.countdown_add': (config) => ({ ...cooldownAddAction(config), type: 'countdown_add' }),
  'reply.cooldown_add': cooldownAddAction
};

function cooldownAddAction(config: FlowConfig): Action {
  const params: Params = { name: recipeName(str(config.name)), seconds: num(config.seconds) };
  if (num(config.per) > 0) params.per = num(config.per);
  return { type: 'cooldown_add', params };
}

type Lowering = {
  trigger: Trigger;
  byId: Map<string, FlowNode>;
  outs: Map<string, FlowEdge[]>;
  counters: Map<string, string[]>;
  visited: Set<string>;
  issues: Issue[];
  owners: Map<object, string>;
};

const nodePath = (id: string) => `nodes.${id}`;

function outgoing(flow: Flow): Map<string, FlowEdge[]> {
  const outs = new Map<string, FlowEdge[]>();
  for (const edge of flow.edges) {
    const list = outs.get(edge.from) ?? [];
    list.push(edge);
    outs.set(edge.from, list);
  }
  return outs;
}

function targets(ctx: Lowering, id: string, port: string): string[] {
  return (ctx.outs.get(id) ?? []).filter((e) => e.port === port).map((e) => e.to);
}

function findCycleNode(outs: Map<string, FlowEdge[]>, start: string): string | null {
  const done = new Set<string>();
  const onPath = new Set<string>();
  const walk = (id: string): string | null => {
    if (onPath.has(id)) return id;
    if (done.has(id)) return null;
    onPath.add(id);
    for (const edge of outs.get(id) ?? []) {
      const hit = walk(edge.to);
      if (hit) return hit;
    }
    onPath.delete(id);
    done.add(id);
    return null;
  };
  return walk(start);
}

function reachable(outs: Map<string, FlowEdge[]>, id: string): Set<string> {
  const seen = new Set<string>([id]);
  const pending = [id];
  for (const current of pending) {
    const next = (outs.get(current) ?? []).map((edge) => edge.to).filter((target) => !seen.has(target));
    next.forEach((target) => seen.add(target));
    pending.push(...next);
  }
  return seen;
}

function priorCounters(ctx: Pick<Lowering, 'outs' | 'byId'>): Map<string, string[]> {
  const counts = new Map<string, number>();
  for (const edges of ctx.outs.values()) for (const edge of edges) counts.set(edge.to, (counts.get(edge.to) ?? 0) + 1);
  const pending = [...ctx.byId.keys()].filter((id) => !counts.has(id));
  const prior = new Map<string, string[]>();
  for (const id of pending) {
    const node = ctx.byId.get(id)!;
    const names = prior.get(id) ?? [];
    const after = node.type === 'reply.counter' ? [...new Set([...names, str(node.config.counter)])].slice(0, 2) : names;
    for (const edge of ctx.outs.get(id) ?? []) propagateCounters({ prior, counts, pending }, edge.to, after);
  }
  return prior;
}

function propagateCounters(state: { prior: Map<string, string[]>; counts: Map<string, number>; pending: string[] }, id: string, names: string[]): void {
  state.prior.set(id, [...new Set([...(state.prior.get(id) ?? []), ...names])].slice(0, 2));
  const remaining = state.counts.get(id)! - 1;
  state.counts.set(id, remaining);
  if (remaining === 0) state.pending.push(id);
}

function triggerKind(trigger: Trigger): string {
  if (trigger.type === 'reward') return REWARD_KIND;
  return trigger.type === 'event' ? str(trigger.params?.kind) : '';
}

function lowerCountToken(ctx: Lowering, counters: string[]): string | null {
  const eventCount = EVENT_TOKENS[triggerKind(ctx.trigger)]?.count;
  if (eventCount) return counters.length > 0 ? null : `{event.${eventCount}}`;
  if (counters.length > 1) return null;
  if (counters.length === 1) return `{counter:${counters[0]}}`;
  return ctx.trigger.type === 'command' ? '{count}' : null;
}

function lowerToken(ctx: Lowering, name: string, counters: string[]): string | null {
  if (name === 'count') return lowerCountToken(ctx, counters);
  const field = EVENT_TOKENS[triggerKind(ctx.trigger)]?.[name];
  if (field) return `{event.${field}}`;
  return EVENT_ONLY_TOKENS.has(name) ? null : `{${name}}`;
}

function lowerTemplate(ctx: Lowering, node: FlowNode, field: string): string {
  const counters = ctx.counters.get(node.id) ?? [];
  return str(node.config[field]).replace(BARE_TOKEN, (token, name: string) => {
    const lowered = lowerToken(ctx, name, counters);
    if (lowered !== null) return lowered;
    ctx.issues.push({ path: `${nodePath(node.id)}.${field}.${name}`, code: FLOW_ISSUE.unmappableToken });
    return token;
  });
}

function loweredConfig(ctx: Lowering, node: FlowNode): FlowConfig {
  const lowered: FlowConfig = { ...node.config };
  for (const field of templateFields(node)) lowered[field] = lowerTemplate(ctx, node, field);
  return lowered;
}

function triggerNode(flow: Flow): FlowResult<FlowNode> {
  const triggers = flow.nodes.filter((n) => isTriggerType(n.type));
  if (triggers.length === 0) return { ok: false, issues: [{ path: 'nodes', code: FLOW_ISSUE.noTrigger }] };
  if (triggers.length > 1) {
    return { ok: false, issues: triggers.map((n) => ({ path: nodePath(n.id), code: FLOW_ISSUE.multipleTriggers })) };
  }
  return { ok: true, value: triggers[0]! };
}

function shapeIssues(flow: Flow, trigger: FlowNode, ctx: Lowering): Issue[] {
  const cycle = findCycleNode(ctx.outs, trigger.id);
  if (cycle) return [{ path: nodePath(cycle), code: FLOW_ISSUE.cycle }];
  const attached = reachable(ctx.outs, trigger.id);
  const known = new Set(flow.nodes.map((n) => n.id));
  const dangling = flow.edges.filter((e) => !known.has(e.from) || !known.has(e.to));
  return [
    ...flow.nodes.filter((n) => !attached.has(n.id)).map((n) => n.id),
    ...dangling.map((e) => (known.has(e.from) ? e.to : e.from))
  ].map((id) => ({ path: nodePath(id), code: FLOW_ISSUE.unconnectedNode }));
}

type Stops = ReadonlySet<string>;

const NO_STOPS: Stops = new Set<string>();

const reachOf = (ctx: Lowering, id: string) => reachable(ctx.outs, id);

function earliest(ctx: Lowering, shared: string[]): string[] {
  const later = new Set(shared.flatMap((id) => (ctx.outs.get(id) ?? []).map((edge) => edge.to)));
  return shared.filter((id) => !later.has(id));
}

function commonJoins(ctx: Lowering, entries: string[]): string[] {
  if (new Set(entries).size < 2) return [];
  const sets = entries.map((id) => reachOf(ctx, id));
  return earliest(ctx, [...sets[0]!].filter((id) => sets.every((s) => s.has(id))));
}

function armJoins(ctx: Lowering, first: string[], second: string[]): string[] {
  if (first.length === 0 || second.length === 0) return [];
  const other = new Set(second.flatMap((id) => [...reachOf(ctx, id)]));
  const joins = earliest(ctx, [...new Set(first.flatMap((id) => [...reachOf(ctx, id)]))].filter((id) => other.has(id)));
  const everyPathMeets = [...first, ...second].every((id) => joins.some((join) => reachOf(ctx, id).has(join)));
  return everyPathMeets ? joins : [];
}

const extended = (stop: Stops, joins: string[]): Stops => (joins.length > 0 ? new Set([...stop, ...joins]) : stop);

function visit(ctx: Lowering, id: string): boolean {
  if (!ctx.visited.has(id)) {
    ctx.visited.add(id);
    return true;
  }
  ctx.issues.push({ path: nodePath(id), code: FLOW_ISSUE.unstructuredMerge });
  return false;
}

function writeCondition(ctx: Lowering, node: FlowNode): Condition {
  const write = CONDITION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return { type: node.type };
  }
  const resolved = str(node.config.userId) !== '' || awaitingUser(node.config);
  if (node.type === 'cond.user' && !resolved) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.userNotResolved });
  }
  return owned(ctx, node, write(loweredConfig(ctx, node)));
}

function owned<T extends object>(ctx: Lowering, node: FlowNode, written: T): T {
  ctx.owners.set(written, node.id);
  return written;
}

function writeAction(ctx: Lowering, node: FlowNode): Action {
  const write = ACTION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return { type: node.type };
  }
  const action = write(loweredConfig(ctx, node));
  if (action.type === 'delay' && num(action.params?.ms) > MAX_DELAY_MS) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.waitTooLong });
  }
  return owned(ctx, node, node.config.disabled === true ? { ...action, enabled: false } : action);
}

const NESTED_PARAMS = ['then', 'else', 'on_ok', 'on_failure', 'actions'] as const;

type Arm = { key: string; list: Action[] };

function armsOf(action: Action): Arm[] {
  const params = action.params ?? {};
  const named = NESTED_PARAMS.map((key): Arm => ({ key, list: (params[key] ?? []) as Action[] }));
  const branches = (params.branches ?? []) as { actions?: Action[] }[];
  return [...named, ...branches.map((branch, b): Arm => ({ key: `branches[${b}].actions`, list: branch.actions ?? [] }))];
}

const nestedLists = (action: Action): Action[][] => armsOf(action).map((arm) => arm.list);

const waits = (list: Action[]): boolean => list.some((a) => a.type === 'delay' || nestedLists(a).some(waits));

const forkPauses = (paths: Action[][]) => paths.length > 1 && paths.some(waits);

function fork(paths: Action[][]): Action[] {
  if (paths.length < 2) return paths[0] ?? [];
  return [{ type: 'parallel', params: { branches: paths.map((actions) => ({ weight: 1, actions })) } }];
}

function lowerRest(ctx: Lowering, joins: string[], stop: Stops): Action[] {
  const open = joins.filter((id) => !stop.has(id));
  return open.length > 0 ? lowerEntries(ctx, open, stop) : [];
}

function lowerEntries(ctx: Lowering, entries: string[], stop: Stops): Action[] {
  entries = entries.filter((id) => !stop.has(id));
  if (entries.length === 1) return lowerFrom(ctx, entries[0]!, stop);
  const joins = commonJoins(ctx, entries);
  const bound = extended(stop, joins);
  const paths = entries.map((id) => lowerFrom(ctx, id, bound)).filter((path) => path.length > 0);
  const anchor = joins[0] ?? [...stop][0];
  if (anchor && forkPauses(paths)) ctx.issues.push({ path: nodePath(anchor), code: FLOW_ISSUE.unstructuredMerge });
  return [...fork(paths), ...lowerRest(ctx, joins, stop)];
}

function lowerFrom(ctx: Lowering, id: string, stop: Stops): Action[] {
  const actions: Action[] = [];
  let entries = [id];
  while (entries.length === 1) {
    const current = entries[0]!;
    if (stop.has(current) || !visit(ctx, current)) return actions;
    const node = ctx.byId.get(current)!;
    const control = controlActions(ctx, node, stop);
    if (control) return actions.concat(control);
    actions.push(writeAction(ctx, node));
    entries = targets(ctx, current, 'next');
  }
  return actions.concat(lowerEntries(ctx, entries, stop));
}

function controlActions(ctx: Lowering, node: FlowNode, stop: Stops): Action[] | null {
  if (isConditionType(node.type)) return lowerBranch(ctx, node, stop);
  if (node.type === LOOP_TYPE) return lowerLoop(ctx, node, stop);
  return Object.hasOwn(API_WRITERS, node.type) ? lowerApi(ctx, node, stop) : null;
}

type Arms = { first: Action[]; second: Action[]; rest: Action[] };

function lowerArms(ctx: Lowering, node: FlowNode, ports: readonly [string, string], stop: Stops): Arms {
  const first = targets(ctx, node.id, ports[0]);
  const second = targets(ctx, node.id, ports[1]);
  const joins = armJoins(ctx, first, second);
  const bound = extended(stop, joins);
  return { first: lowerEntries(ctx, first, bound), second: lowerEntries(ctx, second, bound), rest: lowerRest(ctx, joins, stop) };
}

function lowerBranch(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const params: Params = { condition: writeCondition(ctx, node) };
  const { first, second, rest } = lowerArms(ctx, node, ['yes', 'no'], stop);
  if (first.length > 0) params.then = first;
  if (second.length > 0) params.else = second;
  return [owned(ctx, node, { type: 'if', params }), ...rest];
}

const withEnabled = (node: FlowNode, action: Action): Action => (node.config.disabled === true ? { ...action, enabled: false } : action);

function lowerLoop(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const times = str(loweredConfig(ctx, node).times);
  const { first, second, rest } = lowerArms(ctx, node, LOOP_PORTS, stop);
  const params: Params = first.length > 0 ? { times, actions: first } : { times };
  return [owned(ctx, node, withEnabled(node, { type: 'loop', params })), ...second, ...rest];
}

function lowerApi(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const action = API_WRITERS[node.type]!(loweredConfig(ctx, node));
  const { first, second, rest } = lowerArms(ctx, node, API_PORTS, stop);
  const params = { ...action.params };
  if (first.length > 0) params.on_ok = first;
  if (second.length > 0) params.on_failure = second;
  return [owned(ctx, node, withEnabled(node, { ...action, params })), ...rest];
}

function leadingGate(ctx: Lowering, entries: string[]): FlowNode | null {
  if (entries.length !== 1) return null;
  const node = ctx.byId.get(entries[0]!)!;
  const gated = isConditionType(node.type) && targets(ctx, node.id, 'no').length === 0;
  return gated ? node : null;
}

function lowerRoot(ctx: Lowering, trigger: FlowNode): { conditions: Condition[]; actions: Action[] } {
  const conditions: Condition[] = [];
  let entries = targets(ctx, trigger.id, 'next');
  for (let gate = leadingGate(ctx, entries); gate; gate = leadingGate(ctx, entries)) {
    visit(ctx, gate.id);
    conditions.push(writeCondition(ctx, gate));
    entries = targets(ctx, gate.id, 'yes');
  }
  return { conditions, actions: lowerEntries(ctx, entries, NO_STOPS) };
}

function assembled(flow: Flow, trigger: Trigger, conditions: Condition[], actions: Action[]): Definition {
  const definition: Definition = { version: VERSION, trigger };
  const queue = recipeName(flow.queue);
  if (queue) definition.queue = queue;
  if (conditions.length > 0) definition.conditions = conditions;
  if (actions.length > 0) definition.actions = actions;
  return definition;
}

type Lowered = { result: FlowResult<Definition>; owners: Map<object, string> };

function lowerFlow(flow: Flow): Lowered {
  const owners = new Map<object, string>();
  const found = triggerNode(flow);
  if (!found.ok) return { result: found, owners };
  const trigger = found.value;
  const written = TRIGGER_WRITERS[trigger.type]?.(trigger.config);
  const ctx: Lowering = {
    trigger: written ?? { type: '' },
    byId: new Map(flow.nodes.map((n) => [n.id, n])),
    outs: outgoing(flow),
    counters: new Map(),
    visited: new Set([trigger.id]),
    issues: [],
    owners
  };
  const shape = shapeIssues(flow, trigger, ctx);
  if (shape.length > 0) return { result: { ok: false, issues: shape }, owners };
  if (!written) return { result: { ok: false, issues: [{ path: nodePath(trigger.id), code: FLOW_ISSUE.unsupportedNode }] }, owners };
  ctx.counters = priorCounters(ctx);
  const { conditions, actions } = lowerRoot(ctx, trigger);
  if (ctx.issues.length > 0) return { result: { ok: false, issues: ctx.issues }, owners };
  return { result: { ok: true, value: assembled(flow, written, conditions, actions) }, owners };
}

export function flowToDefinition(flow: Flow): FlowResult<Definition> {
  return lowerFlow(flow).result;
}

export function compileFlow(flow: Flow): FlowResult<{ definition: Definition; nodeByPath: ReadonlyMap<string, string> }> {
  const { result, owners } = lowerFlow(flow);
  if (!result.ok) return result;
  const nodeByPath = stepPaths(result.value, owners).byPath;
  const trigger = flow.nodes.find((node) => isTriggerType(node.type))!;
  nodeByPath.set('trigger', trigger.id);
  return { ok: true, value: { definition: result.value, nodeByPath } };
}

type StepPaths = { byPath: Map<string, string>; lastDelay: string | null };

function walkSteps(owners: Map<object, string>, list: Action[], base: string, into: StepPaths): void {
  list.forEach((action, i) => {
    const path = `${base}[${i}]`;
    const id = owners.get(action);
    if (id) into.byPath.set(path, id);
    if (id && action.type === 'delay') into.lastDelay = id;
    const gate = action.type === 'if' ? owners.get(action.params?.condition as object) : undefined;
    if (gate) into.byPath.set(`${path}.params.condition`, gate);
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
  const into: StepPaths = { byPath: new Map(), lastDelay: null };
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

type Tail = { from: string; port: string };

type Building = { nodes: FlowNode[]; edges: FlowEdge[]; issues: Issue[] };

function addNode(b: Building, type: string, config: FlowConfig, tails: Tail[]): string {
  const id = `n${b.nodes.length + 1}`;
  b.nodes.push({ id, type, x: 0, y: 0, config });
  for (const tail of tails) b.edges.push({ id: `e${b.edges.length + 1}`, from: tail.from, port: tail.port, to: id });
  return id;
}

function eventNode(params: Params): { type: string; config: FlowConfig } | null {
  const kind = str(params.kind);
  const filters = (params.filters ?? {}) as Record<string, string>;
  const fixed = Object.keys(FIXED_EVENT_NODES).find((type) => FIXED_EVENT_NODES[type] === kind);
  if (fixed) return fixedEventNode(fixed, filters);
  const threshold = THRESHOLD_FILTERS[kind];
  if (threshold) return thresholdNode(threshold, filters);
  return subNode(kind, filters);
}

function adBreakConfig(filters: Record<string, string>): FlowConfig | null {
  if (!onlyFilters(filters, ['automatic', 'seconds'])) return null;
  const startedBy = AD_STARTED_BY_OPTIONS.get(filters.automatic?.toLowerCase());
  if (!startedBy) return null;
  return { startedBy, ...readMinimums(filters, AD_MINIMUMS) };
}

function channelUpdateConfig(filters: Record<string, string>): FlowConfig | null {
  if (!onlyFilters(filters, ['category'])) return null;
  return filters.category === undefined ? {} : { category: filters.category };
}

const FIXED_EVENT_CONFIGS: Record<string, (filters: Record<string, string>) => FlowConfig | null> = {
  'twitch.ad_break': adBreakConfig,
  'twitch.channel_update': channelUpdateConfig
};

function fixedEventNode(type: FlowNode['type'], filters: Record<string, string>) {
  const read = FIXED_EVENT_CONFIGS[type];
  const config = read ? read(filters) : onlyFilters(filters, []) ? {} : null;
  return config === null ? null : { type, config };
}

function readMinimums(filters: Record<string, string>, fields: Record<string, string>): FlowConfig {
  return Object.fromEntries(Object.entries(fields)
    .filter(([filter]) => Object.hasOwn(filters, filter))
    .map(([filter, key]) => [key, Number(filters[filter])]));
}

function onlyFilters(filters: Record<string, string>, allowed: readonly string[]): boolean {
  return Object.keys(filters).every((key) => allowed.includes(key));
}

function thresholdNode(threshold: ThresholdSpec, filters: Record<string, string>) {
  const allowed = [threshold.filter];
  if (threshold.actor) allowed.push('anonymous');
  if (threshold.tier) allowed.push('tier');
  if (!onlyFilters(filters, allowed)) return null;
  const actor = ANONYMOUS_OPTIONS.get(filters.anonymous);
  if (!actor) return null;
  const config = readMinimums(filters, { [threshold.filter]: threshold.key });
  if (threshold.actor) config[threshold.actor] = actor;
  if (threshold.tier) config.tier = filters.tier ?? 'any';
  return { type: threshold.type, config };
}

function subNode(kind: string, filters: Record<string, string>): { type: string; config: FlowConfig } | null {
  if (kind !== SUB_KINDS.any && kind !== SUB_KINDS.resub) return null;
  const resub = kind === SUB_KINDS.resub;
  const allowed = resub ? ['tier', ...Object.keys(SUB_MONTHS)] : ['tier', 'is_gift'];
  if (!onlyFilters(filters, allowed)) return null;
  const variant = resub ? 'resub' : SUB_VARIANTS.get(filters.is_gift);
  if (!variant) return null;
  const config: FlowConfig = { variant, tier: filters.tier ?? 'any', ...readMinimums(filters, SUB_MONTHS) };
  return { type: 'twitch.sub', config };
}

function literalWord(pattern: string): string | undefined {
  if (!pattern.startsWith(WORD_START) || !pattern.endsWith(WORD_END)) return undefined;
  const word = unescapeWord(pattern.slice(WORD_START.length, pattern.length - WORD_END.length));
  return wordPattern(word) === pattern ? word : undefined;
}

function readChatSettings(params: Params): FlowConfig {
  const sources = (params.sources ?? []) as string[];
  const subMessages = sources.includes('sub_message');
  const config: FlowConfig = { caseSensitive: params.case_sensitive === true, subMessages };
  const imported = sources.join(',');
  if (subMessages && imported !== CHAT_SOURCES) config.importedSources = imported;
  return config;
}

function readChatAliases(params: Params, config: FlowConfig): void {
  const aliases = (params.aliases ?? []) as string[];
  if (aliases.length === 0) return;
  config.aliases = aliases.join(', ');
  const parsed = aliasList(config.aliases);
  const authored = params.match === 'anywhere' ? parsed : parsed.map(bangName);
  if (authored.join('\n') !== aliases.join('\n')) config.importedAliases = aliases.join('\n');
}

function wordNode(params: Params, config: FlowConfig): { type: string; config: FlowConfig } {
  const name = str(params.name);
  const word = params.match === 'regex' ? literalWord(name) : undefined;
  config.word = word ?? name;
  const mode = params.match === 'anywhere' ? 'anywhere' : 'pattern';
  config.match = word === undefined ? mode : 'word';
  if (word !== undefined && word !== word.trim()) config.importedWord = word;
  return { type: 'twitch.chat.word', config };
}

function commandNode(params: Params): { type: string; config: FlowConfig } | null {
  if (Object.keys(params).some((key) => !COMMAND_KEYS.has(key))) return null;
  const config = readChatSettings(params);
  readChatAliases(params, config);
  if (params.match === 'regex' || params.match === 'anywhere') return wordNode(params, config);
  const match = params.match ?? 'start';
  if (match !== 'start' && match !== 'exact') return null;
  const name = str(params.name);
  config.trigger = name;
  if (name !== bangName(name.trim())) config.importedName = name;
  config.match = match;
  return { type: 'twitch.chat.command', config };
}

function rewardNode(params: Params): { type: string; config: FlowConfig } {
  const id = str(params.reward_id);
  const title = str(params.title);
  if (title === '') return { type: 'twitch.reward', config: { reward: id } };
  const reward = id === SETUP_REWARD_ID ? '' : id;
  const config: FlowConfig = {
    reward,
    title,
    cost: num(params.cost),
    color: str(params.color),
    cooldown: num(params.cooldown_s),
    onRedeem: str(params.on_redeem) || 'leave',
    userInput: params.input_required === true
  };
  return { type: 'twitch.reward', config };
}

function timerNode(params: Params): { type: string; config: FlowConfig } {
  return {
    type: 'schedule.timer',
    config: { every: num(params.interval_s), jitter: num(params.random_jitter_s), chatLines: num(params.min_chat_lines), offline: params.offline_ok === true }
  };
}

const TRIGGER_READERS = new Map<Trigger['type'], (params: Params) => Pick<FlowNode, 'type' | 'config'> | null>([
  ['countdown', (params) => ({ type: 'countdown.finished', config: { name: str(params.name) } })],
  ['command', commandNode],
  ['event', eventNode],
  ['reward', rewardNode],
  ['timer', timerNode],
  ['manual', () => ({ type: 'schedule.manual', config: {} })]
]);

function triggerToNode(trigger: Trigger): Pick<FlowNode, 'type' | 'config'> | null {
  return TRIGGER_READERS.get(trigger.type)?.(trigger.params ?? {}) ?? null;
}

function cooldownNode(params: Params): { type: string; config: FlowConfig } | null {
  const global = num(params.global_s);
  const user = num(params.user_s);
  if (global > 0 && user > 0) return null;
  return { type: 'cond.cooldown', config: { seconds: global || user, perUser: user > 0, ...timeLeftConfig(params) } };
}

function userNode(params: Params): { type: string; config: FlowConfig } | null {
  const ids = (params.allow_ids ?? []) as string[];
  const others = [params.allow, params.deny, params.deny_ids].flatMap((list) => (list ?? []) as string[]);
  const everyone = [...ids, ...others];
  if (everyone.length === 1 && (params.allow as string[] | undefined)?.[0] === SETUP_LOGIN) return { type: 'cond.user', config: { login: SETUP_LOGIN } };
  return ids.length === 1 && others.length === 0 ? { type: 'cond.user', config: { userId: ids[0]! } } : null;
}

function compareNode(params: Params): { type: string; config: FlowConfig } | null {
  if (!COMPARE_OPS.includes(str(params.op))) return null;
  return { type: 'cond.compare', config: { left: str(params.left), op: str(params.op), right: str(params.right) } };
}

const varConfig = (params: Params): FlowConfig => ({ scope: str(params.scope), name: str(params.name) });

function varConditionNode(params: Params): { type: string; config: FlowConfig } {
  const op = str(params.op);
  const config: FlowConfig = { ...varConfig(params), op };
  return { type: 'cond.var', config: op === 'exists' ? config : { ...config, value: str(params.value) } };
}

function conditionToNode(condition: Condition): { type: string; config: FlowConfig } | null {
  const params = condition.params ?? {};
  if (condition.type === 'var') return varConditionNode(params);
  if (condition.type === 'compare') return compareNode(params);
  if (condition.type === 'users') return userNode(params);
  if (condition.type === 'perm') return { type: 'cond.role', config: { level: str(params.level) } };
  if (condition.type === 'chance') return { type: 'cond.chance', config: { percent: num(params.percent) } };
  if (condition.type === 'cooldown') return cooldownNode(params);
  return null;
}

function lightsNode(params: Params): { type: string; config: FlowConfig } | null {
  const config: FlowConfig = { device: str(params.device), sku: str(params.sku) };
  if (str(params.device_name)) config.deviceName = str(params.device_name);
  const extra = (params.extra_lights ?? []) as string[];
  if (extra.length) config.extraLights = extra.join(',');
  config.liveOnly = params.any_time !== true;
  const mode = str(params.mode) || 'color';
  if (mode === 'color') return { type: 'reply.lights', config: { ...config, mode, color: str(params.color) } };
  if (mode !== 'scene') return null;
  const scene: FlowConfig = {
    scene: str(params.scene_id),
    sceneType: str(params.scene_type),
    sceneInstance: str(params.scene_instance),
    sceneValue: str(params.scene_value)
  };
  if (str(params.scene_name)) scene.sceneName = str(params.scene_name);
  return { type: 'reply.lights', config: { ...config, mode, ...scene } };
}

function textNode(params: Params): { type: string; config: FlowConfig } {
  const op = str(params.op);
  const config: FlowConfig = { op, input: str(params.input), saveAs: str(params.save_as) };
  const [first, second] = TEXT_ARG_FIELDS.get(op) ?? [];
  if (first) config[first] = str(params.arg1);
  if (second) config[second] = str(params.arg2);
  return { type: 'reply.text', config };
}

function rewardEnabledChoice(enabled: unknown): string {
  if (enabled === true) return 'on';
  return enabled === false ? 'off' : 'keep';
}

function rewardUpdateNode(params: Params): { type: string; config: FlowConfig } {
  const config: FlowConfig = { reward: str(params.reward_id), setEnabled: rewardEnabledChoice(params.enabled), reset: params.reset_on_stream_end === true };
  return { type: 'reply.reward_update', config: typeof params.cost === 'number' ? { ...config, cost: params.cost } : config };
}

const API_READERS: Record<string, (params: Params) => { type: string; config: FlowConfig }> = {
  http_get: (params) => ({
    type: 'reply.http_get',
    config: { url: str(params.url), jsonPath: ((params.json_path ?? []) as string[]).join(JSON_PATH_SEPARATOR), saveAs: str(params.save_as) }
  }),
  http_post: (params) => ({ type: 'reply.http_post', config: { url: str(params.url), body: str(params.body), saveAs: str(params.save_as) } })
};

const ACTION_READERS: Record<string, (params: Params) => { type: string; config: FlowConfig } | null> = {
  ...streamActionReaders,
  chat: (params) => ({ type: 'reply.chat', config: { message: str(params.text) } }),
  lights: lightsNode,
  counter_add: (params) => (num(params.amount) === 1 ? { type: 'reply.counter', config: { counter: str(params.name) } } : null),
  delay: (params) => ({ type: 'reply.wait', config: { seconds: num(params.ms) / 1000 } }),
  var_set: (params) => ({ type: 'reply.var_set', config: { ...varConfig(params), value: str(params.value) } }),
  var_add: (params) => ({ type: 'reply.var_add', config: { ...varConfig(params), amount: num(params.amount) } }),
  var_delete: (params) => ({ type: 'reply.var_delete', config: varConfig(params) }),
  text: textNode,
  math: (params) => ({ type: 'reply.math', config: { expression: str(params.expression), saveAs: str(params.save_as) } }),
  reward_update: rewardUpdateNode,
  countdown_start: (params) => ({ type: 'reply.countdown_start', config: { ...timeLeftConfig(params), name: str(params.name), seconds: num(params.seconds) } }),
  countdown_add: (params) => ({ ...cooldownAddNode(params), type: 'reply.countdown_add' }),
  cooldown_add: cooldownAddNode
};

function cooldownAddNode(params: Params): { type: string; config: FlowConfig } {
  const config: FlowConfig = { name: str(params.name), seconds: num(params.seconds) };
  if (num(params.per) > 0) config.per = num(params.per);
  return { type: 'reply.cooldown_add', config };
}

const placedConfig = (action: Action, config: FlowConfig): FlowConfig => (action.enabled === false ? { ...config, disabled: true } : config);

function placeGate(b: Building, condition: Condition, path: string, tails: Tail[]): string | null {
  const node = conditionToNode(condition);
  if (node) return addNode(b, node.type, node.config, tails);
  b.issues.push({ path, code: FLOW_ISSUE.unsupportedCondition });
  return null;
}

function placeIf(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const params = action.params ?? {};
  const gate = placeGate(b, params.condition as Condition, `${path}.condition`, tails);
  if (!gate) return tails;
  const yes = placeActions(b, (params.then ?? []) as Action[], `${path}.then`, [{ from: gate, port: 'yes' }]);
  const no = placeActions(b, (params.else ?? []) as Action[], `${path}.else`, [{ from: gate, port: 'no' }]);
  return [...yes, ...no];
}

function placeParallel(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const branches = (action.params?.branches ?? []) as { actions?: Action[] }[];
  return branches.flatMap((branch, i) => placeActions(b, branch.actions ?? [], `${path}.params.branches[${i}].actions`, tails));
}

function placeLoop(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const params = action.params ?? {};
  const id = addNode(b, LOOP_TYPE, placedConfig(action, { times: str(params.times) }), tails);
  placeActions(b, (params.actions ?? []) as Action[], `${path}.params.actions`, [{ from: id, port: 'body' }]);
  return [{ from: id, port: 'done' }];
}

function placeApi(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const params = action.params ?? {};
  const node = API_READERS[action.type]!(params);
  const id = addNode(b, node.type, placedConfig(action, node.config), tails);
  const worked = placeActions(b, (params.on_ok ?? []) as Action[], `${path}.params.on_ok`, [{ from: id, port: 'ok' }]);
  const failed = placeActions(b, (params.on_failure ?? []) as Action[], `${path}.params.on_failure`, [{ from: id, port: 'failure' }]);
  return [...worked, ...failed];
}

function placeAction(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  if (disabledControlFlow(action)) {
    b.issues.push({ path, code: FLOW_ISSUE.unsupportedDisabledControlFlow });
    return tails;
  }
  if (action.type === 'if') return placeIf(b, action, path, tails);
  if (action.type === 'parallel') return placeParallel(b, action, path, tails);
  if (action.type === 'loop') return placeLoop(b, action, path, tails);
  if (Object.hasOwn(API_READERS, action.type)) return placeApi(b, action, path, tails);
  const node = Object.hasOwn(ACTION_READERS, action.type) ? ACTION_READERS[action.type]!(action.params ?? {}) : null;
  if (!node) {
    b.issues.push({ path, code: FLOW_ISSUE.unsupportedAction });
    return tails;
  }
  return [{ from: addNode(b, node.type, placedConfig(action, node.config), tails), port: 'next' }];
}

function disabledControlFlow(action: Action): boolean {
  return action.enabled === false && ['if', 'parallel'].includes(action.type);
}

function placeActions(b: Building, actions: Action[], path: string, tails: Tail[]): Tail[] {
  return actions.reduce<Tail[]>((open, action, i) => placeAction(b, action, `${path}[${i}]`, open), tails);
}

function placeConditions(b: Building, conditions: Condition[], tails: Tail[]): Tail[] {
  return conditions.reduce<Tail[]>((open, condition, i) => {
    const gate = placeGate(b, condition, `conditions[${i}]`, open);
    return gate ? [{ from: gate, port: 'yes' }] : open;
  }, tails);
}

type CounterScan = { kind: string; counters: Map<string, string[]> };

function raiseTemplate(scan: CounterScan, node: FlowNode, field: string): string {
  const counters = scan.counters.get(node.id) ?? [];
  const text = eventTokensToBuilder(scan.kind, str(node.config[field]), counters.length > 0);
  if (counters.length === 1 && !EVENT_TOKENS[scan.kind]?.count) return text.replaceAll(`{counter:${counters[0]}}`, '{count}');
  return text;
}

function raiseMessages(b: Building, kind: string): void {
  const flow = { nodes: b.nodes, edges: b.edges } as Flow;
  const scan: CounterScan = { kind, counters: priorCounters({ byId: new Map(b.nodes.map((node) => [node.id, node])), outs: outgoing(flow) }) };
  for (const node of b.nodes) {
    for (const field of templateFields(node)) {
      node.config = { ...node.config, [field]: raiseTemplate(scan, node, field) };
    }
  }
}

export function definitionToFlow(definition: Definition, meta: FlowMeta): FlowResult<Flow> {
  const trigger = triggerToNode(definition.trigger);
  if (!trigger) return { ok: false, issues: [{ path: 'trigger', code: FLOW_ISSUE.unsupportedTrigger }] };
  const b: Building = { nodes: [], edges: [], issues: [] };
  const root = addNode(b, trigger.type, trigger.config, []);
  const gated = placeConditions(b, definition.conditions ?? [], [{ from: root, port: 'next' }]);
  placeActions(b, definition.actions ?? [], 'actions', gated);
  if (b.issues.length > 0) return { ok: false, issues: b.issues };
  raiseMessages(b, triggerKind(definition.trigger));
  const queue = definition.queue ? { queue: definition.queue } : {};
  return { ok: true, value: { ...meta, ...queue, nodes: b.nodes, edges: b.edges } };
}
