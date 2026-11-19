// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { ACTION_TYPES, CONDITION_TYPES, TRIGGER_TYPES, TEXT_ARG_FIELDS, chatModeValueField, type Issue } from '@bagel/kit/automations';
import { specFor, targetFieldFor, targetMode, type FlowNode } from './model';
import { configText } from './setup';

type FieldKey = string;
type IssuePath = string;

export type FieldIssue = Issue & { nodeId: string; fieldKey?: FieldKey };

const CODE_FIELDS = new Map([['user_not_resolved', 'login'], ['wait_too_long', 'seconds'], ['delay_budget', 'seconds']]);

const PARAM_FIELDS: Record<string, string> = {
  save_as: 'saveAs', json_path: 'jsonPath', left_command: 'leftCommand', left_reply: 'leftReply', ready_reply: 'readyReply',
  reward_id: 'reward', cooldown_s: 'cooldown', on_redeem: 'onRedeem', input_required: 'userInput',
  device_name: 'device', sku: 'device', extra_lights: 'device', any_time: 'liveOnly',
  scene_id: 'scene', scene_name: 'scene', scene_type: 'scene', scene_instance: 'scene', scene_value: 'scene',
  global_s: 'seconds', user_s: 'seconds', ms: 'seconds', allow: 'login', allow_ids: 'login', userId: 'login', user_id: 'login',
  enabled: 'setEnabled', reset_on_stream_end: 'reset'
};

const NODE_FIELDS: Record<string, Record<string, string>> = {
  'twitch.chat.command': { name: 'trigger', aliases: 'trigger' },
  'twitch.chat.word': { name: 'word' },
  'twitch.sub': { kind: 'variant', filters: 'variant' },
  'twitch.cheer': { filters: 'minBits' },
  'twitch.raid': { filters: 'minViewers' },
  'twitch.reward': { title: 'reward', cost: 'reward', color: 'reward', cooldown_s: 'reward', on_redeem: 'reward', input_required: 'reward' },
  'reply.chat': { text: 'message', thread: 'reply' },
  'reply.poll': { points_per_vote: 'pointsPerVote' },
  'reply.prediction_end': { winner: 'outcome' },
  'reply.counter': { name: 'counter' }
};

const WHOLE_NODE_FIELDS: Record<string, string> = { 'cond.cooldown': 'seconds', 'cond.user': 'login' };

function fieldKeyFor(node: FlowNode, path: IssuePath): FieldKey | undefined {
  const param = path.replace(/^(?:params|config)(?:\.|$)/, '').split(/[.\[]/)[0];
  const key = param ? parameterField(node, param) : WHOLE_NODE_FIELDS[node.type];
  return specFor(node.type)?.fields.some((field) => field.key === key) ? key : undefined;
}

function parameterField(node: FlowNode, param: FieldKey): FieldKey | undefined {
  if (isTextArgument(node, param)) return TEXT_ARG_FIELDS.get(String(node.config.op))?.[Number(param.slice(-1)) - 1];
  if (node.type === 'reply.chat_mode' && param === 'value') return chatModeValueField(node.config.mode);
  const override = NODE_FIELDS[node.type]?.[param];
  if (override) return override;
  if (param === 'login') return loginField(node);
  return PARAM_FIELDS[param] ?? param;
}

function loginField(node: FlowNode): FieldKey {
  if (node.config.who === 'user') return 'login';
  return specFor(node.type)?.fields.some((field) => field.key === 'target') ? 'target' : 'login';
}

function isTextArgument(node: FlowNode, param: FieldKey): boolean {
  return node.type === 'reply.text' && /^arg[12]$/.test(param);
}

export function fieldIssues(issues: readonly Issue[], nodes: readonly FlowNode[]): FieldIssue[] {
  const trigger = nodes.find((node) => specFor(node.type)?.kind === 'trigger');
  return issues.flatMap((issue) => locateFieldIssue(issue, nodes, trigger));
}

type IssueTarget = { node: FlowNode; path: IssuePath };

function issueTarget(issue: Issue, nodes: readonly FlowNode[], trigger: FlowNode | undefined): IssueTarget | undefined {
  const match = /^nodes\.([^.]+)(?:\.(.*))?$/.exec(issue.path);
  if (match) {
    const node = nodes.find((entry) => entry.id === match[1]);
    return node && { node, path: match[2] ?? '' };
  }
  const onTrigger = issue.path.startsWith('trigger') ? trigger : undefined;
  return onTrigger && { node: onTrigger, path: issue.path.replace(/^trigger\.?/, '') };
}

function locateFieldIssue(issue: Issue, nodes: readonly FlowNode[], trigger: FlowNode | undefined): FieldIssue[] {
  const target = issueTarget(issue, nodes, trigger);
  if (!target) return [];
  const path = target.path || (CODE_FIELDS.get(issue.code) ?? '');
  return [{ ...issue, nodeId: target.node.id, fieldKey: fieldKeyFor(target.node, path) }];
}

const TYPES: Record<string, string> = {
  'twitch.chat.command': 'command', 'twitch.chat.word': 'command', 'twitch.reward': 'reward', 'countdown.finished': 'countdown',
  'cond.role': 'perm', 'cond.user': 'users', 'reply.points': 'points_add', 'reply.counter': 'counter_add', 'reply.wait': 'delay'
};

const REQUIRED: Record<string, readonly string[]> = { 'cond.user': ['login'], 'cond.cooldown': ['seconds'] };
const REQUIRED_IF: Record<string, (config: FlowNode['config']) => boolean> = {
  'cond.compare:right': (config) => config.op === 'contains',
  'reply.prediction_end:outcome': (config) => config.status === 'resolve',
  'reply.lights:scene': (config) => config.mode === 'scene'
};

const POINTS_NODES = new Set(['reply.points', 'reply.points_set']);
const TARGET_REQUIRED = new Map<FieldKey, (node: FlowNode) => boolean>([
  ['login', (node) => targetMode(node.config) === 'user'],
  ['target', (node) => targetMode(node.config) === 'login' && !POINTS_NODES.has(node.type)]
]);

function targetRequired(node: FlowNode, key: FieldKey): boolean | undefined {
  if (!targetFieldFor(specFor(node.type))) return undefined;
  return TARGET_REQUIRED.get(key)?.(node);
}

function builderRule(node: FlowNode, key: FieldKey): boolean | undefined {
  const required = REQUIRED[node.type];
  return required ? required.includes(key) : REQUIRED_IF[`${node.type}:${key}`]?.(node.config);
}

function catalogRequires(node: FlowNode, key: FieldKey): boolean {
  const type = TYPES[node.type] ?? node.type.split('.').slice(1).join('.');
  const catalog = [...TRIGGER_TYPES, ...CONDITION_TYPES, ...ACTION_TYPES].find((item) => item.type === type);
  return catalog?.fields.some((field) => field.required && fieldKeyFor(node, field.name) === key) ?? false;
}

export function fieldRequired(node: FlowNode, key: FieldKey): boolean {
  const field = specFor(node.type)?.fields.find((entry) => entry.key === key);
  return field?.required ?? targetRequired(node, key) ?? builderRule(node, key) ?? catalogRequires(node, key);
}

export function missingField(node: FlowNode): string | undefined {
  const spec = specFor(node.type);
  return spec?.fields.find(({ key }) => {
    const stored = key === 'login' && targetFieldFor(spec) ? 'userId' : key;
    return fieldRequired(node, key) && !configText(node, stored).trim();
  })?.key;
}
