// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { COMPARE_OPS } from './compare';
import {
  isSetupRunPlaceholder, JSON_PATH_SEPARATOR, LIGHT_OPTION_FIELDS, num, REWARD_ENABLED, REWARD_PAUSED, str, timeLeftConfig,
  type FlowConfig, type NamedTimerActionType
} from './flow-graph-model';
import { SETUP_LOGIN, SETUP_RUN_TARGET } from './flow-setup';
import { streamActionReaders } from './stream-flow';
import type { Action, Condition, Params } from './types';
import { CMP_OPS, VAR_OPS_WITH_VALUE } from './values';

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

export function conditionToNode(condition: Condition): ConditionNode | null {
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

export const API_READERS: Record<string, (params: Params) => { type: string; config: FlowConfig }> = {
  http_get: (params) => ({
    type: 'reply.http_get',
    config: { url: str(params.url), jsonPath: ((params.json_path ?? []) as string[]).join(JSON_PATH_SEPARATOR), saveAs: str(params.save_as) }
  }),
  http_post: (params) => ({ type: 'reply.http_post', config: { url: str(params.url), body: str(params.body), saveAs: str(params.save_as) } }),
  points_spend: (params) => ({ type: 'reply.points_spend', config: { amount: num(params.amount) } })
};

export const ACTION_READERS: Record<string, (params: Params, action: Action) => { type: string; config: FlowConfig } | null> = {
  ...streamActionReaders,
  chat: (params, action): { type: string; config: FlowConfig } => isSetupRunPlaceholder(action)
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
