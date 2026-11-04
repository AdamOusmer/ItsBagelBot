// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { ChoiceSource } from './choices';
import type { FlowIconName as IconName } from './icons';
import { MAX_DELAY_MS } from '@bagel/kit/automations/limits';
import { ARGS_SAMPLE, GAME_SAMPLE, TITLE_SAMPLE, TOUSER_SAMPLE, USERID_SAMPLE, USER_SAMPLE } from '@bagel/kit/variables/preview-values';

export const MAX_WAIT_SECONDS = MAX_DELAY_MS / 1000;

export type FlowKind = 'trigger' | 'condition' | 'reply';

export type StepGroup = 'twitch' | 'discord' | 'modules' | 'basics';

type Typed = { key: string; placeholder?: true };

export type FieldSpec = { when?: { key: string; value: string } } & (
  | (Typed & { type: 'text' })
  | (Typed & { type: 'template'; singleLine?: boolean })
  | { key: string; type: 'number'; min?: number; max?: number; unit?: true }
  | { key: string; type: 'select'; options: readonly string[] }
  | { key: string; type: 'toggle' }
  | { key: string; type: 'color' }
  | (Typed & { type: 'login' })
  | { key: string; type: 'goveeLight' }
  | { key: string; type: 'choice'; source: ChoiceSource; nameKey?: string }
);

export type VariableSpec = { token: string; name: string; sample: string };

export type OutputSpec = { id: string };

export type NodeSpec = {
  type: string;
  slug: string;
  kind: FlowKind;
  group: StepGroup;
  icon: IconName;
  fields: readonly FieldSpec[];
  provides?: readonly VariableSpec[];
  outputs?: readonly OutputSpec[];
};

export type FlowNode = {
  id: string;
  type: string;
  x: number;
  y: number;
  config: Record<string, string | number | boolean>;
};

export type FlowEdge = { id: string; from: string; port: string; to: string };

export type Flow = { id: string; name: string; enabled: boolean; nodes: FlowNode[]; edges: FlowEdge[] };

export const GRID = 24;
export const NODE_W = 288;
export const COLUMN_GAP = 80;
export const ROW_GAP = 28;

const user: VariableSpec[] = [
  { token: '{user}', name: 'user', sample: USER_SAMPLE },
  { token: '{user.id}', name: 'userId', sample: USERID_SAMPLE }
];

const yesNo: OutputSpec[] = [{ id: 'yes' }, { id: 'no' }];

export const newFlow = (name: string): Flow => ({ id: 'new', name, enabled: true, nodes: [], edges: [] });

export const NODE_SPECS: readonly NodeSpec[] = [
  {
    type: 'twitch.chat.command',
    slug: 'chatCommand',
    kind: 'trigger',
    group: 'twitch',
    icon: 'commands',
    fields: [{ key: 'trigger', type: 'text', placeholder: true }],
    provides: [...user, { token: '{args}', name: 'args', sample: ARGS_SAMPLE }, { token: '{touser}', name: 'touser', sample: TOUSER_SAMPLE }]
  },
  {
    type: 'twitch.chat.word',
    slug: 'chatWord',
    kind: 'trigger',
    group: 'twitch',
    icon: 'messageSquare',
    fields: [{ key: 'word', type: 'text', placeholder: true }],
    provides: user
  },
  {
    type: 'twitch.follow',
    slug: 'follow',
    kind: 'trigger',
    group: 'twitch',
    icon: 'heart',
    fields: [],
    provides: user
  },
  {
    type: 'twitch.sub',
    slug: 'sub',
    kind: 'trigger',
    group: 'twitch',
    icon: 'star',
    fields: [{ key: 'variant', type: 'select', options: ['any', 'new', 'resub', 'gift'] }],
    provides: [...user, { token: '{tier}', name: 'tier', sample: '1' }, { token: '{months}', name: 'months', sample: '14' }]
  },
  {
    type: 'twitch.cheer',
    slug: 'cheer',
    kind: 'trigger',
    group: 'twitch',
    icon: 'gem',
    fields: [{ key: 'minBits', type: 'number', min: 1, unit: true }],
    provides: [...user, { token: '{bits}', name: 'bits', sample: '500' }]
  },
  {
    type: 'twitch.raid',
    slug: 'raid',
    kind: 'trigger',
    group: 'twitch',
    icon: 'swords',
    fields: [{ key: 'minViewers', type: 'number', min: 1, unit: true }],
    provides: [...user, { token: '{viewers}', name: 'viewers', sample: '42' }]
  },
  {
    type: 'twitch.reward',
    slug: 'reward',
    kind: 'trigger',
    group: 'modules',
    icon: 'ticket',
    fields: [{ key: 'reward', type: 'choice', source: 'rewards', nameKey: 'rewardTitle' }],
    provides: [...user, { token: '{input}', name: 'input', sample: 'lofi beats' }]
  },
  {
    type: 'twitch.stream.online',
    slug: 'streamOnline',
    kind: 'trigger',
    group: 'twitch',
    icon: 'radio',
    fields: [],
    provides: [{ token: '{title}', name: 'title', sample: TITLE_SAMPLE }, { token: '{game}', name: 'game', sample: GAME_SAMPLE }]
  },
  {
    type: 'cond.role',
    slug: 'role',
    kind: 'condition',
    group: 'basics',
    icon: 'moderation',
    fields: [{ key: 'level', type: 'select', options: ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'] }],
    outputs: yesNo
  },
  {
    type: 'cond.user',
    slug: 'user',
    kind: 'condition',
    group: 'basics',
    icon: 'users',
    fields: [{ key: 'login', type: 'login', placeholder: true }],
    outputs: yesNo
  },
  {
    type: 'cond.cooldown',
    slug: 'cooldown',
    kind: 'condition',
    group: 'basics',
    icon: 'hourglass',
    fields: [
      { key: 'seconds', type: 'number', min: 1, unit: true },
      { key: 'perUser', type: 'toggle' }
    ],
    outputs: yesNo
  },
  {
    type: 'cond.compare',
    slug: 'compare',
    kind: 'condition',
    group: 'basics',
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
    kind: 'condition',
    group: 'basics',
    icon: 'dice',
    fields: [{ key: 'percent', type: 'number', min: 1, max: 100, unit: true }],
    outputs: yesNo
  },
  {
    type: 'reply.chat',
    slug: 'chat',
    kind: 'reply',
    group: 'twitch',
    icon: 'send',
    fields: [{ key: 'message', type: 'template', placeholder: true }]
  },
  {
    type: 'reply.discord',
    slug: 'discord',
    kind: 'reply',
    group: 'discord',
    icon: 'discord',
    fields: [
      { key: 'channel', type: 'choice', source: 'discordChannels', nameKey: 'channelName' },
      { key: 'message', type: 'template', placeholder: true }
    ]
  },
  {
    type: 'reply.points',
    slug: 'points',
    kind: 'reply',
    group: 'modules',
    icon: 'coins',
    fields: [{ key: 'amount', type: 'number', unit: true }]
  },
  {
    type: 'reply.counter',
    slug: 'counter',
    kind: 'reply',
    group: 'modules',
    icon: 'hash',
    fields: [{ key: 'counter', type: 'choice', source: 'counters' }],
    provides: [{ token: '{count}', name: 'count', sample: '17' }]
  },
  {
    type: 'reply.lights',
    slug: 'lights',
    kind: 'reply',
    group: 'modules',
    icon: 'bulb',
    fields: [
      { key: 'device', type: 'goveeLight' },
      { key: 'mode', type: 'select', options: ['color', 'scene'] },
      { key: 'color', type: 'color', when: { key: 'mode', value: 'color' } },
      { key: 'scene', type: 'choice', source: 'scenes', nameKey: 'sceneName', when: { key: 'mode', value: 'scene' } }
    ]
  },
  {
    type: 'reply.wait',
    slug: 'wait',
    kind: 'reply',
    group: 'basics',
    icon: 'timer',
    fields: [{ key: 'seconds', type: 'number', min: 1, max: MAX_WAIT_SECONDS, unit: true }]
  }
];

export function fieldVisible(spec: NodeSpec, node: FlowNode, field: FieldSpec): boolean {
  if (!field.when) return true;
  const gate = spec.fields.find((f) => f.key === field.when?.key);
  const fallback = gate?.type === 'select' ? gate.options[0] : undefined;
  return (node.config[field.when.key] ?? fallback) === field.when.value;
}

export const specFor = (type: string): NodeSpec | undefined => NODE_SPECS.find((s) => s.type === type);

export const outputsFor = (spec: NodeSpec): readonly OutputSpec[] => spec.outputs ?? [{ id: 'next' }];

export function variablesFor(flow: Flow, nodeId: string): VariableSpec[] {
  const seen = new Map<string, VariableSpec>();
  const visit = (id: string, trail: Set<string>) => {
    for (const e of flow.edges) {
      if (e.to !== id || trail.has(e.from)) continue;
      const parent = flow.nodes.find((n) => n.id === e.from);
      const spec = parent && specFor(parent.type);
      for (const v of spec?.provides ?? []) seen.set(v.token, v);
      visit(e.from, new Set(trail).add(e.from));
    }
  };
  visit(nodeId, new Set([nodeId]));
  return [...seen.values()];
}
