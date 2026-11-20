// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  activeRandomPaths, apiPorts, awaitingUser, canNameCooldown, CHECK_PORTS, cooldownWindows, flowList, isConditionType,
  JSON_PATH_SEPARATOR, LOOP_PORTS, LOOP_TYPE, num, RANDOM_TYPE, recipeName, REWARD_ENABLED, REWARD_PAUSED, setupRunPlaceholder,
  str, TEXT_OPERAND_OPS, textFieldUsed, textOp, timeLeftParams, varConditionOp, varIdentity, variableActionType, viewerList,
  type FlowConfig, type FlowNode, type NamedTimerActionType
} from './flow-graph-model';
import { SETUP_LOGIN, SETUP_RUN_TARGET } from './flow-setup';
import { streamActionWriters, streamInfoActions } from './stream-flow';
import type { Action, Condition, Params } from './types';
import { VAR_OPS_WITH_VALUE } from './values';

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

const JSON_PATH_BRACKET = /\[(\d+|"[A-Za-z0-9_-]+"|'[A-Za-z0-9_-]+')\](?=\.|\[|$)/g;

function jsonPathOf(config: FlowConfig): string[] | undefined {
  const path = str(config.jsonPath).trim().replace(JSON_PATH_BRACKET, (_, segment: string) =>
    `${JSON_PATH_SEPARATOR}${segment.replace(/['"]/g, '')}`);
  const segments = path.split(JSON_PATH_SEPARATOR).filter(Boolean);
  return segments.length > 0 ? segments : undefined;
}

export const API_WRITERS: Record<string, (config: FlowConfig) => Action> = {
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

const NODE_PORTS: Readonly<Record<string, (node: FlowNode) => readonly string[]>> = {
  [LOOP_TYPE]: () => LOOP_PORTS,
  [RANDOM_TYPE]: (node) => activeRandomPaths(node.config).map(({ id }) => id)
};

export function outputPorts(node: FlowNode): readonly string[] {
  if (isConditionType(node.type)) return CHECK_PORTS;
  const ports = NODE_PORTS[node.type];
  if (ports) return ports(node);
  return Object.hasOwn(API_WRITERS, node.type) ? apiPorts(API_WRITERS[node.type]!({}).type) : ['next'];
}

const DEFAULT_LIGHT_COLOR = '#ffffff';
const DEFAULT_LIGHT_KELVIN = 4000;

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
  const validBrightness = Number.isFinite(brightness) && brightness !== 0;
  const useBrightness = mode !== 'off' && validBrightness;
  return { type: 'lights', params: withOptional(params, { brightness: useBrightness && brightness, hold_s: hold > 0 && hold }) };
}

function runAction(config: FlowConfig): Action {
  const id = str(config.automationId);
  return id === SETUP_RUN_TARGET ? setupRunPlaceholder() : { type: 'run', params: { automation_id: id } };
}

function userCondition(config: FlowConfig): Condition {
  if (awaitingUser(config)) return { type: 'users', params: { allow: [SETUP_LOGIN] } };
  const ids = viewerList(config).map(({ id }) => id);
  return { type: 'users', params: { [config.mode === 'except' ? 'deny_ids' : 'allow_ids']: ids } };
}

export const CONDITION_WRITERS: Record<string, (config: FlowConfig) => Condition> = {
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

export const ACTION_WRITERS: Record<string, (config: FlowConfig) => Action | Action[]> = {
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
