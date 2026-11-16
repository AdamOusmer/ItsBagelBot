// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { STREAM_STEPS } from './stream-steps';
import { graphIndex, portKey } from './graph-index';
import type { ChoiceSource } from './choices';
import type { FlowIconName as IconName } from './icons';
import { MAX_COOLDOWN_SECONDS, MAX_DELAY_MS, MAX_REWARD_COST } from '@bagel/kit/automations/limits';
import { ARGS_SAMPLE, GAME_SAMPLE, TITLE_SAMPLE, TOUSER_SAMPLE, USERID_SAMPLE, USER_SAMPLE } from '@bagel/kit/variables/preview-values';

export const MAX_WAIT_SECONDS = MAX_DELAY_MS / 1000;

export type FlowKind = 'trigger' | 'condition' | 'reply';

export type StepGroup = 'twitch' | 'modules' | 'basics';

type Typed = { key: string; placeholder?: true };

export type FieldSpec = { when?: { key: string; value: string | readonly string[] | boolean } } & (
  | (Typed & { type: 'text' })
  | (Typed & { type: 'template'; singleLine?: boolean })
  | { key: string; type: 'number'; min?: number; max?: number; unit?: true }
  | { key: string; type: 'duration'; min: number; max: number }
  | { key: string; type: 'select'; options: readonly string[] }
  | { key: string; type: 'toggle'; defaultOn?: true }
  | { key: string; type: 'color' }
  | (Typed & { type: 'login' })
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
  provides?: readonly VariableSpec[];
  providesFor?(config: FlowNode['config']): readonly VariableSpec[];
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
  resub: [{ token: '{months}', name: 'months', sample: '14' }],
  gift: [{ token: '{count}', name: 'count', sample: '5' }]
};

const subViewer: readonly VariableSpec[] = [...user, { token: '{tier}', name: 'tier', sample: '1' }];

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
const VAR_OPS_WITH_VALUE = ['eq', 'ne', 'gt', 'gte', 'lt', 'lte', 'contains'] as const;

export const newFlow = (name: string): Flow => ({ id: 'new', name, enabled: true, nodes: [], edges: [] });

export const NODE_SPECS: readonly NodeSpec[] = [
  {
    type: 'countdown.finished', slug: 'countdownFinished', kind: 'trigger', group: 'basics', icon: 'timer',
    fields: [{ key: 'name', type: 'choice', source: 'namedCountdowns' }]
  },
  {
    type: 'reply.countdown_start', slug: 'countdownStart', kind: 'reply', group: 'basics', icon: 'timer',
    fields: [
      { key: 'name', type: 'text', placeholder: true },
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS },
      { key: 'leftCommand', type: 'text', placeholder: true },
      { key: 'leftReply', type: 'template', placeholder: true, singleLine: true },
      { key: 'readyReply', type: 'template', placeholder: true, singleLine: true }
    ],
    provides: [{ token: '{countdown:left}', name: 'countdownLeft', sample: '2d 3h' }]
  },
  {
    type: 'reply.countdown_add', slug: 'countdownAdd', kind: 'reply', group: 'basics', icon: 'timer',
    fields: [
      { key: 'name', type: 'choice', source: 'namedCountdowns' },
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS },
      { key: 'per', type: 'number', min: 0 }
    ],
    provides: [{ token: '{countdown:left}', name: 'countdownLeft', sample: '2d 3h' }]
  },
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
    provides: [...subViewer, ...subVariables.resub],
    providesFor: (config) => [...subViewer, ...(subVariables[String(config.variant)] ?? [])]
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
    fields: [{ key: 'reward', type: 'reward' }],
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
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS },
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
    type: 'cond.var',
    slug: 'var',
    kind: 'condition',
    group: 'basics',
    icon: 'braces',
    fields: [
      varScope,
      varName,
      { key: 'op', type: 'select', options: [...VAR_OPS_WITH_VALUE, 'exists'] },
      { key: 'value', type: 'template', placeholder: true, singleLine: true, when: { key: 'op', value: VAR_OPS_WITH_VALUE } }
    ],
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
      { key: 'scene', type: 'choice', source: 'scenes', nameKey: 'sceneName', when: { key: 'mode', value: 'scene' } },
      { key: 'liveOnly', type: 'toggle', defaultOn: true }
    ]
  },
  {
    type: 'reply.wait',
    slug: 'wait',
    kind: 'reply',
    group: 'basics',
    icon: 'timer',
    fields: [{ key: 'seconds', type: 'number', min: 1, max: MAX_WAIT_SECONDS, unit: true }]
  },
  {
    type: 'reply.loop',
    slug: 'loop',
    kind: 'reply',
    group: 'basics',
    icon: 'repeat',
    fields: [{ key: 'times', type: 'template', placeholder: true, singleLine: true }],
    outputs: loopPorts
  },
  {
    type: 'reply.var_set',
    slug: 'varSet',
    kind: 'reply',
    group: 'basics',
    icon: 'braces',
    fields: [varScope, varName, { key: 'value', type: 'template', placeholder: true, singleLine: true }],
    providesFor: storedVariable
  },
  {
    type: 'reply.var_add',
    slug: 'varAdd',
    kind: 'reply',
    group: 'basics',
    icon: 'braces',
    fields: [varScope, varName, { key: 'amount', type: 'number' }],
    providesFor: storedVariable
  },
  {
    type: 'reply.var_delete',
    slug: 'varDelete',
    kind: 'reply',
    group: 'basics',
    icon: 'braces',
    fields: [varScope, varName]
  },
  {
    type: 'reply.text',
    slug: 'text',
    kind: 'reply',
    group: 'basics',
    icon: 'letters',
    fields: [
      { key: 'op', type: 'select', options: ['upper', 'lower', 'trim', 'length', 'replace', 'substring', 'split_pick', 'contains', 'url_encode'] },
      { key: 'input', type: 'template', placeholder: true, singleLine: true },
      { key: 'find', type: 'template', singleLine: true, when: { key: 'op', value: 'replace' } },
      { key: 'replaceWith', type: 'template', singleLine: true, when: { key: 'op', value: 'replace' } },
      { key: 'start', type: 'template', singleLine: true, when: { key: 'op', value: 'substring' } },
      { key: 'count', type: 'template', singleLine: true, when: { key: 'op', value: 'substring' } },
      { key: 'separator', type: 'template', singleLine: true, when: { key: 'op', value: 'split_pick' } },
      { key: 'pick', type: 'template', placeholder: true, singleLine: true, when: { key: 'op', value: 'split_pick' } },
      { key: 'needle', type: 'template', singleLine: true, when: { key: 'op', value: 'contains' } },
      saveAs
    ]
  },
  {
    type: 'reply.math',
    slug: 'math',
    kind: 'reply',
    group: 'basics',
    icon: 'calculator',
    fields: [{ key: 'expression', type: 'template', placeholder: true, singleLine: true }, saveAs]
  },
  {
    type: 'reply.http_get',
    slug: 'httpGet',
    kind: 'reply',
    group: 'basics',
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
    kind: 'reply',
    group: 'basics',
    icon: 'globe',
    fields: [{ key: 'url', type: 'template', placeholder: true, singleLine: true }, { key: 'body', type: 'template', placeholder: true }, saveAs],
    outputs: apiPorts
  },
  {
    type: 'reply.reward_update',
    slug: 'rewardUpdate',
    kind: 'reply',
    group: 'modules',
    icon: 'ticket',
    fields: [
      { key: 'reward', type: 'choice', source: 'manageableRewards', nameKey: 'rewardTitle' },
      { key: 'setEnabled', type: 'select', options: ['keep', 'on', 'off'] },
      { key: 'cost', type: 'number', min: 1, max: MAX_REWARD_COST, unit: true },
      { key: 'reset', type: 'toggle' }
    ]
  },
  {
    type: 'reply.cooldown_add',
    slug: 'cooldownAdd',
    kind: 'reply',
    group: 'basics',
    icon: 'timer',
    fields: [
      { key: 'name', type: 'choice', source: 'namedCooldowns' },
      { key: 'seconds', type: 'duration', min: 1, max: MAX_COOLDOWN_SECONDS },
      { key: 'per', type: 'number', min: 0 }
    ]
  },
  ...STREAM_STEPS
];

function gateMatches(wanted: string | readonly string[] | boolean, value: unknown): boolean {
  if (typeof wanted === 'boolean') return Boolean(value) === wanted;
  return typeof wanted === 'string' ? value === wanted : wanted.includes(String(value));
}

export function fieldVisible(spec: NodeSpec, node: Pick<FlowNode, 'config'>, field: FieldSpec): boolean {
  if (!field.when) return true;
  const gate = spec.fields.find((f) => f.key === field.when?.key);
  const fallback = gate?.type === 'select' ? gate.options[0] : undefined;
  return gateMatches(field.when.value, node.config[field.when.key] ?? fallback);
}

export const specFor = (type: string): NodeSpec | undefined => NODE_SPECS.find((s) => s.type === type);

export const outputsFor = (spec: NodeSpec): readonly OutputSpec[] => spec.outputs ?? [{ id: 'next' }];

export const isSecondPort = (spec: NodeSpec | undefined, port: string): boolean => spec?.outputs?.[1]?.id === port;

function storedVariable(config: FlowNode['config']): VariableSpec[] {
  const name = String(config.name ?? '').trim().toLowerCase();
  if (!/^[a-z]+$/.test(name)) return [];
  const scope = config.scope === 'viewer' ? 'viewervar' : 'var';
  return [{ token: `{${scope}:${name}}`, name, literalName: true, sample: String(config.value ?? config.amount ?? '0') }];
}

function savedLocal(parent: FlowNode, spec: NodeSpec): VariableSpec[] {
  const name = String(parent.config.saveAs ?? '').trim().toLowerCase();
  const saves = spec.fields.some((field) => field.key === 'saveAs');
  const sample = parent.type === 'reply.math' ? '42' : name;
  return name && saves ? [{ token: `{local:${name}}`, name, literalName: true, sample }] : [];
}

function providedBy(parent: FlowNode, spec: NodeSpec, port: string): readonly VariableSpec[] {
  if (parent.config.disabled) return [];
  const viaPort = spec.outputs?.find((output) => output.id === port)?.provides ?? [];
  const provided = spec.providesFor?.(parent.config) ?? spec.provides ?? [];
  const saved = spec.outputs === apiPorts && port !== 'ok' ? [] : savedLocal(parent, spec);
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
  return scope ? [...scopeTokens(scope).values()] : [];
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
  const clearError = spec.outputs === apiPorts && edge.port === 'ok' && !parent.config.disabled;
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
  const forks = new Map(paths[0].forks);
  const joined = [...forks].filter(([key, fork]) => paths.some((path) => path.forks.get(key)?.branch !== fork.branch));
  for (const [key] of joined) forks.delete(key);
  const outer = joined.sort((a, b) => a[1].outer.forks.size - b[1].outer.forks.size)[0]?.[1].outer;
  const provided = outer ? [...common.filter((variable) => !variable.token.startsWith('{local:')), ...[...scopeTokens(outer).values()].filter((variable) => variable.token.startsWith('{local:'))] : common;
  return { parent: null, provided, clearError: false, forks };
}
