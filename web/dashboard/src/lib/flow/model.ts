// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { FlowIconName as IconName } from './icons';
import { ARGS_SAMPLE, GAME_SAMPLE, TITLE_SAMPLE, TOUSER_SAMPLE, USERID_SAMPLE, USER_SAMPLE } from '@bagel/kit/variables/preview-values';

export type FlowKind = 'trigger' | 'condition' | 'reply';

export type FieldSpec = { when?: { key: string; value: string } } & (
  | { key: string; label: string; type: 'text'; placeholder?: string }
  | { key: string; label: string; type: 'template'; placeholder?: string; singleLine?: boolean }
  | { key: string; label: string; type: 'number'; min?: number; max?: number; unit?: string }
  | { key: string; label: string; type: 'select'; options: readonly { value: string; label: string }[] }
  | { key: string; label: string; type: 'toggle' }
  | { key: string; label: string; type: 'color' }
  | { key: string; label: string; type: 'login'; placeholder?: string }
  | { key: string; label: string; type: 'goveeLight' }
);

export type VariableSpec = { token: string; label: string; sample: string };

export type NodeSpec = {
  type: string;
  kind: FlowKind;
  label: string;
  summary: string;
  icon: IconName;
  source: string;
  fields: readonly FieldSpec[];
  provides?: readonly VariableSpec[];
  outputs?: readonly { id: string; label: string }[];
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
  { token: '{user}', label: 'Display name', sample: USER_SAMPLE },
  { token: '{user.id}', label: 'User ID', sample: USERID_SAMPLE }
];

export const newFlow = (): Flow => ({ id: 'new', name: 'Untitled', enabled: true, nodes: [], edges: [] });

export const NODE_SPECS: readonly NodeSpec[] = [
  {
    type: 'twitch.chat.command',
    kind: 'trigger',
    label: 'Chat command',
    summary: 'Someone types a command in chat',
    icon: 'commands',
    source: 'Twitch chat',
    fields: [{ key: 'trigger', label: 'Command', type: 'text', placeholder: '!hug' }],
    provides: [...user, { token: '{args}', label: 'Arguments', sample: ARGS_SAMPLE }, { token: '{touser}', label: 'First argument', sample: TOUSER_SAMPLE }]
  },
  {
    type: 'twitch.chat.word',
    kind: 'trigger',
    label: 'Chat word',
    summary: 'Someone says a word in chat',
    icon: 'messageSquare',
    source: 'Twitch chat',
    fields: [{ key: 'word', label: 'Word', type: 'text', placeholder: 'bagel' }],
    provides: user
  },
  {
    type: 'twitch.follow',
    kind: 'trigger',
    label: 'New follower',
    summary: 'Someone follows the channel',
    icon: 'heart',
    source: 'Twitch EventSub',
    fields: [],
    provides: user
  },
  {
    type: 'twitch.sub',
    kind: 'trigger',
    label: 'Subscription',
    summary: 'New sub, resub or gift',
    icon: 'star',
    source: 'Twitch EventSub',
    fields: [
      {
        key: 'variant',
        label: 'Kind',
        type: 'select',
        options: [
          { value: 'any', label: 'Any' },
          { value: 'new', label: 'New' },
          { value: 'resub', label: 'Resub' },
          { value: 'gift', label: 'Gift' }
        ]
      }
    ],
    provides: [...user, { token: '{tier}', label: 'Tier', sample: '1' }, { token: '{months}', label: 'Months', sample: '14' }]
  },
  {
    type: 'twitch.cheer',
    kind: 'trigger',
    label: 'Cheer',
    summary: 'Bits are cheered',
    icon: 'gem',
    source: 'Twitch EventSub',
    fields: [{ key: 'minBits', label: 'Minimum bits', type: 'number', min: 1, unit: 'bits' }],
    provides: [...user, { token: '{bits}', label: 'Bits', sample: '500' }]
  },
  {
    type: 'twitch.raid',
    kind: 'trigger',
    label: 'Raid',
    summary: 'Another channel raids you',
    icon: 'swords',
    source: 'Twitch EventSub',
    fields: [{ key: 'minViewers', label: 'Minimum viewers', type: 'number', min: 1, unit: 'viewers' }],
    provides: [...user, { token: '{viewers}', label: 'Viewers', sample: '42' }]
  },
  {
    type: 'twitch.reward',
    kind: 'trigger',
    label: 'Channel point reward',
    summary: 'A reward is redeemed',
    icon: 'ticket',
    source: 'Channel points module',
    fields: [{ key: 'reward', label: 'Reward', type: 'select', options: [{ value: 'hydrate', label: 'Hydrate' }, { value: 'song', label: 'Song request' }] }],
    provides: [...user, { token: '{input}', label: 'Viewer input', sample: 'lofi beats' }]
  },
  {
    type: 'twitch.stream.online',
    kind: 'trigger',
    label: 'Stream goes live',
    summary: 'Your stream starts',
    icon: 'radio',
    source: 'Twitch EventSub',
    fields: [],
    provides: [{ token: '{title}', label: 'Stream title', sample: TITLE_SAMPLE }, { token: '{game}', label: 'Category', sample: GAME_SAMPLE }]
  },
  {
    type: 'cond.role',
    kind: 'condition',
    label: 'User level',
    summary: 'Only certain viewers pass',
    icon: 'moderation',
    source: 'Core',
    fields: [
      {
        key: 'level',
        label: 'At least',
        type: 'select',
        options: [
          { value: 'everyone', label: 'Everyone' },
          { value: 'sub', label: 'Subscriber' },
          { value: 'vip', label: 'VIP' },
          { value: 'mod', label: 'Moderator' },
          { value: 'lead_mod', label: 'Lead moderator' },
          { value: 'broadcaster', label: 'Broadcaster' }
        ]
      }
    ],
    outputs: [{ id: 'yes', label: 'Allowed' }, { id: 'no', label: 'Blocked' }]
  },
  {
    type: 'cond.user',
    kind: 'condition',
    label: 'Specific user',
    summary: 'Only this viewer passes',
    icon: 'users',
    source: 'Core',
    fields: [{ key: 'login', label: 'Username', type: 'login', placeholder: 'itsbagelbot' }],
    outputs: [{ id: 'yes', label: 'This user' }, { id: 'no', label: 'Anyone else' }]
  },
  {
    type: 'cond.cooldown',
    kind: 'condition',
    label: 'Cooldown',
    summary: 'Wait between runs',
    icon: 'hourglass',
    source: 'Core',
    fields: [
      { key: 'seconds', label: 'Cooldown', type: 'number', min: 1, unit: 'seconds' },
      { key: 'perUser', label: 'Per viewer', type: 'toggle' }
    ],
    outputs: [{ id: 'yes', label: 'Ready' }, { id: 'no', label: 'Cooling down' }]
  },
  {
    type: 'cond.compare',
    kind: 'condition',
    label: 'Compare value',
    summary: 'Check a variable',
    icon: 'scale',
    source: 'Core',
    fields: [
      { key: 'left', label: 'Value', type: 'template', placeholder: '{bits}', singleLine: true },
      {
        key: 'op',
        label: 'Is',
        type: 'select',
        options: [
          { value: 'gte', label: 'at least' },
          { value: 'lt', label: 'less than' },
          { value: 'eq', label: 'equal to' },
          { value: 'contains', label: 'contains' }
        ]
      },
      { key: 'right', label: 'This value', type: 'template', placeholder: '1000', singleLine: true }
    ],
    outputs: [{ id: 'yes', label: 'Yes' }, { id: 'no', label: 'No' }]
  },
  {
    type: 'cond.chance',
    kind: 'condition',
    label: 'Random chance',
    summary: 'Pass some of the time',
    icon: 'dice',
    source: 'Core',
    fields: [{ key: 'percent', label: 'Chance', type: 'number', min: 1, max: 100, unit: '%' }],
    outputs: [{ id: 'yes', label: 'Hit' }, { id: 'no', label: 'Miss' }]
  },
  {
    type: 'reply.chat',
    kind: 'reply',
    label: 'Send chat message',
    summary: 'Bot replies in chat',
    icon: 'send',
    source: 'Twitch chat',
    fields: [{ key: 'message', label: 'Message', type: 'template', placeholder: 'Thanks {user}!' }]
  },
  {
    type: 'reply.discord',
    kind: 'reply',
    label: 'Post to Discord',
    summary: 'Announce in a channel',
    icon: 'discord',
    source: 'Discord module',
    fields: [
      { key: 'channel', label: 'Channel', type: 'select', options: [{ value: 'announcements', label: '#announcements' }, { value: 'general', label: '#general' }] },
      { key: 'message', label: 'Message', type: 'template', placeholder: '{user} just raided!' }
    ]
  },
  {
    type: 'reply.points',
    kind: 'reply',
    label: 'Give loyalty points',
    summary: 'Add or remove points',
    icon: 'coins',
    source: 'Loyalty module',
    fields: [{ key: 'amount', label: 'Amount', type: 'number', unit: 'points' }]
  },
  {
    type: 'reply.counter',
    kind: 'reply',
    label: 'Bump counter',
    summary: 'Increase a counter',
    icon: 'hash',
    source: 'Counters module',
    fields: [{ key: 'counter', label: 'Counter', type: 'select', options: [{ value: 'deaths', label: 'deaths' }, { value: 'hugs', label: 'hugs' }] }],
    provides: [{ token: '{count}', label: 'New count', sample: '17' }]
  },
  {
    type: 'reply.lights',
    kind: 'reply',
    label: 'Set lights',
    summary: 'Set a Govee color or scene',
    icon: 'bulb',
    source: 'Govee module',
    fields: [
      { key: 'device', label: 'Light', type: 'goveeLight' },
      { key: 'mode', label: 'Set', type: 'select', options: [{ value: 'color', label: 'Color' }, { value: 'scene', label: 'Scene' }] },
      { key: 'color', label: 'Color', type: 'color', when: { key: 'mode', value: 'color' } },
      { key: 'scene', label: 'Scene', type: 'select', options: [{ value: 'party', label: 'Party' }, { value: 'alert', label: 'Alert' }], when: { key: 'mode', value: 'scene' } }
    ]
  },
  {
    type: 'reply.wait',
    kind: 'reply',
    label: 'Wait',
    summary: 'Pause before the next step',
    icon: 'timer',
    source: 'Core',
    fields: [{ key: 'seconds', label: 'Delay', type: 'number', min: 1, max: 600, unit: 'seconds' }]
  }
];

export function fieldVisible(spec: NodeSpec, node: FlowNode, field: FieldSpec): boolean {
  if (!field.when) return true;
  const gate = spec.fields.find((f) => f.key === field.when?.key);
  const fallback = gate?.type === 'select' ? gate.options[0]?.value : undefined;
  return (node.config[field.when.key] ?? fallback) === field.when.value;
}

export const specFor = (type: string): NodeSpec | undefined => NODE_SPECS.find((s) => s.type === type);

export const outputsFor = (spec: NodeSpec): readonly { id: string; label: string }[] =>
  spec.outputs ?? [{ id: 'next', label: 'Then' }];

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

