// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { ACTION_TYPES } from './catalog';
import { SETUP_LOGIN } from './flow-setup';
import { streamTargetNodes, usesLegacyTarget } from './stream-flow';
import type { Action, Issue, Params, Trigger } from './types';
import { VAR_OPS_WITH_VALUE } from './values';

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

export const EVENT_TOKENS: Record<string, Record<string, string>> = {
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

export const EVENT_ONLY_TOKENS = new Set(['bits', 'viewers', 'tier', 'months', 'input', 'message', 'streak', 'total', 'reward', 'cost', 'text', 'seconds']);

export const BARE_TOKEN = /\{([a-z]+)\}/g;
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

const TRIGGER_PREFIXES = ['twitch.', 'schedule.'];
export const isTriggerType = (type: FlowNode['type']) =>
  type === 'countdown.finished' || TRIGGER_PREFIXES.some((prefix) => type.startsWith(prefix));
export const isConditionType = (type: string) => type.startsWith('cond.');

export const str = (v: unknown): string => (typeof v === 'string' ? v : '');
export const num = (v: unknown): number => (typeof v === 'number' ? v : Number(v) || 0);

type CooldownWindows = { channelSeconds: number; viewerSeconds: number };

export function cooldownWindows(config: FlowConfig): CooldownWindows {
  if (config.channelSeconds !== undefined || config.viewerSeconds !== undefined) {
    return { channelSeconds: num(config.channelSeconds), viewerSeconds: num(config.viewerSeconds) };
  }
  const seconds = num(config.seconds);
  return config.perUser ? { channelSeconds: 0, viewerSeconds: seconds } : { channelSeconds: seconds, viewerSeconds: 0 };
}

export const canNameCooldown = ({ channelSeconds, viewerSeconds }: CooldownWindows): boolean => viewerSeconds === 0 && channelSeconds > 0;

export const DEFAULT_REWARD_OUTCOME = 'leave';
export const CREATED_REWARD_OUTCOME = 'fulfill';

export const hasRedemptionStep = (nodes: readonly FlowNode[]): boolean =>
  nodes.some((node) => node.type === 'reply.redemption' && node.config.disabled !== true);

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
  'reply.speak': ['text'],
  'reply.clip': ['title', 'reply'],
  'cond.cooldown': ['leftReply', 'readyReply']
};

export const recipeName = (v: unknown) => str(v).trim().toLowerCase();

export const varIdentity = (config: FlowConfig): Params => ({ scope: str(config.scope) || 'channel', name: recipeName(config.name) });

export const TEXT_OPERAND_OPS = {
  arg1: ['replace', 'substring', 'split_pick', 'contains', 'count'],
  arg2: ['replace', 'substring', 'split_pick']
} satisfies Record<string, readonly string[]>;

export function textFieldUsed(field: string, op: string): boolean {
  const operands: Partial<Record<string, readonly string[]>> = TEXT_OPERAND_OPS;
  return operands[field]?.includes(op) ?? true;
}

export const textOp = (config: FlowConfig) => str(config.op) || 'upper';
export const varConditionOp = (config: FlowConfig) => str(config.op) || 'eq';

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

export function templateFields(node: FlowNode): readonly string[] {
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

export const REWARD_ENABLED = new Map<string, boolean | null>([
  ['keep', null],
  ['on', true],
  ['off', false]
]);

export const REWARD_PAUSED = new Map<string, boolean>([['pause', true], ['resume', false]]);

export const JSON_PATH_SEPARATOR = '.';

const API_PORTS: Readonly<Record<string, readonly [string, string]>> = {
  http_get: ['ok', 'failure'],
  http_post: ['ok', 'failure'],
  points_spend: ['enough', 'notEnough']
};

export const apiPorts = (type: Action['type']): readonly [string, string] => API_PORTS[type]!;

export const LOOP_TYPE = 'reply.loop';
export const LOOP_PORTS = ['body', 'done'] as const;
export const RANDOM_TYPE = 'reply.random';
const randomBranchCount = ACTION_TYPES.find((entry) => entry.type === 'random_pick')!.fields.find((field) => field.name === 'branches')!;
export const RANDOM_PATHS = Array.from({ length: randomBranchCount.max! }, (_, i) => ({
  id: `p${i + 1}`, path: i + 1, weightKey: `weight${i + 1}`
}));
export const RANDOM_PATH_COUNTS = RANDOM_PATHS.slice(randomBranchCount.min! - 1).map(({ path }) => String(path));
export const RANDOM_PATH_DEFAULT_WEIGHT = 1;
export const activeRandomPaths = (config: FlowConfig) =>
  RANDOM_PATHS.slice(0, Number(RANDOM_PATH_COUNTS.find((paths) => paths === String(config.paths)) ?? RANDOM_PATH_COUNTS[0]));
export const randomPathWeight = (config: FlowConfig, key: keyof FlowConfig) => num(config[key] ?? RANDOM_PATH_DEFAULT_WEIGHT);
export const CHECK_PORTS = ['yes', 'no'] as const;

export const LIGHT_OPTION_FIELDS = [
  ['brightness', 'brightness'],
  ['hold_s', 'holdS']
] as const;

export const awaitingUser = (config: FlowConfig) => config.users === undefined && config.login === SETUP_LOGIN;

const SETUP_RUN_TEXT = '[setup-required:run]';
export const setupRunPlaceholder = (): Action => ({ type: 'chat', enabled: false, params: { text: SETUP_RUN_TEXT } });
export function isSetupRunPlaceholder(action: Action): boolean {
  const disabledChat = action.type === 'chat' && action.enabled === false;
  return disabledChat && action.params?.text === SETUP_RUN_TEXT;
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

const TIME_LEFT_PARAMS = [
  ['leftCommand', 'left_command'],
  ['leftReply', 'left_reply'],
  ['readyReply', 'ready_reply']
] as const;

export function timeLeftParams(config: FlowConfig): Params {
  return Object.fromEntries(TIME_LEFT_PARAMS.filter(([key]) => str(config[key]).trim() !== '').map(([key, param]) => [param, str(config[key]).trim()]));
}

export function timeLeftConfig(params: Params): FlowConfig {
  return Object.fromEntries(TIME_LEFT_PARAMS.filter(([, param]) => str(params[param]) !== '').map(([key, param]) => [key, str(params[param])]));
}

export type NamedTimerActionType = 'countdown_stop' | 'cooldown_reset';

export const nodePath = (id: string) => `nodes.${id}`;

export function triggerKind(trigger: Trigger): string {
  if (trigger.type === 'reward') return REWARD_KIND;
  return trigger.type === 'event' ? str(trigger.params?.kind) : '';
}

export function amountUnit(trigger: Trigger): 'bits' | 'gifts' | undefined {
  const tokens = EVENT_TOKENS[triggerKind(trigger)];
  if (tokens?.bits === 'amount') return 'bits';
  if (tokens?.count === 'amount') return 'gifts';
}

const NESTED_PARAMS = ['then', 'else', 'on_ok', 'on_failure', 'actions'] as const;

type Arm = { key: string; list: Action[] };

export function armsOf(action: Action): Arm[] {
  const params = action.params ?? {};
  const named = NESTED_PARAMS.map((key): Arm => ({ key, list: (params[key] ?? []) as Action[] }));
  const branches = (params.branches ?? []) as { actions?: Action[] }[];
  return [...named, ...branches.map((branch, b): Arm => ({ key: `branches[${b}].actions`, list: branch.actions ?? [] }))];
}
