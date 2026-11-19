// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { SETUP_REWARD_ID } from './flow-setup';
import { amountUnit, CREATED_REWARD_OUTCOME, DEFAULT_REWARD_OUTCOME, num, recipeName, str, type FlowConfig, type FlowNode } from './flow-graph-model';
import type { Params, Trigger } from './types';

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

function eventTrigger(kind: string, filters?: Record<string, string>): Trigger {
  return { type: 'event', params: filters ? { kind, filters } : { kind } };
}

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

export const TRIGGER_WRITERS: Record<string, (config: FlowConfig) => Trigger | null> = {
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

export function triggerAmountUnit(node?: Pick<FlowNode, 'type' | 'config'>): ReturnType<typeof amountUnit> {
  const trigger = node && TRIGGER_WRITERS[node.type]?.(node.config);
  return trigger ? amountUnit(trigger) : undefined;
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

export function triggerToNode(trigger: Trigger): Pick<FlowNode, 'type' | 'config'> | null {
  return TRIGGER_READERS.get(trigger.type)?.(trigger.params ?? {}) ?? null;
}
