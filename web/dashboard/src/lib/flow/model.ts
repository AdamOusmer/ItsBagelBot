// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { STREAM_STEP_DEFINITIONS, TARGET_FIELDS } from './stream-steps';
import { graphIndex, portKey } from './graph-index';
import type { ChoiceSource } from './choices';
import type { FlowIconName as IconName } from './icons';
import { activeRandomPaths, RANDOM_PATH_COUNTS, RANDOM_PATH_DEFAULT_WEIGHT, RANDOM_PATHS, TEXT_OPERAND_OPS, TEXT_OPS, VAR_OPS_WITH_VALUE } from '@bagel/kit/automations';
import { MAX_COOLDOWN_SECONDS, MAX_DELAY_MS, MAX_LIGHTS_BRIGHTNESS, MAX_LIGHTS_HOLD_S, MAX_LIGHTS_KELVIN, MAX_REWARD_COST, MIN_LIGHTS_BRIGHTNESS, MIN_LIGHTS_KELVIN } from '@bagel/kit/automations/limits';
import { ARGS_SAMPLE, GAME_SAMPLE, TITLE_SAMPLE, TOUSER_SAMPLE, USERID_SAMPLE, USER_SAMPLE } from '@bagel/kit/variables/preview-values';

export const MAX_WAIT_SECONDS = MAX_DELAY_MS / 1000;
export const INITIAL_LIGHT_KELVIN = 4000;

export type FlowKind = 'trigger' | 'condition' | 'reply';

export const STEP_GROUPS = ['chat', 'community', 'moderation', 'stream', 'loyalty', 'timers', 'lights', 'logic'] as const;
export type StepGroup = (typeof STEP_GROUPS)[number];

export const STEP_PLACEMENT = {
  'twitch.chat.command': { kind: 'trigger', group: 'chat' },
  'twitch.chat.word': { kind: 'trigger', group: 'chat' },
  'reply.chat': { kind: 'reply', group: 'chat' },
  'reply.shoutout': { kind: 'reply', group: 'chat' },
  'reply.announce': { kind: 'reply', group: 'chat' },
  'reply.reply': { kind: 'reply', group: 'chat' },
  'reply.poll': { kind: 'reply', group: 'chat' },
  'reply.poll_end': { kind: 'reply', group: 'chat' },
  'reply.prediction': { kind: 'reply', group: 'chat' },
  'reply.prediction_end': { kind: 'reply', group: 'chat' },
  'reply.chat_mode': { kind: 'reply', group: 'chat' },
  'reply.clear_chat': { kind: 'reply', group: 'chat' },
  'reply.delete_message': { kind: 'reply', group: 'chat' },
  'reply.pin': { kind: 'reply', group: 'chat' },
  'reply.whisper': { kind: 'reply', group: 'chat' },
  'twitch.follow': { kind: 'trigger', group: 'community' },
  'twitch.sub': { kind: 'trigger', group: 'community' },
  'twitch.sub_gift': { kind: 'trigger', group: 'community' },
  'twitch.cheer': { kind: 'trigger', group: 'community' },
  'twitch.raid': { kind: 'trigger', group: 'community' },
  'twitch.unban_request': { kind: 'trigger', group: 'moderation' },
  'cond.role': { kind: 'condition', group: 'moderation' },
  'cond.user': { kind: 'condition', group: 'moderation' },
  'reply.timeout': { kind: 'reply', group: 'moderation' },
  'reply.unban': { kind: 'reply', group: 'moderation' },
  'reply.ban': { kind: 'reply', group: 'moderation' },
  'reply.mod': { kind: 'reply', group: 'moderation' },
  'reply.vip': { kind: 'reply', group: 'moderation' },
  'reply.unban_resolve': { kind: 'reply', group: 'moderation' },
  'twitch.stream.online': { kind: 'trigger', group: 'stream' },
  'twitch.stream.offline': { kind: 'trigger', group: 'stream' },
  'twitch.ad_break': { kind: 'trigger', group: 'stream' },
  'twitch.channel_update': { kind: 'trigger', group: 'stream' },
  'reply.raid': { kind: 'reply', group: 'stream' },
  'reply.raid_cancel': { kind: 'reply', group: 'stream' },
  'reply.ad_snooze': { kind: 'reply', group: 'stream' },
  'reply.stream_info': { kind: 'reply', group: 'stream' },
  'twitch.reward': { kind: 'trigger', group: 'loyalty' },
  'reply.points': { kind: 'reply', group: 'loyalty' },
  'reply.points_set': { kind: 'reply', group: 'loyalty' },
  'reply.counter': { kind: 'reply', group: 'loyalty' },
  'reply.reward_update': { kind: 'reply', group: 'loyalty' },
  'countdown.finished': { kind: 'trigger', group: 'timers' },
  'schedule.timer': { kind: 'trigger', group: 'timers' },
  'cond.cooldown': { kind: 'condition', group: 'timers' },
  'reply.countdown_start': { kind: 'reply', group: 'timers' },
  'reply.countdown_add': { kind: 'reply', group: 'timers' },
  'reply.cooldown_add': { kind: 'reply', group: 'timers' },
  'reply.lights': { kind: 'reply', group: 'lights' },
  'schedule.manual': { kind: 'trigger', group: 'logic' },
  'cond.compare': { kind: 'condition', group: 'logic' },
  'cond.chance': { kind: 'condition', group: 'logic' },
  'cond.var': { kind: 'condition', group: 'logic' },
  'reply.wait': { kind: 'reply', group: 'logic' },
  'reply.loop': { kind: 'reply', group: 'logic' },
  'reply.var': { kind: 'reply', group: 'logic' },
  'reply.text': { kind: 'reply', group: 'logic' },
  'reply.math': { kind: 'reply', group: 'logic' },
  'reply.http_get': { kind: 'reply', group: 'logic' },
  'reply.http_post': { kind: 'reply', group: 'logic' },
  'reply.commercial': { kind: 'reply', group: 'stream' },
  'reply.marker': { kind: 'reply', group: 'stream' },
  'reply.clip': { kind: 'reply', group: 'stream' },
  'reply.stop': { kind: 'reply', group: 'logic' },
  'reply.random': { kind: 'reply', group: 'logic' },
  'reply.run': { kind: 'reply', group: 'logic' }
} as const satisfies Record<string, { kind: FlowKind; group: StepGroup }>;

type Typed = { key: string; placeholder?: true };

type FieldGate = { key: string; value: string | readonly string[] | boolean };

export type FieldSpec = { when?: FieldGate | readonly FieldGate[]; labelBy?: string; initial?: string | number | boolean; required?: true } & (
  | (Typed & { type: 'text' })
  | (Typed & { type: 'template'; singleLine?: boolean; max?: number })
  | (Typed & { type: 'number'; min?: number; max?: number; step?: number; unit?: true })
  | { key: string; type: 'slider'; min: number; max: number; step: number; unit?: string }
  | { key: string; type: 'duration'; min: number; max: number }
  | { key: string; type: 'select'; options: readonly string[]; fallback?: string }
  | { key: string; type: 'toggle' }
  | { key: string; type: 'color' }
  | (Typed & { type: 'login' })
  | { key: 'who'; type: 'target' }
  | { key: string; type: 'goveeLight' }
  | { key: string; type: 'choice'; source: ChoiceSource; nameKey?: string }
  | { key: string; type: 'reward' }
);

export const LINKED_REWARD_CHOICE = { key: 'reward', type: 'choice', source: 'rewards', nameKey: 'rewardTitle' } as const;

export type VariableSpec = { token: string; name: string; sample: string; literalName?: boolean };

export type OutputSpec = { id: string; provides?: readonly VariableSpec[] };

export type NodeSpec = {
  type: string;
  slug: string;
  kind: FlowKind;
  group: StepGroup;
  icon: IconName;
  fields: readonly FieldSpec[];
  hidden?: true;
  lineBy?: string;
  provides?: readonly VariableSpec[];
  providesFor?(config: FlowNode['config']): readonly VariableSpec[];
  outputs?: readonly OutputSpec[];
  outputsFor?(config: FlowNode['config']): readonly OutputSpec[];
};

export type StepDefinition = Omit<NodeSpec, 'kind' | 'group' | 'type'> & { type: keyof typeof STEP_PLACEMENT };

export type FlowNode = {
  id: string;
  type: string;
  x: number;
  y: number;
  config: Record<string, string | number | boolean>;
};

export type FlowEdge = { id: string; from: string; port: string; to: string };

export type Flow = { id: string; name: string; enabled: boolean; nodes: FlowNode[]; edges: FlowEdge[]; queue?: string };

export const GRID = 24;
export const NODE_W = 288;
export const COLUMN_GAP = 80;
export const ROW_GAP = 28;

const user: VariableSpec[] = [
  { token: '{user}', name: 'user', sample: USER_SAMPLE },
  { token: '{user.id}', name: 'userId', sample: USERID_SAMPLE }
];

const subVariables: Record<string, readonly VariableSpec[]> = {
  resub: [
    { token: '{months}', name: 'months', sample: '14' },
    { token: '{streak}', name: 'streak', sample: '6' },
    { token: '{message}', name: 'message', sample: 'GG!' }
  ],
  gift: [
    { token: '{count}', name: 'count', sample: '5' },
    { token: '{total}', name: 'total', sample: '20' }
  ]
};

const subViewer: readonly VariableSpec[] = [...user, { token: '{tier}', name: 'tier', sample: '1000' }];
const args: VariableSpec = { token: '{args}', name: 'args', sample: ARGS_SAMPLE };
const streamInfo: readonly VariableSpec[] = [
  { token: '{title}', name: 'title', sample: TITLE_SAMPLE },
  { token: '{game}', name: 'game', sample: GAME_SAMPLE }
];

const matchCase: FieldSpec = { key: 'caseSensitive', type: 'toggle', initial: false };
const subMessages: FieldSpec = { key: 'subMessages', type: 'toggle', initial: false };
const subTier: FieldSpec = { key: 'tier', type: 'select', options: ['any', '1000', '2000', '3000'], initial: 'any' };
const resubOnly: FieldGate = { key: 'variant', value: 'resub' };
const eventActors = ['anyone', 'named', 'anonymous'] as const;

const yesNo: OutputSpec[] = [{ id: 'yes' }, { id: 'no' }];

const apiPorts: OutputSpec[] = [
  { id: 'ok' },
  { id: 'failure', provides: [{ token: '{local:error}', name: 'apiError', sample: '503' }] }
];

const loopPorts: OutputSpec[] = [
  { id: 'body', provides: [{ token: '{local:index}', name: 'loopIndex', sample: '1' }] },
  { id: 'done' }
];

const varScope: FieldSpec = { key: 'scope', type: 'select', options: ['channel', 'viewer'] };
const varName: FieldSpec = { key: 'name', type: 'text', placeholder: true };
const saveAs: FieldSpec = { key: 'saveAs', type: 'text', placeholder: true };
const textOperands: FieldSpec[] = Object.entries(TEXT_OPERAND_OPS).map(([key, ops]) => ({
  key, type: 'template', singleLine: true, labelBy: 'op', when: { key: 'op', value: ops }
}));

export const newFlow = (name: string): Flow => ({ id: 'new', name, enabled: true, nodes: [], edges: [] });

const STEP_DEFINITIONS: readonly StepDefinition[] = [
  {
    type: 'countdown.finished', slug: 'countdownFinished', icon: 'timer',
    fields: [{ key: 'name', type: 'choice', source: 'namedCountdowns' }]
  },
  {
    type: 'schedule.timer', slug: 'timer', icon: 'repeat',
    fields: [
      { key: 'every', type: 'duration', min: 60, max: 86_400, initial: 900, required: true },
      { key: 'jitter', type: 'duration', min: 0, max: 3600, initial: 0 },
      { key: 'chatLines', type: 'number', min: 0, max: 100, initial: 5 },
      { key: 'offline', type: 'toggle', initial: false }
    ]
  },
  {
    type: 'schedule.manual', slug: 'manual', icon: 'send',
    fields: [],
    provides: [args]
  },
  {
    type: 'reply.countdown_start', slug: 'countdownStart', icon: 'timer',
    fields: [
      { key: 'name', type: 'text', placeholder: true },
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS, initial: 3_600 },
      { key: 'leftCommand', type: 'text', placeholder: true },
      { key: 'leftReply', type: 'template', placeholder: true, singleLine: true },
      { key: 'readyReply', type: 'template', placeholder: true, singleLine: true }
    ],
    provides: [{ token: '{countdown:left}', name: 'countdownLeft', sample: '2d 3h' }]
  },
  {
    type: 'reply.countdown_add', slug: 'countdownAdd', icon: 'timer',
    fields: [
      { key: 'name', type: 'choice', source: 'namedCountdowns' },
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS, initial: 60 },
      { key: 'per', type: 'number', min: 0 }
    ],
    provides: [{ token: '{countdown:left}', name: 'countdownLeft', sample: '2d 3h' }]
  },
  {
    type: 'twitch.chat.command',
    slug: 'chatCommand',
    icon: 'commands',
    fields: [
      { key: 'trigger', type: 'text', placeholder: true, required: true },
      { key: 'aliases', type: 'text', placeholder: true },
      { key: 'match', type: 'select', options: ['start', 'exact'], initial: 'start' },
      matchCase,
      subMessages
    ],
    provides: [...user, args, { token: '{touser}', name: 'touser', sample: TOUSER_SAMPLE }]
  },
  {
    type: 'twitch.chat.word',
    slug: 'chatWord',
    icon: 'messageSquare',
    fields: [
      { key: 'word', type: 'text', placeholder: true, required: true, labelBy: 'match' },
      { key: 'match', type: 'select', options: ['word', 'anywhere', 'pattern'], initial: 'word' },
      matchCase,
      subMessages
    ],
    provides: [...user, { ...args, name: 'wholeMessage' }]
  },
  {
    type: 'twitch.follow',
    slug: 'follow',
    icon: 'heart',
    fields: [],
    provides: user
  },
  {
    type: 'twitch.sub',
    slug: 'sub',
    icon: 'star',
    fields: [
      { key: 'variant', type: 'select', options: ['any', 'new', 'gifted', 'resub'], initial: 'any' },
      subTier,
      { key: 'minMonths', type: 'number', min: 1, unit: true, when: resubOnly },
      { key: 'minStreak', type: 'number', min: 1, unit: true, when: resubOnly }
    ],
    provides: [...subViewer, ...subVariables.resub],
    providesFor: (config) => [...subViewer, ...(subVariables[String(config.variant)] ?? [])]
  },
  {
    type: 'twitch.sub_gift',
    slug: 'subGift',
    icon: 'star',
    fields: [
      { key: 'minGifts', type: 'number', min: 1, unit: true },
      subTier,
      { key: 'gifter', type: 'select', options: eventActors, initial: 'anyone' }
    ],
    provides: [...subViewer, ...subVariables.gift]
  },
  {
    type: 'twitch.cheer',
    slug: 'cheer',
    icon: 'gem',
    fields: [
      { key: 'minBits', type: 'number', min: 1, unit: true },
      { key: 'cheerer', type: 'select', options: eventActors, initial: 'anyone' }
    ],
    provides: [...user, { token: '{bits}', name: 'bits', sample: '500' }, { token: '{message}', name: 'message', sample: 'GG!' }]
  },
  {
    type: 'twitch.raid',
    slug: 'raid',
    icon: 'swords',
    fields: [{ key: 'minViewers', type: 'number', min: 1, unit: true }],
    provides: [...user, { token: '{viewers}', name: 'viewers', sample: '42' }]
  },
  {
    type: 'twitch.reward',
    slug: 'reward',
    icon: 'ticket',
    fields: [{ key: 'reward', type: 'reward' }],
    provides: [
      ...user,
      { token: '{input}', name: 'input', sample: 'lofi beats' },
      { token: '{reward}', name: 'reward', sample: 'VIP' },
      { token: '{cost}', name: 'cost', sample: '500' }
    ]
  },
  {
    type: 'twitch.stream.online',
    slug: 'streamOnline',
    icon: 'radio',
    fields: [],
    provides: streamInfo
  },
  {
    type: 'twitch.ad_break', slug: 'adBreak', icon: 'timer',
    fields: [
      { key: 'startedBy', type: 'select', options: ['any', 'automatic', 'manual'], initial: 'any' },
      { key: 'minLength', type: 'duration', min: 30, max: 180 }
    ],
    provides: [{ token: '{seconds}', name: 'seconds', sample: '90' }]
  },
  {
    type: 'twitch.channel_update', slug: 'channelUpdate', icon: 'radio',
    fields: [{ key: 'category', type: 'text', placeholder: true }],
    provides: streamInfo
  },
  {
    type: 'cond.role',
    slug: 'role',
    icon: 'moderation',
    fields: [{ key: 'level', type: 'select', options: ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'], initial: 'mod' }],
    outputs: yesNo
  },
  {
    type: 'cond.user',
    slug: 'user',
    icon: 'users',
    fields: [{ key: 'login', type: 'login', placeholder: true }],
    outputs: yesNo
  },
  {
    type: 'cond.cooldown',
    slug: 'cooldown',
    icon: 'hourglass',
    fields: [
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS, initial: 30 },
      { key: 'perUser', type: 'toggle' },
      { key: 'name', type: 'text', placeholder: true, when: { key: 'perUser', value: false } },
      { key: 'leftCommand', type: 'text', placeholder: true },
      { key: 'leftReply', type: 'template', placeholder: true, singleLine: true },
      { key: 'readyReply', type: 'template', placeholder: true, singleLine: true }
    ],
    outputs: [{ id: 'yes' }, { id: 'no', provides: [{ token: '{cooldown:left}', name: 'cooldownLeft', sample: '2d 3h' }] }]
  },
  {
    type: 'cond.compare',
    slug: 'compare',
    icon: 'scale',
    fields: [
      { key: 'left', type: 'template', placeholder: true, singleLine: true },
      { key: 'op', type: 'select', options: ['gte', 'lt', 'eq', 'contains'] },
      { key: 'right', type: 'template', placeholder: true, singleLine: true }
    ],
    outputs: yesNo
  },
  {
    type: 'cond.chance',
    slug: 'chance',
    icon: 'dice',
    fields: [{ key: 'percent', type: 'number', min: 1, max: 100, unit: true, initial: 50 }],
    outputs: yesNo
  },
  {
    type: 'cond.var',
    slug: 'var',
    icon: 'braces',
    fields: [
      varScope,
      varName,
      { key: 'op', type: 'select', options: [...VAR_OPS_WITH_VALUE, 'exists'], initial: 'exists' },
      { key: 'value', type: 'template', placeholder: true, singleLine: true, when: { key: 'op', value: VAR_OPS_WITH_VALUE } }
    ],
    outputs: yesNo
  },
  {
    type: 'reply.chat',
    slug: 'chat',
    icon: 'send',
    fields: [
      { key: 'message', type: 'template', placeholder: true },
      { key: 'reply', type: 'select', options: ['off', 'mention', 'thread'], initial: 'off' }
    ]
  },
  {
    type: 'reply.points',
    slug: 'points',
    icon: 'coins',
    fields: [{ key: 'amount', type: 'number', unit: true, initial: 10 }, ...TARGET_FIELDS]
  },
  {
    type: 'reply.points_set',
    slug: 'pointsSet',
    icon: 'coins',
    fields: [{ key: 'amount', type: 'number', unit: true, initial: 10 }, ...TARGET_FIELDS]
  },
  {
    type: 'reply.counter',
    slug: 'counter',
    icon: 'hash',
    fields: [{ key: 'counter', type: 'choice', source: 'counters' }],
    provides: [{ token: '{count}', name: 'count', sample: '17' }]
  },
  {
    type: 'reply.lights',
    slug: 'lights',
    icon: 'bulb',
    fields: [
      { key: 'device', type: 'goveeLight' },
      { key: 'mode', type: 'select', options: ['color', 'scene', 'white', 'on', 'off'] },
      { key: 'color', type: 'color', when: { key: 'mode', value: 'color' } },
      { key: 'scene', type: 'choice', source: 'scenes', nameKey: 'sceneName', when: { key: 'mode', value: 'scene' } },
      { key: 'kelvin', type: 'slider', min: MIN_LIGHTS_KELVIN, max: MAX_LIGHTS_KELVIN, step: 100, unit: 'K', initial: INITIAL_LIGHT_KELVIN, when: { key: 'mode', value: 'white' } },
      { key: 'brightness', type: 'number', min: MIN_LIGHTS_BRIGHTNESS, max: MAX_LIGHTS_BRIGHTNESS, unit: true, placeholder: true, when: { key: 'mode', value: ['color', 'scene', 'white', 'on'] } },
      { key: 'holdS', type: 'duration', min: 0, max: MAX_LIGHTS_HOLD_S },
      { key: 'liveOnly', type: 'toggle', initial: true }
    ]
  },
  {
    type: 'reply.wait',
    slug: 'wait',
    icon: 'timer',
    fields: [{ key: 'seconds', type: 'number', min: 0.1, max: MAX_WAIT_SECONDS, step: 0.1, unit: true, initial: 5 }]
  },
  {
    type: 'reply.loop',
    slug: 'loop',
    icon: 'repeat',
    fields: [{ key: 'times', type: 'template', placeholder: true, singleLine: true, initial: '3' }],
    outputs: loopPorts
  },
  {
    type: 'reply.var',
    slug: 'varChange',
    icon: 'braces',
    lineBy: 'do',
    fields: [
      { key: 'do', type: 'select', options: ['set', 'add', 'subtract', 'forget'], initial: 'set' },
      { ...varScope, initial: 'channel' },
      varName,
      { key: 'value', type: 'template', placeholder: true, singleLine: true, when: { key: 'do', value: 'set' } },
      { key: 'amount', type: 'number', min: 1, step: 1, initial: 1, when: { key: 'do', value: ['add', 'subtract'] } }
    ],
    providesFor: changedVariable
  },
  {
    type: 'reply.text',
    slug: 'text',
    icon: 'letters',
    fields: [
      { key: 'op', type: 'select', options: TEXT_OPS },
      { key: 'input', type: 'template', placeholder: true, singleLine: true },
      ...textOperands,
      saveAs
    ]
  },
  {
    type: 'reply.math',
    slug: 'math',
    icon: 'calculator',
    fields: [{ key: 'expression', type: 'template', placeholder: true, singleLine: true }, saveAs]
  },
  {
    type: 'reply.http_get',
    slug: 'httpGet',
    icon: 'globe',
    fields: [
      { key: 'url', type: 'template', placeholder: true, singleLine: true },
      { key: 'jsonPath', type: 'text', placeholder: true },
      saveAs
    ],
    outputs: apiPorts
  },
  {
    type: 'reply.http_post',
    slug: 'httpPost',
    icon: 'globe',
    fields: [{ key: 'url', type: 'template', placeholder: true, singleLine: true }, { key: 'body', type: 'template', placeholder: true }, saveAs],
    outputs: apiPorts
  },
  {
    type: 'reply.reward_update',
    slug: 'rewardUpdate',
    icon: 'ticket',
    fields: [
      { key: 'reward', type: 'choice', source: 'manageableRewards', nameKey: 'rewardTitle' },
      { key: 'setEnabled', type: 'select', options: ['keep', 'on', 'off'], initial: 'on' },
      { key: 'cost', type: 'number', min: 1, max: MAX_REWARD_COST, unit: true },
      { key: 'reset', type: 'toggle' }
    ]
  },
  {
    type: 'reply.cooldown_add',
    slug: 'cooldownAdd',
    icon: 'timer',
    fields: [
      { key: 'name', type: 'choice', source: 'namedCooldowns' },
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS, initial: 60 },
      { key: 'per', type: 'number', min: 0 }
    ]
  },
  ...STREAM_STEP_DEFINITIONS,
  {
    type: 'reply.stop',
    slug: 'stop',
    icon: 'x',
    fields: [],
    outputs: []
  },
  {
    type: 'reply.random',
    slug: 'random',
    icon: 'dice',
    fields: [
      { key: 'paths', type: 'select', options: RANDOM_PATH_COUNTS, initial: RANDOM_PATH_COUNTS[0]! },
      ...RANDOM_PATHS.map(({ path, weightKey }): FieldSpec => ({
        key: weightKey, type: 'number', min: 1, max: 100, step: 1, initial: RANDOM_PATH_DEFAULT_WEIGHT, unit: true,
        when: { key: 'paths', value: RANDOM_PATH_COUNTS.filter((count) => Number(count) >= path) }
      }))
    ],
    outputsFor: activeRandomPaths
  },
  {
    type: 'reply.run',
    slug: 'run',
    icon: 'play',
    fields: [{ key: 'automationId', type: 'choice', source: 'recipes', nameKey: 'name' }]
  }
];

export const NODE_SPECS: readonly NodeSpec[] = STEP_DEFINITIONS.map((spec) => ({ ...spec, ...STEP_PLACEMENT[spec.type] }));
export const LIBRARY_SPECS = NODE_SPECS.filter((spec) => !spec.hidden);

function matchesValue(wanted: FieldGate['value'], value: unknown): boolean {
  if (typeof wanted === 'boolean') return Boolean(value) === wanted;
  return typeof wanted === 'string' ? value === wanted : wanted.includes(String(value));
}

function gateMatches(spec: NodeSpec, config: FlowNode['config'], { key, value }: FieldGate): boolean {
  const gate = spec.fields.find((field) => field.key === key);
  if (gate?.type === 'target') return matchesValue(value, targetMode(config));
  const fallback = gate?.type === 'select' ? gate.options[0] : gate?.initial;
  return matchesValue(value, config[key] ?? fallback);
}

export function fieldVisible(spec: NodeSpec, node: Pick<FlowNode, 'config'>, field: FieldSpec): boolean {
  if (!field.when) return true;
  const gates = 'key' in field.when ? [field.when] : field.when;
  return gates.every((gate) => gateMatches(spec, node.config, gate));
}

export const specFor = (type: string): NodeSpec | undefined => NODE_SPECS.find((s) => s.type === type);

export const targetFieldFor = (spec: NodeSpec | undefined): boolean =>
  spec?.fields.some((field) => field.type === 'target') ?? false;

export const hasLegacyTarget = (config: FlowNode['config']): boolean => typeof config.target === 'string';

const pickerOwnsTarget = (spec: NodeSpec, node: FlowNode): boolean => targetFieldFor(spec) && !hasLegacyTarget(node.config);

export const fieldMounted = (spec: NodeSpec, node: FlowNode, field: FieldSpec): boolean =>
  field.key !== 'target' || !pickerOwnsTarget(spec, node);

export const triggerProvidesViewer = (type: FlowNode['type'] | undefined): boolean =>
  Boolean(specFor(type ?? '')?.provides?.some((variable) => variable.token === '{user}'));

export const targetHasViewer = (type: FlowNode['type'] | undefined): boolean =>
  type === 'twitch.unban_request' || triggerProvidesViewer(type);

const PICKED_TARGETS = new Set(['user', 'mention', 'viewer']);

export function targetMode(config: FlowNode['config']): string {
  if (PICKED_TARGETS.has(String(config.who))) return String(config.who);
  return hasLegacyTarget(config) ? 'login' : 'viewer';
}

export function targetModesFor(flow: Flow, node: FlowNode): string[] {
  const trigger = flow.nodes.find((entry) => specFor(entry.type)?.kind === 'trigger')?.type;
  const selected = targetMode(node.config);
  const options = [
    ['viewer', targetHasViewer(trigger)],
    ['mention', trigger === 'twitch.chat.command' || trigger === 'twitch.chat.word'],
    ['user', true],
    ['login', hasLegacyTarget(node.config)]
  ] as const;
  return options.filter(([mode, allowed]) => allowed || mode === selected).map(([mode]) => mode);
}

const nextOutput: readonly OutputSpec[] = [{ id: 'next' }];
const defaultOutputConfig: Pick<FlowNode, 'config'> = { config: {} };

export const outputsFor = (spec: NodeSpec | undefined, node = defaultOutputConfig): readonly OutputSpec[] =>
  spec ? spec.outputsFor?.(node.config) ?? spec.outputs ?? nextOutput : [];

export const isSecondPort = (spec: NodeSpec | undefined, node: FlowNode, port: FlowEdge['port']): boolean => outputsFor(spec, node)[1]?.id === port;

export function storedVariable(config: FlowNode['config']): VariableSpec[] {
  const name = String(config.name ?? '').trim().toLowerCase();
  if (!/^[a-z0-9]{1,32}$/.test(name)) return [];
  const scope = config.scope === 'viewer' ? 'viewervar' : 'var';
  const sample = config.do === 'add' || config.do === 'subtract' ? config.amount : config.value;
  return [{ token: `{${scope}:${name}}`, name, literalName: true, sample: String(sample ?? '0') }];
}

function changedVariable(config: FlowNode['config']): VariableSpec[] {
  return config.do === 'forget' ? [] : storedVariable(config);
}

function savedLocal(parent: FlowNode, spec: NodeSpec): VariableSpec[] {
  const name = String(parent.config.saveAs ?? '').trim().toLowerCase();
  const saves = spec.fields.some((field) => field.key === 'saveAs');
  const sample = parent.type === 'reply.math' ? '42' : name;
  return name && saves ? [{ token: `{local:${name}}`, name, literalName: true, sample }] : [];
}

function providedBy(parent: FlowNode, spec: NodeSpec, port: string): readonly VariableSpec[] {
  if (parent.config.disabled) return [];
  const outputs = outputsFor(spec, parent);
  const viaPort = outputs.find((output) => output.id === port)?.provides ?? [];
  const provided = spec.providesFor?.(parent.config) ?? spec.provides ?? [];
  const saved = outputs === apiPorts && port !== 'ok' ? [] : savedLocal(parent, spec);
  return [...provided, ...viaPort, ...saved];
}

export function variablesFor(flow: Flow, nodeId: string): VariableSpec[] {
  const index = graphIndex(flow);
  const memo = new Map<string, Scope | null>();
  const visit = (id: string): Scope | null => {
    if (memo.has(id)) return memo.get(id)!;
    memo.set(id, null);
    const node = index.nodes.get(id);
    if (node && specFor(node.type)?.kind === 'trigger') {
      const root: Scope = { parent: null, provided: [], clearError: false, forks: new Map() };
      memo.set(id, root);
      return root;
    }
    const paths = (index.incoming.get(id) ?? []).map((edge) => pathVariables(flow, edge, visit));
    const connected = paths.filter((path): path is Scope => path !== null);
    const result = mergeScopes(connected);
    memo.set(id, result);
    return result;
  };
  const scope = visit(nodeId);
  const tokens = scope ? scopeTokens(scope) : new Map<string, VariableSpec>();
  for (const variable of persistentVariables(flow)) tokens.set(variable.token, variable);
  return [...tokens.values()];
}

function persistentVariables(flow: Flow): VariableSpec[] {
  return flow.nodes.filter((node) => node.type === 'reply.var').flatMap((node) => changedVariable(node.config));
}

type ForkScope = { branch: string; outer: Scope };
type Scope = { parent: Scope | null; provided: readonly VariableSpec[]; clearError: boolean; forks: Map<string, ForkScope> };

function pathVariables(flow: Flow, edge: FlowEdge, visit: (id: string) => Scope | null): Scope | null {
  const prior = visit(edge.from);
  const index = graphIndex(flow);
  const parent = index.nodes.get(edge.from);
  if (!parent || !prior) return null;
  const spec = specFor(parent.type);
  if (!spec) return null;
  const clearError = outputsFor(spec, parent) === apiPorts && edge.port === 'ok' && !parent.config.disabled;
  const scope: Scope = { parent: prior, provided: providedBy(parent, spec, edge.port), clearError, forks: prior.forks };
  return forkScope(flow, edge, scope);
}

function forkScope(flow: Flow, edge: FlowEdge, scope: Scope): Scope {
  const key = portKey(edge);
  const siblings = graphIndex(flow).ports.get(key) ?? [];
  if (siblings.length < 2) return scope;
  const forks = new Map(scope.forks);
  forks.set(key, { branch: edge.to, outer: scope });
  return { ...scope, forks };
}

function scopeTokens(scope: Scope): Map<string, VariableSpec> {
  const chain: Scope[] = [];
  for (let cursor: Scope | null = scope; cursor; cursor = cursor.parent) chain.push(cursor);
  const tokens = new Map<string, VariableSpec>();
  for (const entry of chain.reverse()) {
    if (entry.clearError) tokens.delete('{local:error}');
    for (const variable of entry.provided) tokens.set(variable.token, variable);
  }
  return tokens;
}

function mergeScopes(paths: Scope[]): Scope | null {
  if (paths.length < 2) return paths[0] ?? null;
  const tokens = paths.map(scopeTokens);
  const common = [...tokens[0].values()].filter((variable) => tokens.every((path) => path.has(variable.token)));
  const allForks = new Map(paths.flatMap((path) => [...path.forks]));
  const joined = [...allForks].filter(([key, fork]) => paths.some((path) => path.forks.has(key) && path.forks.get(key)?.branch !== fork.branch));
  const forks = new Map([...allForks].filter(([key, fork]) => paths.every((path) => path.forks.get(key)?.branch === fork.branch)));
  const join = joined.sort((a, b) => a[1].outer.forks.size - b[1].outer.forks.size)[0];
  const restored = restoredLocals({ join, paths });
  const provided = join ? [...common.filter((variable) => !variable.token.startsWith('{local:')), ...restored] : common;
  return { parent: null, provided, clearError: false, forks };
}

function restoredLocals({ join, paths }: { join?: [string, ForkScope]; paths: Scope[] }): VariableSpec[] {
  if (!join) return [];
  const [key, { outer }] = join;
  const locals = [...scopeTokens(outer).values()].filter((variable) => variable.token.startsWith('{local:'));
  const bypass = paths.filter((path) => !path.forks.has(key)).map(scopeTokens);
  return locals.filter((variable) => bypass.every((path) => path.has(variable.token)));
}
