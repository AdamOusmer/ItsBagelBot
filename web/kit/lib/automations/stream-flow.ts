// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FlowConfig } from './flow-graph';
import type { Action, Params } from './types';

type ValueKind = 'text' | 'number' | 'toggle' | 'lines' | 'change';
type Fields = Record<string, ValueKind>;
type StreamNode = { type: string; config: FlowConfig };

const TARGET_FIELDS = { who: 'text', user_id: 'text', login: 'text' } as const satisfies Fields;

const STREAM_FIELDS: Record<string, Fields> = {
  shoutout: { login: 'text' },
  announce: { text: 'text', color: 'text' },
  reply: { text: 'text' },
  timeout: { ...TARGET_FIELDS, seconds: 'number', reason: 'text' },
  ban: { ...TARGET_FIELDS, reason: 'text' },
  unban: { ...TARGET_FIELDS },
  poll: { title: 'text', choices: 'lines', seconds: 'number' },
  poll_end: { status: 'text' },
  prediction: { title: 'text', outcomes: 'lines', seconds: 'number' },
  prediction_end: { status: 'text', winner: 'number' },
  raid: { login: 'text' },
  raid_cancel: {},
  mod: { ...TARGET_FIELDS, on: 'change' },
  vip: { ...TARGET_FIELDS, on: 'change' },
  points_add: { ...TARGET_FIELDS, amount: 'number' },
  points_set: { ...TARGET_FIELDS, amount: 'number' },
  chat_mode: { mode: 'text', on: 'toggle', value: 'number' },
  clear_chat: {},
  ad_snooze: {},
  whisper: { login: 'text', text: 'text' },
  unban_resolve: { status: 'text', resolution: 'text' },
  branded_content: { on: 'toggle' }
};

const nodeType = (type: string): string => (type === 'points_add' ? 'reply.points' : `reply.${type}`);

export const streamTargetNodes = new Set(Object.keys(STREAM_FIELDS).filter((type) => STREAM_FIELDS[type]!.who).map(nodeType));

export const configKey = (param: string): string => {
  if (param === 'user_id') return 'userId';
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

function parameter(kind: ValueKind, value: unknown): string | number | boolean | string[] {
  if (kind === 'lines') return text(value).split(/\r?\n/);
  if (kind === 'change') return value === 'add';
  return scalar(kind, value);
}

function configValue(kind: ValueKind, value: unknown): string | number | boolean {
  if (kind === 'lines') return Array.isArray(value) ? value.join('\n') : '';
  if (kind === 'change') return value === true ? 'add' : 'remove';
  return scalar(kind, value);
}

function scalar(kind: Exclude<ValueKind, 'lines' | 'change'>, value: unknown): string | number | boolean {
  if (kind === 'number') return Number(value) || 0;
  return kind === 'toggle' ? value === true : text(value);
}

function streamAction(type: string, config: FlowConfig): Action {
  const fields = activeFields(STREAM_FIELDS[type]!, config.who);
  const values = fields.map(([key, kind]) => ({ key, kind, value: config[fieldConfigKey(key, kind, config.who)] }));
  const params = Object.fromEntries(values.filter(({ value }) => value !== undefined).map(({ key, kind, value }) => [key, parameter(kind, value)]));
  return { type, params };
}

function streamNode(type: string, params: Params): StreamNode | null {
  const fields = STREAM_FIELDS[type]!;
  const lists = Object.entries(fields).filter(([, kind]) => kind === 'lines');
  if (lists.some(([key]) => hasMultilineOption(params[key]))) return null;
  const supplied = activeFields(fields, params.who).filter(([key]) => params[key] !== undefined);
  const config = Object.fromEntries(supplied.map(([key, kind]) => [fieldConfigKey(key, kind, params.who), configValue(kind, params[key])]));
  return { type: nodeType(type), config };
}

function hasMultilineOption(value: unknown): boolean {
  return Array.isArray(value) && value.some((item) => /[\r\n]/.test(text(item)));
}

export const streamActionWriters = Object.fromEntries(
  Object.keys(STREAM_FIELDS).map((type) => [nodeType(type), (config: FlowConfig) => streamAction(type, config)])
);

export const streamActionReaders = Object.fromEntries(
  Object.keys(STREAM_FIELDS).map((type) => [type, (params: Params) => streamNode(type, params)])
);
