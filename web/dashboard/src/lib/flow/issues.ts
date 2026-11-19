// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { ACTION_TYPES, CONDITION_TYPES, FLOW_ISSUE, RANDOM_PATHS, TRIGGER_TYPES, chatModeValueField, cooldownWindows, needsSearchValue, variableActionType, viewerList, type Issue } from '@bagel/kit/automations';
import { CODE, RECIPE_NAME_PATTERN } from '@bagel/kit/automations/limits';
import { specFor, targetFieldFor, targetMode, type FieldSpec, type FlowNode } from './model';
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
  global_s: 'channelSeconds', user_s: 'viewerSeconds', ms: 'seconds', allow: 'login', allow_ids: 'login', userId: 'login', user_id: 'login',
  enabled: 'setEnabled', reset_on_stream_end: 'reset', automation_id: 'automationId',
  kelvin: 'kelvin', brightness: 'brightness', hold_s: 'holdS', case_sensitive: 'caseSensitive', sources: 'subMessages'
};

const NODE_FIELDS: Record<string, Record<string, string>> = {
  'twitch.chat.command': { name: 'trigger' },
  'twitch.chat.word': { name: 'word' },
  'twitch.sub': { kind: 'variant', filters: 'variant', 'filters.tier': 'tier', 'filters.months': 'minMonths', 'filters.streak': 'minStreak' },
  'twitch.sub_gift': { filters: 'minGifts', 'filters.tier': 'tier', 'filters.anonymous': 'gifter' },
  'twitch.cheer': { filters: 'minBits', 'filters.anonymous': 'cheerer' },
  'twitch.raid': { filters: 'minViewers' },
  'twitch.reward': { title: 'reward', cost: 'reward', color: 'reward', cooldown_s: 'reward', on_redeem: 'reward', input_required: 'reward' },
  'twitch.ad_break': { filters: 'minLength', 'filters.automatic': 'startedBy', 'filters.seconds': 'minLength' },
  'twitch.channel_update': { filters: 'category', 'filters.category': 'category' },
  'schedule.timer': { interval_s: 'every', random_jitter_s: 'jitter', min_chat_lines: 'chatLines', offline_ok: 'offline' },
  'reply.chat': { text: 'message', thread: 'reply' },
  'reply.poll': { points_per_vote: 'pointsPerVote' },
  'reply.prediction_end': { winner: 'outcome' },
  'reply.counter': { name: 'counter' },
  'cond.user': { allow: 'users', deny: 'users', allow_ids: 'users', deny_ids: 'users', userId: 'users', user_id: 'users', login: 'users' },
  'cond.category': { allow: 'games', deny: 'games' },
  'cond.follow': { min_days: 'minDays' },
  'cond.accountAge': { min_days: 'minDays' },
  'cond.counter': { name: 'counter' },
  'reply.redemption': { status: 'result' }
};

const WHOLE_NODE_FIELDS: Record<string, string> = { 'cond.cooldown': 'channelSeconds', 'cond.user': 'users', 'cond.chance': 'percent', 'cond.category': 'games' };

function fieldKeyFor(node: FlowNode, path: IssuePath): FieldKey | undefined {
  const configPath = path.replace(/^(?:params|config)(?:\.|$)/, '');
  if (node.type === 'reply.random') return randomField(configPath);
  const [param, child] = configPath.split(/[.\[]/);
  const filterKey = param === 'filters' ? NODE_FIELDS[node.type]?.[`filters.${child}`] : undefined;
  const key = filterKey ?? (param ? parameterField(node, param) : WHOLE_NODE_FIELDS[node.type]);
  return specFor(node.type)?.fields.some((field) => field.key === key) ? key : undefined;
}

function randomField(path: IssuePath): FieldKey | undefined {
  const weight = /^branches\[(\d+)\]\.weight$/.exec(path);
  if (weight) return RANDOM_PATHS[Number(weight[1])]?.weightKey;
  return path === 'branches' ? 'paths' : undefined;
}

function parameterField(node: FlowNode, param: FieldKey): FieldKey | undefined {
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
  const fieldKey = issue.code === FLOW_ISSUE.unknownPort ? undefined : fieldKeyFor(target.node, path);
  return [{ ...issue, nodeId: target.node.id, fieldKey }];
}

const TYPES: Record<string, string> = {
  'twitch.chat.command': 'command', 'twitch.chat.word': 'command', 'twitch.reward': 'reward', 'countdown.finished': 'countdown',
  'schedule.timer': 'timer', 'schedule.manual': 'manual',
  'cond.role': 'perm', 'cond.user': 'users', 'reply.points': 'points_add', 'reply.wait': 'delay'
};

const REQUIRED: Record<string, readonly string[]> = { 'cond.user': ['users'], 'cond.cooldown': ['channelSeconds'] };
const searchValueRequired = (config: FlowNode['config']) => needsSearchValue(config.op);

const REQUIRED_IF: Record<string, (config: FlowNode['config']) => boolean> = {
  'cond.compare:right': searchValueRequired,
  'cond.var:value': searchValueRequired,
  'reply.prediction_end:outcome': (config) => config.status === 'resolve',
  'reply.lights:scene': (config) => config.mode === 'scene'
};

const POINTS_NODES = new Set(['reply.points', 'reply.points_set']);
const INLINE_NUMBER_NODES = new Set(['reply.points', 'reply.counter']);
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

const DYNAMIC_TYPES: Record<string, (config: FlowNode['config']) => string> = {
  'reply.var': variableActionType,
  'reply.counter': (config) => config.change === 'set' ? 'counter_set' : 'counter_add'
};

function catalogRequires(node: FlowNode, key: FieldKey): boolean {
  const type = DYNAMIC_TYPES[node.type]?.(node.config) ?? TYPES[node.type] ?? node.type.split('.').slice(1).join('.');
  const catalog = [...TRIGGER_TYPES, ...CONDITION_TYPES, ...ACTION_TYPES].find((item) => item.type === type);
  return catalog?.fields.some((field) => field.required && fieldKeyFor(node, field.name) === key) ?? false;
}

export function fieldRequired(node: FlowNode, key: FieldKey): boolean {
  const field = specFor(node.type)?.fields.find((entry) => entry.key === key);
  return field?.required ?? targetRequired(node, key) ?? builderRule(node, key) ?? catalogRequires(node, key);
}

function inlineNumberCode(node: FlowNode, field: Extract<FieldSpec, { type: 'number' }>): Issue['code'] | undefined {
  if (field.whenTrigger !== 'amount' && !INLINE_NUMBER_NODES.has(node.type)) return undefined;
  const raw = configText(node, field.key).trim();
  if (!raw) return field.required ? CODE.required : undefined;
  const value = Number(raw);
  const legacyPoints = node.type === 'reply.points' && node.config.change === undefined;
  const amount = legacyPoints ? Math.abs(value) : value;
  const counterDelta = node.type === 'reply.counter' && field.key === 'amount';
  if (counterDelta && amount === 0) return CODE.outOfRange;
  if (amount < (field.min ?? -Infinity) || amount > (field.max ?? Infinity)) return CODE.outOfRange;
}

function missingRewardChange(node: FlowNode, field: FieldKey): boolean {
  if (node.type !== 'reply.reward_update' || field !== 'setEnabled') return false;
  const { setEnabled, paused } = node.config;
  const togglesKept = !['on', 'off'].includes(String(setEnabled)) && !['pause', 'resume'].includes(String(paused));
  return togglesKept && !['cost', 'cooldown'].some((key) => configText(node, key).trim());
}

function inlineCode(node: FlowNode, field: FieldSpec): Issue['code'] | undefined {
  if (field.type === 'number') return inlineNumberCode(node, field);
  if (missingRewardChange(node, field.key)) return CODE.required;
  if (node.type !== 'reply.countdown_start' || field.key !== 'name') return undefined;
  const value = configText(node, field.key).trim();
  if (value && !RECIPE_NAME_PATTERN.test(value)) return CODE.simpleWord;
}

export function inlineFieldIssue(node: FlowNode, field: FieldSpec): FieldIssue | undefined {
  const code = inlineCode(node, field);
  return code ? { nodeId: node.id, fieldKey: field.key, path: `nodes.${node.id}.config.${field.key}`, code } : undefined;
}

export function missingField(node: FlowNode): string | undefined {
  if (node.type === 'cond.user') return viewerList(node.config).length ? undefined : 'users';
  if (node.type === 'cond.cooldown') {
    const { channelSeconds, viewerSeconds } = cooldownWindows(node.config);
    return channelSeconds > 0 || viewerSeconds > 0 ? undefined : 'channelSeconds';
  }
  const spec = specFor(node.type);
  return spec?.fields.find(({ key }) => {
    const stored = key === 'login' && targetFieldFor(spec) ? 'userId' : key;
    return fieldRequired(node, key) && !configText(node, stored).trim();
  })?.key;
}
