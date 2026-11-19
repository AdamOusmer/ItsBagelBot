// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FlowConfig } from './flow-graph';
import { trimSpace } from './text';
import type { Action, Params } from './types';

type ValueKind = 'text' | 'number' | 'toggle' | 'lines' | 'numberOption' | 'confirmation' | 'change';
type Scalar = string | number | boolean;
type ValueCodec = { write(value: unknown): Scalar | string[]; read(value: unknown): Scalar };
type Fields = Record<string, ValueKind>;
type StreamNode = { type: string; config: FlowConfig };

const TARGET_FIELDS = { who: 'text', user_id: 'text', login: 'text' } as const satisfies Fields;

const STREAM_FIELDS: Record<string, Fields> = {
  shoutout: { login: 'text' },
  announce: { text: 'text', color: 'text' },
  timeout: { ...TARGET_FIELDS, seconds: 'number', reason: 'text' },
  ban: { ...TARGET_FIELDS, reason: 'text' },
  unban: { ...TARGET_FIELDS },
  poll: { title: 'text', choices: 'lines', seconds: 'number' },
  poll_end: { status: 'text' },
  prediction: { title: 'text', outcomes: 'lines', seconds: 'number' },
  prediction_end: { status: 'text' },
  raid: { login: 'text' },
  raid_cancel: {},
  mod: { ...TARGET_FIELDS, on: 'change' },
  vip: { ...TARGET_FIELDS, on: 'change' },
  points_add: { ...TARGET_FIELDS, amount: 'number' },
  points_set: { ...TARGET_FIELDS, amount: 'number' },
  chat_mode: { mode: 'text', on: 'toggle' },
  clear_chat: {},
  delete_message: {},
  pin: { text: 'text' },
  ad_snooze: {},
  whisper: { login: 'text', text: 'text' },
  unban_resolve: { status: 'text', resolution: 'text' },
  commercial: { seconds: 'numberOption', quiet: 'confirmation' },
  marker: { description: 'text', quiet: 'confirmation' },
  clip: { title: 'text', seconds: 'number', reply: 'text', quiet: 'confirmation' }
};

const STREAM_INFO_FIELDS = { set_title: 'title', set_game: 'game', set_tags: 'tags' } as const;
const BRANDED_CONTENT = 'branded_content';

const nodeType = (type: string): string => (type === 'points_add' ? 'reply.points' : `reply.${type}`);

const CHAT_MODE_VALUE_FIELDS = new Map([
  ['slow', 'wait'], ['followers', 'followAge'], ['chat_delay', 'delay']
]);

export const chatModeValueField = (mode: unknown): string | undefined => CHAT_MODE_VALUE_FIELDS.get(String(mode));

type Adapter = {
  write?(config: FlowConfig): Params;
  read(params: Params): FlowConfig;
};

const STREAM_ADAPTERS: Record<string, Adapter> = {
  announce: { read: (params) => ({ color: text(params.color) || 'primary' }) },
  poll: {
    write: (config) => config.extraVotes === true ? { points_per_vote: Number(config.pointsPerVote ?? 100) } : {},
    read: (params): FlowConfig => {
      const points = Number(params.points_per_vote);
      return points > 0 ? { extraVotes: true, pointsPerVote: points } : { extraVotes: false };
    }
  },
  prediction_end: {
    write: (config) => config.status === 'resolve' ? { outcome: text(config.outcome) } : {},
    read: (params): FlowConfig => params.status === 'resolve'
      ? { outcome: text(params.outcome) || String(params.winner ?? '') }
      : {}
  },
  chat_mode: { write: chatModeParams, read: chatModeConfig }
};

function chatModeValue(config: FlowConfig): unknown {
  const key = chatModeValueField(config.mode);
  return key && config.on === true ? config[key] : undefined;
}

function chatModeParams(config: FlowConfig): Params {
  const value = chatModeValue(config);
  return value === undefined ? {} : { value: Number(value) };
}

function chatModeConfig(params: Params): FlowConfig {
  if (params.on !== true) return { on: false };
  const key = chatModeValueField(params.mode);
  if (!key) return {};
  const value = Number(params.value ?? 0);
  if (key === 'wait') return { wait: value || 30 };
  return { [key]: key === 'delay' ? String(value || 2) : value };
}

export const streamTargetNodes = new Set(Object.keys(STREAM_FIELDS).filter((type) => STREAM_FIELDS[type]!.who).map(nodeType));

export const configKey = (param: string): string => {
  if (param === 'user_id') return 'userId';
  if (param === 'quiet') return 'confirm';
  return param === 'login' ? 'target' : param;
};

export const usesLegacyTarget = (who: unknown): boolean => who === undefined || who === 'login';

function activeFields(fields: Fields, who: unknown): [string, ValueKind][] {
  const entries = Object.entries(fields);
  if (!fields.who || who === 'user') return entries;
  const legacy = usesLegacyTarget(who);
  const active = (key: string): boolean => key !== 'login' || legacy;
  return entries.filter(([key]) => key !== 'user_id' && active(key));
}

function fieldConfigKey(param: string, kind: ValueKind, who: unknown): string {
  if (kind === 'change') return 'change';
  return param === 'login' && who === 'user' ? 'login' : configKey(param);
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const number = (value: unknown): number => Number(value) || 0;
const toggle = (value: unknown): boolean => value === true;

const VALUE_CODECS: Record<ValueKind, ValueCodec> = {
  text: { write: text, read: text },
  number: { write: number, read: number },
  toggle: { write: toggle, read: toggle },
  lines: { write: (value) => text(value).split(/\r?\n/).filter((line) => trimSpace(line) !== ''), read: (value) => Array.isArray(value) ? value.join('\n') : '' },
  numberOption: { write: number, read: (value) => String(number(value)) },
  confirmation: { write: (value) => value === false, read: (value) => value !== true },
  change: { write: (value) => value === 'add', read: (value) => value === true ? 'add' : 'remove' }
};

function streamAction(type: string, config: FlowConfig): Action {
  const fields = activeFields(STREAM_FIELDS[type]!, config.who);
  const values = fields.map(([key, kind]) => ({ key, kind, value: config[fieldConfigKey(key, kind, config.who)] }));
  const params = Object.fromEntries(values.filter(({ value }) => value !== undefined).map(({ key, kind, value }) => [key, VALUE_CODECS[kind].write(value)]));
  return { type, params: { ...params, ...STREAM_ADAPTERS[type]?.write?.(config) } };
}

function streamNode(type: string, params: Params): StreamNode | null {
  const fields = STREAM_FIELDS[type]!;
  const lists = Object.entries(fields).filter(([, kind]) => kind === 'lines');
  if (lists.some(([key]) => hasMultilineOption(params[key]))) return null;
  const supplied = activeFields(fields, params.who).filter(([key, kind]) => params[key] !== undefined || kind === 'confirmation');
  const config = Object.fromEntries(supplied.map(([key, kind]) => [fieldConfigKey(key, kind, params.who), VALUE_CODECS[kind].read(params[key])]));
  return { type: nodeType(type), config: { ...config, ...STREAM_ADAPTERS[type]?.read(params) } };
}

function hasMultilineOption(value: unknown): boolean {
  return Array.isArray(value) && value.some((item) => /[\r\n]/.test(text(item)));
}

export function streamInfoActions(config: FlowConfig): Action[] {
  const actions: Action[] = Object.entries(STREAM_INFO_FIELDS).flatMap(([type, field]) => {
    const value = text(config[field]).trim();
    return value ? [{ type, params: { [field]: value } }] : [];
  });
  const branded = config.branded;
  if (branded === 'on' || branded === 'off') actions.push({ type: BRANDED_CONTENT, params: { on: branded === 'on' } });
  return actions;
}

export function invalidLiteralTags(value: unknown): boolean {
  const tags = text(value);
  if (tags.includes('{')) return false;
  const literals = tags.split(',').map((tag) => tag.trim()).filter(Boolean);
  return literals.length > 10 || literals.some((tag) => !/^[\p{L}\p{N}]{1,25}$/u.test(tag));
}

function streamInfoField({ type, params }: Action): FlowConfig {
  if (type === BRANDED_CONTENT) return { branded: params?.on === true ? 'on' : 'off' };
  const field = STREAM_INFO_FIELDS[type as keyof typeof STREAM_INFO_FIELDS];
  return { [field]: text(params?.[field]) };
}

function streamInfoType(type: Action['type']): boolean {
  return type === BRANDED_CONTENT || Object.hasOwn(STREAM_INFO_FIELDS, type);
}

export function streamInfoRun(actions: readonly Action[], start: number): { config: FlowConfig; end: number } | undefined {
  const first = actions[start]!;
  if (!streamInfoType(first.type)) return undefined;
  const config: FlowConfig = { branded: 'keep' };
  const seen = new Set<Action['type']>();
  const disabled = first.enabled === false;
  let end = start;
  for (; end < actions.length; end++) {
    const action = actions[end]!;
    if (!streamInfoType(action.type) || seen.has(action.type)) break;
    if ((action.enabled === false) !== disabled) break;
    Object.assign(config, streamInfoField(action));
    seen.add(action.type);
  }
  return { config, end };
}

export const streamActionWriters = Object.fromEntries(
  Object.keys(STREAM_FIELDS).map((type) => [nodeType(type), (config: FlowConfig) => streamAction(type, config)])
);

export const streamActionReaders = Object.fromEntries(
  Object.keys(STREAM_FIELDS).map((type) => [type, (params: Params) => streamNode(type, params)])
);
