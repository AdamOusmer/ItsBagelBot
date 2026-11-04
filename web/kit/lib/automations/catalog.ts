// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { COMPARE_OPS } from './compare';
import { eventKinds } from './event-kinds';
import { CMP_OPS, PERM_LEVELS } from './values';

export type FieldKind = 'text' | 'template' | 'login' | 'int' | 'bool' | 'enum' | 'list' | 'actions' | 'condition' | 'branches' | 'map';

export interface FieldDef {
  name: string;
  kind: FieldKind;
  required?: boolean;
  min?: number;
  max?: number;
  values?: readonly string[];
  labelKey: string;
}

export interface TypeDef {
  group: 'trigger' | 'condition' | 'action';
  type: string;
  supported: boolean;
  labelKey: string;
  fields: FieldDef[];
}

type FieldOpts = Partial<Omit<FieldDef, 'name' | 'kind' | 'labelKey'>>;

type FieldName = FieldDef['name'];

type TypeName = TypeDef['type'];

const f = (name: FieldName, kind: FieldKind, opts: FieldOpts = {}): FieldDef => ({
  name,
  kind,
  ...opts,
  labelKey: `automations.field.${name}`,
});

const def = (group: TypeDef['group'], type: TypeName, fields: FieldDef[] = [], supported = true): TypeDef => ({
  group,
  type,
  supported,
  labelKey: `automations.${group}.${type}`,
  fields,
});

const trigger = (type: TypeName, fields?: FieldDef[]) => def('trigger', type, fields);
const condition = (type: TypeName, fields?: FieldDef[]) => def('condition', type, fields);
const action = (type: TypeName, fields?: FieldDef[]) => def('action', type, fields);

const counterName = f('name', 'text', { required: true, min: 1, max: 64 });
const reasonOpt = f('reason', 'template', { max: 500 });
const loginReq = f('login', 'login', { required: true, min: 1, max: 100 });
const loginOpt = f('login', 'login', { min: 0, max: 100 });
const textReq = f('text', 'template', { required: true, min: 1, max: 500 });
const op = f('op', 'enum', { required: true, values: CMP_OPS });
const listLogin = (name: FieldName) => f(name, 'list', { max: 100 });
const listUserId = (name: FieldName) => f(name, 'list', { max: 100 });
const listCategory = (name: FieldName) => f(name, 'list', { max: 50 });

export const TRIGGER_TYPES: TypeDef[] = [
  trigger('command', [
    f('name', 'text', { required: true, min: 1, max: 200 }),
    f('aliases', 'list', { max: 10 }),
    f('match', 'enum', { values: ['start', 'exact', 'anywhere', 'regex'] }),
    f('case_sensitive', 'bool'),
    f('sources', 'list', { min: 1, max: 2, values: ['chat', 'sub_message'] }),
  ]),
  trigger('event', [
    f('kind', 'enum', { required: true, values: eventKinds().map((k) => k.kind) }),
    f('filters', 'map', { max: 10 }),
  ]),
  trigger('timer', [
    f('interval_s', 'int', { required: true, min: 60, max: 86400 }),
    f('random_jitter_s', 'int', { min: 0, max: 3600 }),
    f('min_chat_lines', 'int', { min: 0, max: 100 }),
    f('offline_ok', 'bool'),
  ]),
  trigger('reward', [f('reward_id', 'text', { required: true, min: 1, max: 64 })]),
  trigger('manual'),
];

export const CONDITION_TYPES: TypeDef[] = [
  condition('perm', [f('level', 'enum', { required: true, values: PERM_LEVELS })]),
  condition('users', [listLogin('allow'), listLogin('deny'), listUserId('allow_ids'), listUserId('deny_ids')]),
  condition('live', [f('state', 'enum', { required: true, values: ['live', 'offline'] })]),
  condition('cooldown', [f('global_s', 'int', { min: 0, max: 86400 }), f('user_s', 'int', { min: 0, max: 86400 })]),
  condition('chance', [f('percent', 'int', { required: true, min: 1, max: 100 })]),
  condition('counter', [counterName, op, f('value', 'int', { required: true })]),
  condition('points', [op, f('value', 'int', { required: true })]),
  condition('arg_regex', [f('pattern', 'text', { required: true, min: 1, max: 200 })]),
  condition('category', [listCategory('allow'), listCategory('deny')]),
  condition('compare', [
    f('left', 'template', { required: true, min: 1, max: 500 }),
    f('op', 'enum', { required: true, values: COMPARE_OPS }),
    f('right', 'template', { max: 500 }),
  ]),
];

export const ACTION_TYPES: TypeDef[] = [
  action('chat', [textReq]),
  action('announce', [textReq, f('color', 'enum', { values: ['', 'primary', 'blue', 'green', 'orange', 'purple'] })]),
  action('reply', [textReq]),
  action('timeout', [loginReq, f('seconds', 'int', { required: true, min: 1, max: 1209600 }), reasonOpt]),
  action('ban', [loginReq, reasonOpt]),
  action('delete_message'),
  action('warn', [loginReq, f('reason', 'template', { required: true, min: 1, max: 500 })]),
  action('shield', [f('on', 'bool')]),
  action('shoutout', [loginReq]),
  action('clip'),
  action('marker', [f('description', 'template', { max: 140 })]),
  action('set_title', [f('title', 'template', { required: true, min: 1, max: 140 })]),
  action('set_game', [f('game', 'template', { required: true, min: 1, max: 100 })]),
  action('set_tags', [f('tags', 'template', { required: true, min: 1, max: 500 })]),
  action('commercial', [f('seconds', 'enum', { required: true, values: ['30', '60', '90', '120', '150', '180'] })]),
  action('points_add', [loginOpt, f('amount', 'int', { required: true, min: -1000000, max: 1000000 })]),
  action('points_set', [loginOpt, f('amount', 'int', { required: true, min: 0, max: 1000000000 })]),
  action('counter_add', [counterName, f('amount', 'int', { required: true, min: -1000000, max: 1000000 })]),
  action('counter_set', [counterName, f('value', 'int', { required: true })]),
  action('redemption', [f('status', 'enum', { required: true, values: ['fulfill', 'cancel'] })]),
  action('delay', [f('ms', 'int', { required: true, min: 1, max: 10000 })]),
  action('random_pick', [f('branches', 'branches', { required: true, min: 2, max: 5 })]),
  action('run', [f('automation_id', 'text', { required: true, min: 1, max: 64 })]),
  action('if', [
    f('condition', 'condition', { required: true }),
    f('then', 'actions'),
    f('else', 'actions'),
  ]),
  action('discord_post', [
    f('guild_id', 'text', { required: true }),
    f('channel_id', 'text', { required: true }),
    f('text', 'template', { required: true, min: 1, max: 2000 }),
  ]),
  action('http_post', [
    f('url', 'text', { required: true, min: 1, max: 512 }),
    f('body', 'template', { max: 2000 }),
  ]),
  action('lights', [
    f('device', 'text', { required: true, min: 1, max: 64 }),
    f('sku', 'text', { required: true, min: 1, max: 32 }),
    f('device_name', 'text', { max: 100 }),
    f('mode', 'enum', { values: ['color', 'scene'] }),
    f('color', 'text', { min: 7, max: 7 }),
    f('scene_id', 'text', { max: 100 }),
    f('scene_name', 'text', { max: 100 }),
    f('scene_type', 'text', { min: 1, max: 64 }),
    f('scene_instance', 'text', { min: 1, max: 64 }),
    f('scene_value', 'text', { min: 1, max: 200 }),
  ]),
  action('stop'),
];
