// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { invalidLiteralTags, streamActionReaders, streamActionWriters, streamInfoActions, streamInfoRun, streamTargetNodes, usesLegacyTarget } from './stream-flow';
import { COMPARE_OPS } from './compare';
import { ACTION_TYPES } from './catalog';
import { SETUP_LOGIN, SETUP_REWARD_ID, SETUP_RUN_TARGET } from './flow-setup';
import { CODE, MAX_DELAY_MS, VERSION } from './limits';
import { CMP_OPS } from './values';
import type { Action, Condition, Definition, Issue, Params, Trigger } from './types';
import { VAR_OPS_WITH_VALUE } from './values';

export { VAR_OPS_WITH_VALUE } from './values';

export { pointsChange } from './stream-flow';
export { CMP_OPS } from './values';

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
  userNotResolved: 'user_not_resolved',
  nothingToChange: 'nothing_to_change',
  invalidTags: 'invalid_tags',
  unknownPort: 'unknown_port'
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

type CooldownWindows = { channelSeconds: number; viewerSeconds: number };

export function cooldownWindows(config: FlowConfig): CooldownWindows {
  if (config.channelSeconds !== undefined || config.viewerSeconds !== undefined) {
    return { channelSeconds: num(config.channelSeconds), viewerSeconds: num(config.viewerSeconds) };
  }
  const seconds = num(config.seconds);
  return config.perUser ? { channelSeconds: 0, viewerSeconds: seconds } : { channelSeconds: seconds, viewerSeconds: 0 };
}

export const canNameCooldown = ({ channelSeconds, viewerSeconds }: CooldownWindows): boolean => viewerSeconds === 0 && channelSeconds > 0;

function eventTrigger(kind: string, filters?: Record<string, string>): Trigger {
  return { type: 'event', params: filters ? { kind, filters } : { kind } };
}

export const DEFAULT_REWARD_OUTCOME = 'leave';
export const CREATED_REWARD_OUTCOME = 'fulfill';

export const hasRedemptionStep = (nodes: readonly FlowNode[]): boolean =>
  nodes.some((node) => node.type === 'reply.redemption' && node.config.disabled !== true);

function rewardTrigger(config: FlowConfig): Trigger {
  const title = str(config.title).trim();
  const chosen = str(config.onRedeem);
  if (title === '') return { type: 'reward', params: chosen ? { reward_id: str(config.reward), on_redeem: chosen } : { reward_id: str(config.reward) } };
  const params = {
    reward_id: str(config.reward) || SETUP_REWARD_ID,
    title,
    cost: num(config.cost),
    color: str(config.color),
    cooldown_s: num(config.cooldown),
    on_redeem: chosen || CREATED_REWARD_OUTCOME,
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
  'reply.unban': ['target'],
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
  'reply.pin': ['text'],
  'cond.compare': ['left', 'right'],
  'reply.var': ['value'],
  'reply.text': ['input', 'arg1', 'arg2'],
  'reply.math': ['expression'],
  'reply.loop': ['times'],
  'reply.http_get': ['url'],
  'reply.http_post': ['url', 'body'],
  'reply.stream_info': ['title', 'game', 'tags'],
  'reply.marker': ['description'],
  'reply.clip': ['title', 'reply'],
  'cond.cooldown': ['leftReply', 'readyReply']
};

const recipeName = (v: unknown) => str(v).trim().toLowerCase();

const varIdentity = (config: FlowConfig): Params => ({ scope: str(config.scope) || 'channel', name: recipeName(config.name) });

export const TEXT_OPERAND_OPS = {
  arg1: ['replace', 'substring', 'split_pick', 'contains', 'count'],
  arg2: ['replace', 'substring', 'split_pick']
} satisfies Record<string, readonly string[]>;

function textFieldUsed(field: string, op: string): boolean {
  const operands: Partial<Record<string, readonly string[]>> = TEXT_OPERAND_OPS;
  return operands[field]?.includes(op) ?? true;
}

const textOp = (config: FlowConfig) => str(config.op) || 'upper';
const varConditionOp = (config: FlowConfig) => str(config.op) || 'eq';

const VARIABLE_ACTION_TYPES = new Map([
  ['set', 'var_set'], ['add', 'var_add'], ['subtract', 'var_add'], ['forget', 'var_delete']
]);

export const variableActionType = (config: FlowConfig) => VARIABLE_ACTION_TYPES.get(str(config.do)) ?? 'var_set';

const TEMPLATE_FIELD_OVERRIDES = new Map<string, (config: FlowConfig, fields: readonly string[]) => readonly string[]>([
  ['reply.text', (config, fields) => fields.filter((field) => textFieldUsed(field, textOp(config)))],
  ['reply.var', (config, fields) => variableActionType(config) === 'var_set' ? fields : []],
  ['cond.var', (config) => VAR_OPS_WITH_VALUE.includes(varConditionOp(config)) ? ['value'] : []],
  ['reply.prediction_end', (config) => config.status === 'resolve' ? ['outcome'] : []],
  ['cond.cooldown', (config, fields) => fields.filter((field) => config[field] !== undefined)]
]);

function templateFields(node: FlowNode): readonly string[] {
  const fields = TEMPLATE_FIELDS[node.type] ?? [];
  const override = TEMPLATE_FIELD_OVERRIDES.get(node.type);
  return override ? override(node.config, fields) : targetTemplateFields(node, fields);
}

function targetTemplateFields(node: FlowNode, fields: readonly string[]): readonly string[] {
  if (!streamTargetNodes.has(node.type)) return fields;
  const legacy = usesLegacyTarget(node.config.who);
  const active = (field: string): boolean => field !== 'target' || legacy;
  return fields.filter((field) => node.config[field] !== undefined && active(field));
}

function textAction(config: FlowConfig): Action {
  const op = textOp(config);
  const operand = (key: keyof typeof TEXT_OPERAND_OPS) => (textFieldUsed(key, op) ? str(config[key]) : '');
  return { type: 'text', params: { op, input: str(config.input), arg1: operand('arg1'), arg2: operand('arg2'), save_as: recipeName(config.saveAs) } };
}

function variableAction(config: FlowConfig): Action {
  const params = varIdentity(config);
  const type = variableActionType(config);
  if (type === 'var_delete') return { type, params };
  if (type === 'var_set') return { type, params: { ...params, value: str(config.value) } };
  const amount = num(config.amount) * (config.do === 'subtract' ? -1 : 1);
  return { type, params: { ...params, amount } };
}

const REWARD_ENABLED = new Map<string, boolean | null>([
  ['keep', null],
  ['on', true],
  ['off', false]
]);

const REWARD_PAUSED = new Map<string, boolean>([['pause', true], ['resume', false]]);

function rewardUpdateAction(config: FlowConfig): Action {
  const cost = String(config.cost ?? '').trim() === '' ? null : num(config.cost);
  const enabled = REWARD_ENABLED.get(str(config.setEnabled)) ?? null;
  const params: Params = { reward_id: str(config.reward), enabled, cost, reset_on_stream_end: config.reset === true };
  const paused = REWARD_PAUSED.get(str(config.paused));
  if (paused !== undefined) params.paused = paused;
  if (String(config.cooldown ?? '').trim() !== '') params.cooldown_s = num(config.cooldown);
  return { type: 'reward_update', params };
}

function withOptional(params: Params, optional: Params): Params {
  for (const [key, value] of Object.entries(optional)) if (value) params[key] = value;
  return params;
}

const JSON_PATH_SEPARATOR = '.';
const JSON_PATH_BRACKET = /\[(\d+|"[A-Za-z0-9_-]+"|'[A-Za-z0-9_-]+')\](?=\.|\[|$)/g;

function jsonPathOf(config: FlowConfig): string[] | undefined {
  const path = str(config.jsonPath).trim().replace(JSON_PATH_BRACKET, (_, segment: string) =>
    `${JSON_PATH_SEPARATOR}${segment.replace(/['"]/g, '')}`);
  const segments = path.split(JSON_PATH_SEPARATOR).filter(Boolean);
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
  }),
  'reply.points_spend': (config) => ({ type: 'points_spend', params: { amount: num(config.amount) } })
};

const API_PORTS: Readonly<Record<string, readonly [string, string]>> = {
  http_get: ['ok', 'failure'],
  http_post: ['ok', 'failure'],
  points_spend: ['enough', 'notEnough']
};

export const apiPorts = (type: Action['type']): readonly [string, string] => API_PORTS[type]!;

const LOOP_TYPE = 'reply.loop';
const LOOP_PORTS = ['body', 'done'] as const;
const RANDOM_TYPE = 'reply.random';
const randomBranchCount = ACTION_TYPES.find((entry) => entry.type === 'random_pick')!.fields.find((field) => field.name === 'branches')!;
export const RANDOM_PATHS = Array.from({ length: randomBranchCount.max! }, (_, i) => ({
  id: `p${i + 1}`, path: i + 1, weightKey: `weight${i + 1}`
}));
export const RANDOM_PATH_COUNTS = RANDOM_PATHS.slice(randomBranchCount.min! - 1).map(({ path }) => String(path));
export const RANDOM_PATH_DEFAULT_WEIGHT = 1;
export const activeRandomPaths = (config: FlowConfig) =>
  RANDOM_PATHS.slice(0, Number(RANDOM_PATH_COUNTS.find((paths) => paths === String(config.paths)) ?? RANDOM_PATH_COUNTS[0]));
export const randomPathWeight = (config: FlowConfig, key: keyof FlowConfig) => num(config[key] ?? RANDOM_PATH_DEFAULT_WEIGHT);
const CHECK_PORTS = ['yes', 'no'] as const;

function outputPorts(node: FlowNode): readonly string[] {
  if (isConditionType(node.type)) return CHECK_PORTS;
  if (node.type === LOOP_TYPE) return LOOP_PORTS;
  return Object.hasOwn(API_WRITERS, node.type) ? API_PORTS : ['next'];
}

const DEFAULT_LIGHT_COLOR = '#ffffff';
const DEFAULT_LIGHT_KELVIN = 4000;
const LIGHT_OPTION_FIELDS = [
  ['brightness', 'brightness'],
  ['hold_s', 'holdS']
] as const;

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

const LIGHT_MODE_WRITERS = new Map<string, (config: FlowConfig) => Params>([
  ['color', (config) => ({ color: str(config.color) || DEFAULT_LIGHT_COLOR })],
  ['scene', sceneParams],
  ['white', ({ kelvin }) => ({ kelvin: String(kelvin ?? '').trim() === '' ? DEFAULT_LIGHT_KELVIN : num(kelvin) })],
  ['on', () => ({})],
  ['off', () => ({})]
]);

function lightsAction(config: FlowConfig): Action {
  const mode = typeof config.mode === 'string' ? config.mode : 'color';
  const params = { ...lightParams(config), mode, ...LIGHT_MODE_WRITERS.get(mode)?.(config) };
  const brightness = num(config.brightness);
  const hold = num(config.holdS);
  const useBrightness = mode !== 'off' && Number.isFinite(brightness) && brightness !== 0;
  return { type: 'lights', params: withOptional(params, { brightness: useBrightness && brightness, hold_s: hold > 0 && hold }) };
}

const awaitingUser = (config: FlowConfig) => config.users === undefined && config.login === SETUP_LOGIN;

const SETUP_RUN_TEXT = '[setup-required:run]';
export const setupRunPlaceholder = (): Action => ({ type: 'chat', enabled: false, params: { text: SETUP_RUN_TEXT } });
export const isSetupRunPlaceholder = (action: Action): boolean =>
  action.type === 'chat' && action.enabled === false && action.params?.text === SETUP_RUN_TEXT;

function runAction(config: FlowConfig): Action {
  const id = str(config.automationId);
  return id === SETUP_RUN_TARGET ? setupRunPlaceholder() : { type: 'run', params: { automation_id: id } };
}

export const flowList = (value: FlowConfig[string] | undefined): string[] => str(value).split(',').map((entry) => entry.trim()).filter(Boolean);

type ViewerEntry = { id: string; login: string };

export const viewerIdKey = (id: ViewerEntry['id']): string => id.replace(/^0+(?=\d)/, '');

export function viewerList(config: FlowConfig): ViewerEntry[] {
  if (awaitingUser(config)) return [];
  return flowList(config.users ?? `${String(config.userId ?? '')}:${str(config.login)}`).filter((entry) => entry !== ':').map((entry) => {
    const [id = '', login = ''] = entry.split(':', 2);
    return { id, login };
  });
}

function userCondition(config: FlowConfig): Condition {
  if (awaitingUser(config)) return { type: 'users', params: { allow: [SETUP_LOGIN] } };
  const ids = viewerList(config).map(({ id }) => id);
  return { type: 'users', params: { [config.mode === 'except' ? 'deny_ids' : 'allow_ids']: ids } };
}

const TIME_LEFT_PARAMS = [
  ['leftCommand', 'left_command'],
  ['leftReply', 'left_reply'],
  ['readyReply', 'ready_reply']
] as const;

function timeLeftParams(config: FlowConfig): Params {
  return Object.fromEntries(TIME_LEFT_PARAMS.filter(([key]) => str(config[key]).trim() !== '').map(([key, param]) => [param, str(config[key]).trim()]));
}

function timeLeftConfig(params: Params): FlowConfig {
  return Object.fromEntries(TIME_LEFT_PARAMS.filter(([, param]) => str(params[param]) !== '').map(([key, param]) => [key, str(params[param])]));
}

const CONDITION_WRITERS: Record<string, (config: FlowConfig) => Condition> = {
  'cond.compare': (config) => ({
    type: 'compare',
    params: {
      left: str(config.left), op: str(config.op) || 'gte', right: str(config.right),
      ...(config.matchCase === false ? { ignore_case: true } : {})
    }
  }),
  'cond.role': (config) => ({ type: 'perm', params: { level: str(config.level) || 'everyone' } }),
  'cond.chance': (config) => ({ type: 'chance', params: { percent: num(config.percent) } }),
  'cond.var': (config) => {
    const op = varConditionOp(config);
    const params: Params = { ...varIdentity(config), op };
    if (VAR_OPS_WITH_VALUE.includes(op)) params.value = str(config.value);
    return { type: 'var', params };
  },
  'cond.user': userCondition,
  'cond.cooldown': cooldownConditionAction,
  'cond.live': (config) => ({ type: 'live', params: { state: str(config.state) || 'live' } }),
  'cond.category': (config) => ({ type: 'category', params: { [config.mode === 'deny' ? 'deny' : 'allow']: flowList(config.games) } }),
  'cond.pattern': (config) => ({ type: 'arg_regex', params: { pattern: str(config.pattern) } }),
  'cond.follow': (config) => ({ type: 'follow', params: { min_days: num(config.minDays) } }),
  'cond.accountAge': (config) => ({ type: 'account_age', params: { min_days: num(config.minDays ?? 7) } }),
  'cond.points': (config) => ({ type: 'points', params: loyaltyComparison(config) }),
  'cond.counter': (config) => ({ type: 'counter', params: { name: str(config.counter), ...loyaltyComparison(config) } })
};

function loyaltyComparison(config: FlowConfig): Params {
  return { op: str(config.op) || 'gte', value: num(config.value) };
}

function cooldownConditionAction(config: FlowConfig): Condition {
  const windows = cooldownWindows(config);
  const { channelSeconds, viewerSeconds } = windows;
  const name = canNameCooldown(windows) ? str(config.name).trim() : '';
  const params = withOptional(timeLeftParams(config), { global_s: channelSeconds, user_s: viewerSeconds, name });
  return { type: 'cooldown', params };
}

function chatAction(config: FlowConfig): Action {
  const text = str(config.message);
  if (config.reply === 'thread') return { type: 'reply', params: { text, thread: true } };
  return { type: config.reply === 'mention' ? 'reply' : 'chat', params: { text } };
}

const ACTION_WRITERS: Record<string, (config: FlowConfig) => Action | Action[]> = {
  ...streamActionWriters,
  'reply.chat': chatAction,
  'reply.reply': (config) => ({ type: 'reply', params: { text: str(config.text) } }),
  'reply.lights': lightsAction,
  'reply.counter': counterAction,
  'reply.wait': (config) => ({ type: 'delay', params: { ms: Math.round(num(config.seconds) * 1000) } }),
  'reply.var': variableAction,
  'reply.text': textAction,
  'reply.math': (config) => ({ type: 'math', params: { expression: str(config.expression), save_as: recipeName(config.saveAs) } }),
  'reply.reward_update': rewardUpdateAction,
  'reply.countdown_start': (config) => ({ type: 'countdown_start', params: { ...timeLeftParams(config), name: recipeName(config.name), seconds: num(config.seconds) } }),
  'reply.countdown_add': (config) => ({ ...cooldownAddAction(config), type: 'countdown_add' }),
  'reply.cooldown_add': cooldownAddAction,
  'reply.stream_info': streamInfoActions,
  'reply.stop': () => ({ type: 'stop' }),
  'reply.run': runAction,
  'reply.redemption': (config) => ({ type: 'redemption', params: { status: str(config.result) || 'fulfill' } }),
  'reply.countdown_stop': (config) => namedTimerAction('countdown_stop', config),
  'reply.cooldown_reset': (config) => namedTimerAction('cooldown_reset', config)
};

type NamedTimerActionType = 'countdown_stop' | 'cooldown_reset';

function namedTimerAction(type: NamedTimerActionType, config: FlowConfig): Action {
  return { type, params: { name: recipeName(config.name) } };
}

function counterAction(config: FlowConfig): Action {
  const name = str(config.counter);
  if (config.change === 'set') return { type: 'counter_set', params: { name, value: num(config.value) } };
  const amount = config.change === undefined ? num(config.amount ?? 1) : num(config.amount);
  return { type: 'counter_add', params: { name, amount } };
}

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

function amountUnit(trigger: Trigger): 'bits' | 'gifts' | undefined {
  const tokens = EVENT_TOKENS[triggerKind(trigger)];
  if (tokens?.bits === 'amount') return 'bits';
  if (tokens?.count === 'amount') return 'gifts';
}

export function triggerAmountUnit(node?: Pick<FlowNode, 'type' | 'config'>): ReturnType<typeof amountUnit> {
  const trigger = node && TRIGGER_WRITERS[node.type]?.(node.config);
  return trigger ? amountUnit(trigger) : undefined;
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

function unknownPortIssues(flow: Flow, byId: Lowering['byId']): Issue[] {
  const sources = new Set<string>();
  for (const edge of flow.edges) {
    const source = byId.get(edge.from);
    if (source && !outputPorts(source).includes(edge.port)) sources.add(source.id);
  }
  return [...sources].map((id) => ({ path: nodePath(id), code: FLOW_ISSUE.unknownPort }));
}

function shapeIssues(flow: Flow, trigger: FlowNode, ctx: Lowering): Issue[] {
  const ports = unknownPortIssues(flow, ctx.byId);
  if (ports.length > 0) return ports;
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

function userListResolved(config: FlowConfig): boolean {
  if (awaitingUser(config)) return true;
  const viewers = viewerList(config);
  return viewers.length > 0 && viewers.every(({ id }) => id !== '');
}

function writeCondition(ctx: Lowering, node: FlowNode): Condition {
  const write = CONDITION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return { type: node.type };
  }
  if (node.type === 'cond.user' && !userListResolved(node.config)) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.userNotResolved });
    return { type: 'users' };
  }
  return owned(ctx, node, write(loweredConfig(ctx, node)));
}

function owned<T extends object>(ctx: Lowering, node: FlowNode, written: T): T {
  ctx.owners.set(written, node.id);
  return written;
}

function rangeIssue(ctx: Lowering, node: FlowNode, field: 'amount' | 'per'): void {
  ctx.issues.push({ path: `${nodePath(node.id)}.config.${field}`, code: CODE.outOfRange });
}

function perBelowMinimum(config: FlowConfig): boolean {
  return String(config.per ?? '').trim() !== '' && num(config.per) < 1;
}

function actionConfig(ctx: Lowering, node: FlowNode): FlowConfig {
  const config = loweredConfig(ctx, node);
  switch (node.type) {
    case 'reply.points':
      if (config.change && num(config.amount) < 0) rangeIssue(ctx, node, 'amount');
      return config;
    case 'reply.countdown_add':
    case 'reply.cooldown_add':
      if (perBelowMinimum(config)) rangeIssue(ctx, node, 'per');
      return amountUnit(ctx.trigger) ? config : { ...config, per: '' };
    default:
      return config;
  }
}

function writeAction(ctx: Lowering, node: FlowNode): Action[] {
  const write = ACTION_WRITERS[node.type];
  if (!write) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.unsupportedNode });
    return [{ type: node.type }];
  }
  const written = write(actionConfig(ctx, node));
  const actions = Array.isArray(written) ? written : [written];
  if (actions.length === 0) ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.nothingToChange });
  if (node.type === 'reply.stream_info' && invalidLiteralTags(node.config.tags)) {
    ctx.issues.push({ path: `${nodePath(node.id)}.tags`, code: FLOW_ISSUE.invalidTags });
  }
  return actions.map((action) => ownAction(ctx, node, action));
}

function ownAction(ctx: Lowering, node: FlowNode, action: Action): Action {
  if (action.type === 'delay' && num(action.params?.ms) > MAX_DELAY_MS) {
    ctx.issues.push({ path: nodePath(node.id), code: FLOW_ISSUE.waitTooLong });
  }
  return owned(ctx, node, withEnabled(node, action));
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

function fork(ctx: Lowering, paths: Action[][], source: FlowNode['id'] | undefined): Action[] {
  if (paths.length < 2) return paths[0] ?? [];
  const branches = paths.map((actions) => ({ weight: 1, actions }));
  if (source !== undefined) ctx.owners.set(branches, source);
  return [{ type: 'parallel', params: { branches } }];
}

function lowerRest(ctx: Lowering, joins: string[], stop: Stops): Action[] {
  const open = joins.filter((id) => !stop.has(id));
  return open.length > 0 ? lowerEntries(ctx, open, stop) : [];
}

function lowerEntries(ctx: Lowering, entries: string[], stop: Stops, source?: FlowNode['id']): Action[] {
  entries = entries.filter((id) => !stop.has(id));
  if (entries.length === 1) return lowerFrom(ctx, entries[0]!, stop);
  const joins = commonJoins(ctx, entries);
  const bound = extended(stop, joins);
  const paths = entries.map((id) => lowerFrom(ctx, id, bound)).filter((path) => path.length > 0);
  const anchor = joins[0] ?? [...stop][0];
  if (anchor && forkPauses(paths)) ctx.issues.push({ path: nodePath(anchor), code: FLOW_ISSUE.unstructuredMerge });
  return [...fork(ctx, paths, source), ...lowerRest(ctx, joins, stop)];
}

function lowerFrom(ctx: Lowering, id: string, stop: Stops): Action[] {
  const actions: Action[] = [];
  let entries = [id];
  let source = id;
  while (entries.length === 1) {
    const current = entries[0]!;
    if (stop.has(current) || !visit(ctx, current)) return actions;
    const node = ctx.byId.get(current)!;
    const control = controlActions(ctx, node, stop);
    if (control) return actions.concat(control);
    actions.push(...writeAction(ctx, node));
    source = current;
    entries = targets(ctx, current, 'next');
  }
  return actions.concat(lowerEntries(ctx, entries, stop, source));
}

const CONTROL_WRITERS: Readonly<Record<string, typeof lowerLoop>> = {
  [LOOP_TYPE]: lowerLoop,
  [RANDOM_TYPE]: lowerRandom
};

function controlActions(ctx: Lowering, node: FlowNode, stop: Stops): Action[] | null {
  if (isConditionType(node.type)) return lowerBranch(ctx, node, stop);
  if (Object.hasOwn(CONTROL_WRITERS, node.type)) return CONTROL_WRITERS[node.type]!(ctx, node, stop);
  return Object.hasOwn(API_WRITERS, node.type) ? lowerApi(ctx, node, stop) : null;
}

type Arms = { first: Action[]; second: Action[]; rest: Action[] };

function lowerArms(ctx: Lowering, node: FlowNode, ports: readonly [string, string], stop: Stops): Arms {
  const first = targets(ctx, node.id, ports[0]);
  const second = targets(ctx, node.id, ports[1]);
  const joins = armJoins(ctx, first, second);
  const bound = extended(stop, joins);
  return { first: lowerEntries(ctx, first, bound, node.id), second: lowerEntries(ctx, second, bound, node.id), rest: lowerRest(ctx, joins, stop) };
}

function lowerBranch(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const params: Params = { condition: writeCondition(ctx, node) };
  const { first, second, rest } = lowerArms(ctx, node, CHECK_PORTS, stop);
  if (first.length > 0) params.then = first;
  if (second.length > 0) params.else = second;
  return [owned(ctx, node, { type: 'if', params }), ...rest];
}

const withEnabled = (node: FlowNode, action: Action): Action => (node.config.disabled === true ? { ...action, enabled: false } : action);

function randomJoins(ctx: Lowering, paths: string[][]): string[] {
  if (paths.some((path) => path.length === 0)) return [];
  const reached = paths.flat().map((id) => reachOf(ctx, id));
  return earliest(ctx, [...reached[0]!].filter((id) => reached.every((set) => set.has(id))));
}

function lowerRandom(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const ports = activeRandomPaths(node.config);
  const paths = ports.map(({ id }) => targets(ctx, node.id, id));
  const joins = randomJoins(ctx, paths);
  const bound = extended(stop, joins);
  const branches = ports.map(({ weightKey }, i) => ({
    weight: randomPathWeight(node.config, weightKey), actions: lowerEntries(ctx, paths[i]!, bound, node.id)
  }));
  return [owned(ctx, node, withEnabled(node, { type: 'random_pick', params: { branches } })), ...lowerRest(ctx, joins, stop)];
}

function lowerLoop(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const times = str(loweredConfig(ctx, node).times);
  const { first, second, rest } = lowerArms(ctx, node, LOOP_PORTS, stop);
  const params: Params = first.length > 0 ? { times, actions: first } : { times };
  return [owned(ctx, node, withEnabled(node, { type: 'loop', params })), ...second, ...rest];
}

function lowerApi(ctx: Lowering, node: FlowNode, stop: Stops): Action[] {
  const action = API_WRITERS[node.type]!(loweredConfig(ctx, node));
  const { first, second, rest } = lowerArms(ctx, node, apiPorts(action.type), stop);
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
  let source = trigger.id;
  let entries = targets(ctx, trigger.id, 'next');
  for (let gate = leadingGate(ctx, entries); gate; gate = leadingGate(ctx, entries)) {
    visit(ctx, gate.id);
    conditions.push(writeCondition(ctx, gate));
    source = gate.id;
    entries = targets(ctx, gate.id, 'yes');
  }
  return { conditions, actions: lowerEntries(ctx, entries, NO_STOPS, source) };
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
  const settles = trigger.type === 'twitch.reward' && hasRedemptionStep(flow.nodes);
  const config = settles ? { ...trigger.config, onRedeem: 'leave' } : trigger.config;
  const written = TRIGGER_WRITERS[trigger.type]?.(config);
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
  const onRedeem = str(params.on_redeem) || DEFAULT_REWARD_OUTCOME;
  if (title === '') return { type: 'twitch.reward', config: params.on_redeem ? { reward: id, onRedeem } : { reward: id } };
  const reward = id === SETUP_REWARD_ID ? '' : id;
  const config: FlowConfig = {
    reward,
    title,
    cost: num(params.cost),
    color: str(params.color),
    cooldown: num(params.cooldown_s),
    onRedeem,
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

function cooldownNode(params: Params): { type: string; config: FlowConfig } {
  const config = { channelSeconds: num(params.global_s), viewerSeconds: num(params.user_s), ...timeLeftConfig(params) };
  const name = str(params.name);
  return { type: 'cond.cooldown', config: name ? { ...config, name } : config };
}

type ConditionNode = { type: string; config: FlowConfig };

function userEntries(params: Params, mode: 'allow' | 'deny'): string[] {
  const ids = (params[`${mode}_ids`] ?? []) as string[];
  const logins = (params[mode] ?? []) as string[];
  return [...ids.map((id) => `${id}:`), ...logins.map((login) => `:${login}`)];
}

function userNode(params: Params): ConditionNode | null {
  const allowed = userEntries(params, 'allow');
  const denied = userEntries(params, 'deny');
  if (allowed.length && denied.length) return null;
  if (allowed.length === 1 && allowed[0] === `:${SETUP_LOGIN}`) return { type: 'cond.user', config: { login: SETUP_LOGIN, mode: 'only' } };
  const users = (denied.length ? denied : allowed).join(',');
  return users ? { type: 'cond.user', config: { mode: denied.length ? 'except' : 'only', users } } : null;
}

function categoryNode(params: Params): ConditionNode | null {
  const allow = (params.allow ?? []) as string[];
  const deny = (params.deny ?? []) as string[];
  if (allow.length && deny.length) return null;
  const games = deny.length ? deny : allow;
  if (games.some((game) => game.includes(','))) return null;
  return { type: 'cond.category', config: { mode: deny.length ? 'deny' : 'allow', games: games.join(', ') } };
}

function compareNode(params: Params): { type: string; config: FlowConfig } | null {
  const op = str(params.op);
  if (!COMPARE_OPS.includes(op)) return null;
  return { type: 'cond.compare', config: { left: str(params.left), op, right: str(params.right), matchCase: params.ignore_case !== true } };
}

const varConfig = (params: Params): FlowConfig => ({ scope: str(params.scope), name: str(params.name) });

function varConditionNode(params: Params): { type: string; config: FlowConfig } {
  const op = str(params.op);
  const config: FlowConfig = { ...varConfig(params), op };
  if (VAR_OPS_WITH_VALUE.includes(op)) config.value = str(params.value);
  return { type: 'cond.var', config };
}

const CONDITION_READERS: Readonly<Record<string, (params: Params) => ConditionNode | null>> = {
  live: (params) => ({ type: 'cond.live', config: { state: str(params.state) } }),
  category: categoryNode,
  arg_regex: (params) => ({ type: 'cond.pattern', config: { pattern: str(params.pattern) } }),
  follow: (params) => ({ type: 'cond.follow', config: { minDays: num(params.min_days) } }),
  account_age: (params) => ({ type: 'cond.accountAge', config: { minDays: num(params.min_days) } })
};

function conditionToNode(condition: Condition): ConditionNode | null {
  const params = condition.params ?? {};
  if (condition.type === 'var') return varConditionNode(params);
  if (condition.type === 'compare') return compareNode(params);
  if (condition.type === 'users') return userNode(params);
  if (condition.type === 'perm') return { type: 'cond.role', config: { level: str(params.level) } };
  if (condition.type === 'chance') return { type: 'cond.chance', config: { percent: num(params.percent) } };
  if (condition.type === 'cooldown') return cooldownNode(params);
  if (Object.hasOwn(CONDITION_READERS, condition.type)) return CONDITION_READERS[condition.type]!(params);
  return loyaltyConditionNode(condition);
}

function loyaltyConditionNode({ type, params = {} }: Condition): { type: string; config: FlowConfig } | null {
  if (type !== 'points' && type !== 'counter') return null;
  const op = str(params.op);
  if (!CMP_OPS.includes(op)) return null;
  const config: FlowConfig = { op, value: num(params.value) };
  if (type === 'counter') config.counter = str(params.name);
  return { type: `cond.${type}`, config };
}

function lightConfig(params: Params): FlowConfig {
  const config: FlowConfig = { device: str(params.device), sku: str(params.sku) };
  if (str(params.device_name)) config.deviceName = str(params.device_name);
  const extra = (params.extra_lights ?? []) as string[];
  if (extra.length) config.extraLights = extra.join(',');
  config.liveOnly = params.any_time !== true;
  return config;
}

function derivedSceneId(instance: string, value: string): string {
  if (!instance || !value) return '';
  try {
    const parsed = JSON.parse(value) as number | { paramId?: number; id?: number } | null;
    const ids = typeof parsed === 'number' ? [parsed] : [parsed?.paramId, parsed?.id];
    return ids.every((id) => Number.isSafeInteger(id) && Number(id) >= 0) ? [instance, ...ids].join(':') : '';
  } catch {
    return '';
  }
}

function sceneConfig(params: Params): FlowConfig {
  const instance = str(params.scene_instance);
  const value = str(params.scene_value);
  const scene: FlowConfig = {
    scene: str(params.scene_id) || derivedSceneId(instance, value),
    sceneType: str(params.scene_type),
    sceneInstance: instance,
    sceneValue: value
  };
  if (str(params.scene_name)) scene.sceneName = str(params.scene_name);
  return scene;
}

const LIGHT_MODE_READERS = new Map<string, (params: Params) => FlowConfig>([
  ['color', (params) => ({ color: str(params.color) })],
  ['scene', sceneConfig],
  ['white', ({ kelvin }): FlowConfig => typeof kelvin === 'number' ? { kelvin } : {}],
  ['on', () => ({})],
  ['off', () => ({})]
]);

function lightsNode(params: Params): { type: string; config: FlowConfig } | null {
  const mode = str(params.mode) || 'color';
  const readMode = LIGHT_MODE_READERS.get(mode);
  if (!readMode) return null;
  const config: FlowConfig = { ...lightConfig(params), mode, ...readMode(params) };
  for (const [param, key] of LIGHT_OPTION_FIELDS) {
    const value = params[param];
    if (typeof value === 'number') config[key] = value;
  }
  return { type: 'reply.lights', config };
}

function textNode(params: Params): { type: string; config: FlowConfig } {
  return { type: 'reply.text', config: {
    op: str(params.op), input: str(params.input), arg1: str(params.arg1), arg2: str(params.arg2), saveAs: str(params.save_as)
  } };
}

function variableNode(params: Params, change: 'set' | 'add' | 'forget'): { type: string; config: FlowConfig } {
  const config = { ...varConfig(params), do: change, amount: 1 };
  if (change === 'set') return { type: 'reply.var', config: { ...config, value: str(params.value) } };
  if (change === 'forget') return { type: 'reply.var', config };
  const amount = num(params.amount);
  return { type: 'reply.var', config: { ...config, do: amount < 0 ? 'subtract' : 'add', amount: Math.abs(amount) } };
}

function rewardChoice(value: unknown, choices: ReadonlyMap<string, boolean | null>): string {
  if (typeof value !== 'boolean') return 'keep';
  return [...choices].find(([, option]) => option === value)?.[0] ?? 'keep';
}

function rewardUpdateNode(params: Params): { type: string; config: FlowConfig } {
  const config: FlowConfig = {
    reward: str(params.reward_id), setEnabled: rewardChoice(params.enabled, REWARD_ENABLED), reset: params.reset_on_stream_end === true,
    paused: rewardChoice(params.paused, REWARD_PAUSED)
  };
  if (typeof params.cost === 'number') config.cost = params.cost;
  if (typeof params.cooldown_s === 'number') config.cooldown = params.cooldown_s;
  return { type: 'reply.reward_update', config };
}

const API_READERS: Record<string, (params: Params) => { type: string; config: FlowConfig }> = {
  http_get: (params) => ({
    type: 'reply.http_get',
    config: { url: str(params.url), jsonPath: ((params.json_path ?? []) as string[]).join(JSON_PATH_SEPARATOR), saveAs: str(params.save_as) }
  }),
  http_post: (params) => ({ type: 'reply.http_post', config: { url: str(params.url), body: str(params.body), saveAs: str(params.save_as) } }),
  points_spend: (params) => ({ type: 'reply.points_spend', config: { amount: num(params.amount) } })
};

const ACTION_READERS: Record<string, (params: Params, action: Action) => { type: string; config: FlowConfig } | null> = {
  ...streamActionReaders,
  chat: (params, action) => isSetupRunPlaceholder(action)
    ? { type: 'reply.run', config: { automationId: SETUP_RUN_TARGET } }
    : { type: 'reply.chat', config: { message: str(params.text), reply: 'off' } },
  reply: (params) => ({ type: 'reply.chat', config: { message: str(params.text), reply: params.thread === true ? 'thread' : 'mention' } }),
  lights: lightsNode,
  counter_add: (params) => counterNode(params, 'add'),
  delay: (params) => ({ type: 'reply.wait', config: { seconds: num(params.ms) / 1000 } }),
  var_set: (params) => variableNode(params, 'set'),
  var_add: (params) => variableNode(params, 'add'),
  var_delete: (params) => variableNode(params, 'forget'),
  text: textNode,
  math: (params) => ({ type: 'reply.math', config: { expression: str(params.expression), saveAs: str(params.save_as) } }),
  reward_update: rewardUpdateNode,
  countdown_start: (params) => ({ type: 'reply.countdown_start', config: { ...timeLeftConfig(params), name: str(params.name), seconds: num(params.seconds) } }),
  countdown_add: (params) => ({ ...cooldownAddNode(params), type: 'reply.countdown_add' }),
  cooldown_add: cooldownAddNode,
  stop: () => ({ type: 'reply.stop', config: {} }),
  run: (params) => ({ type: 'reply.run', config: { automationId: str(params.automation_id) } }),
  counter_set: (params) => counterNode(params, 'set'),
  redemption: (params) => ({ type: 'reply.redemption', config: { result: str(params.status) } }),
  countdown_stop: (params) => namedTimerNode('countdown_stop', params),
  cooldown_reset: (params) => namedTimerNode('cooldown_reset', params)
};

function namedTimerNode(type: NamedTimerActionType, params: Params): { type: string; config: FlowConfig } {
  return { type: `reply.${type}`, config: { name: str(params.name) } };
}

function counterNode(params: Params, change: 'add' | 'set'): { type: string; config: FlowConfig } | null {
  const field = change === 'add' ? 'amount' : 'value';
  const value = num(params[field]);
  if (field === 'amount' && value === 0) return null;
  return { type: 'reply.counter', config: { counter: str(params.name), change, [field]: value } };
}

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
  const [okPort, failurePort] = apiPorts(action.type);
  const worked = placeActions(b, (params.on_ok ?? []) as Action[], `${path}.params.on_ok`, [{ from: id, port: okPort }]);
  const failed = placeActions(b, (params.on_failure ?? []) as Action[], `${path}.params.on_failure`, [{ from: id, port: failurePort }]);
  return [...worked, ...failed];
}

function placeRandom(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  const branches = (action.params?.branches ?? []) as { weight: number; actions?: Action[] }[];
  const ports = RANDOM_PATHS.slice(0, branches.length);
  const config = Object.fromEntries(ports.map(({ weightKey }, i) => [weightKey, branches[i]!.weight] as const));
  const id = addNode(b, RANDOM_TYPE, placedConfig(action, { ...config, paths: String(branches.length) }), tails);
  return ports.flatMap(({ id: port }, i) => placeActions(b, branches[i]!.actions ?? [], `${path}.params.branches[${i}].actions`, [{ from: id, port }]));
}

const CONTROL_READERS = { if: placeIf, parallel: placeParallel, loop: placeLoop, random_pick: placeRandom } as const;

function placeAction(b: Building, action: Action, path: string, tails: Tail[]): Tail[] {
  if (disabledControlFlow(action)) {
    b.issues.push({ path, code: FLOW_ISSUE.unsupportedDisabledControlFlow });
    return tails;
  }
  if (Object.hasOwn(CONTROL_READERS, action.type)) return CONTROL_READERS[action.type as keyof typeof CONTROL_READERS](b, action, path, tails);
  if (Object.hasOwn(API_READERS, action.type)) return placeApi(b, action, path, tails);
  const node = Object.hasOwn(ACTION_READERS, action.type) ? ACTION_READERS[action.type]!(action.params ?? {}, action) : null;
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
  let index = 0;
  while (index < actions.length) {
    const action = actions[index]!;
    const streamInfo = streamInfoRun(actions, index);
    tails = streamInfo
      ? [{ from: addNode(b, 'reply.stream_info', placedConfig(action, streamInfo.config), tails), port: 'next' }]
      : placeAction(b, action, `${path}[${index}]`, tails);
    index = streamInfo?.end ?? index + 1;
  }
  return tails;
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
  return eventTokensToBuilder(scan.kind, str(node.config[field]), counters.length > 0);
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
