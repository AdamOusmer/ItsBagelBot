// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FlowConfig } from './flow-graph';
import type { Action, Params } from './types';

type ValueKind = 'text' | 'number' | 'toggle' | 'lines';
type Fields = Record<string, ValueKind>;
type StreamNode = { type: string; config: FlowConfig };

const STREAM_FIELDS: Record<string, Fields> = {
  poll: { title: 'text', choices: 'lines', seconds: 'number' },
  poll_end: { status: 'text' },
  prediction: { title: 'text', outcomes: 'lines', seconds: 'number' },
  prediction_end: { status: 'text', winner: 'number' },
  raid: { login: 'text' },
  raid_cancel: {},
  mod: { login: 'text', on: 'toggle' },
  vip: { login: 'text', on: 'toggle' },
  chat_mode: { mode: 'text', on: 'toggle', value: 'number' },
  clear_chat: {},
  ad_snooze: {},
  whisper: { login: 'text', text: 'text' },
  unban_resolve: { status: 'text', resolution: 'text' },
  branded_content: { on: 'toggle' }
};

// Login templates are resolved by outgress when the step runs, rather than bound
// to the viewer ID field used by a condition in the editor.
const configKey = (param: string): string => (param === 'login' ? 'target' : param);
const text = (value: unknown): string => (typeof value === 'string' ? value : '');

function parameter(kind: ValueKind, value: unknown): string | number | boolean | string[] {
  if (kind === 'lines') return text(value).split(/\r?\n/);
  return scalar(kind, value);
}

function configValue(kind: ValueKind, value: unknown): string | number | boolean {
  if (kind === 'lines') return Array.isArray(value) ? value.join('\n') : '';
  return scalar(kind, value);
}

function scalar(kind: Exclude<ValueKind, 'lines'>, value: unknown): string | number | boolean {
  if (kind === 'number') return Number(value) || 0;
  return kind === 'toggle' ? value === true : text(value);
}

function streamAction(type: string, config: FlowConfig): Action {
  const supplied = Object.entries(STREAM_FIELDS[type]!).filter(([key]) => config[configKey(key)] !== undefined);
  const params = Object.fromEntries(supplied.map(([key, kind]) => [key, parameter(kind, config[configKey(key)])]));
  return { type, params };
}

function streamNode(type: string, params: Params): StreamNode | null {
  const fields = STREAM_FIELDS[type]!;
  // A newline inside one option cannot be represented as one option per line.
  const lists = Object.entries(fields).filter(([, kind]) => kind === 'lines');
  if (lists.some(([key]) => hasMultilineOption(params[key]))) return null;
  const supplied = Object.entries(fields).filter(([key]) => params[key] !== undefined);
  const config = Object.fromEntries(supplied.map(([key, kind]) => [configKey(key), configValue(kind, params[key])]));
  return { type: `reply.${type}`, config };
}

function hasMultilineOption(value: unknown): boolean {
  return Array.isArray(value) && value.some((item) => /[\r\n]/.test(text(item)));
}

export const streamActionWriters = Object.fromEntries(
  Object.keys(STREAM_FIELDS).map((type) => [`reply.${type}`, (config: FlowConfig) => streamAction(type, config)])
);

export const streamActionReaders = Object.fromEntries(
  Object.keys(STREAM_FIELDS).map((type) => [type, (params: Params) => streamNode(type, params)])
);
