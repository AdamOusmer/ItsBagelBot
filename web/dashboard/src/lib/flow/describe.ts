// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import { optionText, outputLabel, stepSummary } from './copy';
import type { Translate } from './messages';
import { specFor, type FieldSpec, type FlowNode, type NodeSpec } from './model';

const COMPARISONS: Readonly<Record<string, MessageKey>> = Object.freeze({
  gte: 'flows.line.compareGte',
  lt: 'flows.line.compareLt',
  eq: 'flows.line.compareEq',
  contains: 'flows.line.compareContains'
});

const ROLES: Readonly<Record<string, MessageKey>> = Object.freeze({
  everyone: 'flows.line.roleEveryone',
  sub: 'flows.line.roleSub',
  vip: 'flows.line.roleVip',
  mod: 'flows.line.roleMod',
  lead_mod: 'flows.line.roleLeadMod',
  broadcaster: 'flows.line.roleBroadcaster'
});

const SUBS: Readonly<Record<string, MessageKey>> = Object.freeze({
  any: 'flows.line.subAny',
  new: 'flows.line.subNew',
  resub: 'flows.line.subResub',
  gift: 'flows.line.subGift'
});

const text = (node: FlowNode, key: string) => {
  const v = node.config[key];
  return v === undefined || v === false ? '' : String(v).trim();
};

const num = (node: FlowNode, key: string) => {
  const v = Number(node.config[key]);
  return Number.isFinite(v) && v > 0 ? v : 0;
};

const seconds = (t: Translate, count: number) => (count === 1 ? t('flows.line.secondsOne', { count }) : t('flows.line.secondsMany', { count }));

const points = (t: Translate, count: number) => (count === 1 ? t('flows.line.pointsOne', { count }) : t('flows.line.pointsMany', { count }));

const given = <T>(value: T, make: (value: T) => string): string => (value ? make(value) : '');

const named = (t: Translate, keys: Readonly<Record<string, MessageKey>>, value: string): string =>
  Object.hasOwn(keys, value) ? t(keys[value]!) : '';

type SelectField = Extract<FieldSpec, { type: 'select' }>;

function selectLabel(t: Translate, spec: NodeSpec, field: SelectField, value: string): string {
  if (field.options.includes(value)) return optionText(t, spec, field, value);
  const first = field.options[0];
  return value || (first ? optionText(t, spec, field, first) : '');
}

export function optionLabel(t: Translate, spec: NodeSpec, node: FlowNode, key: string): string {
  const field = spec.fields.find((f) => f.key === key);
  const value = text(node, key);
  if (field?.type === 'choice') return text(node, field.nameKey ?? key) || value;
  return field?.type === 'select' ? selectLabel(t, spec, field, value) : value;
}

type Line = (t: Translate, node: FlowNode, spec: NodeSpec) => string;

const TRIGGERS: Readonly<Record<string, Line>> = {
  'twitch.chat.command': (t, n) => given(text(n, 'trigger'), (trigger) => t('flows.line.command', { trigger })),
  'twitch.chat.word': (t, n) => given(text(n, 'word'), (word) => t('flows.line.word', { word })),
  'twitch.sub': (t, n) => named(t, SUBS, text(n, 'variant')),
  'twitch.cheer': (t, n) => given(num(n, 'minBits'), (bits) => t('flows.line.cheer', { bits })),
  'twitch.raid': (t, n) => given(num(n, 'minViewers'), (viewers) => t('flows.line.raid', { viewers })),
  'twitch.reward': (t, n, spec) => given(optionLabel(t, spec, n, 'reward'), (reward) => t('flows.line.reward', { reward }))
};

const compareLine: Line = (t, n) => {
  const left = text(n, 'left');
  const right = text(n, 'right');
  if (!left || !right) return '';
  const op = text(n, 'op');
  return Object.hasOwn(COMPARISONS, op) ? t(COMPARISONS[op]!, { left, right }) : t('flows.line.compareIs', { left, right });
};

const cooldownLine: Line = (t, n) =>
  given(num(n, 'seconds'), (count) =>
    t(n.config.perUser ? 'flows.line.cooldownViewer' : 'flows.line.cooldown', { amount: seconds(t, count) })
  );

const CONDITIONS: Readonly<Record<string, Line>> = {
  'cond.role': (t, n) => named(t, ROLES, text(n, 'level')),
  'cond.user': (t, n) => given(text(n, 'login') || text(n, 'userId'), (user) => t('flows.line.userOnly', { user })),
  'cond.cooldown': cooldownLine,
  'cond.compare': compareLine,
  'cond.chance': (t, n) => given(num(n, 'percent'), (percent) => t('flows.line.chance', { percent }))
};

const discordLine: Line = (t, n, spec) => {
  const channel = optionLabel(t, spec, n, 'channel');
  const message = text(n, 'message');
  if (!message) return given(channel, (name) => t('flows.line.discordPost', { channel: name }));
  return channel ? t('flows.line.discordSay', { channel, message }) : message;
};

const pointsLine: Line = (t, n) => {
  const amount = Number(n.config.amount);
  if (!Number.isFinite(amount) || amount === 0) return '';
  return amount > 0 ? t('flows.line.pointsGive', { amount: points(t, amount) }) : t('flows.line.pointsTake', { amount: points(t, -amount) });
};

const lightsLine: Line = (t, n, spec) => {
  const light = text(n, 'deviceName') || t('flows.line.lightsFallback');
  if (text(n, 'mode') !== 'scene') return given(text(n, 'color'), (color) => t('flows.line.lightsColor', { light, color }));
  return given(optionLabel(t, spec, n, 'scene'), (scene) => t('flows.line.lightsScene', { light, scene }));
};

const REPLIES: Readonly<Record<string, Line>> = {
  'reply.chat': (_t, n) => text(n, 'message'),
  'reply.discord': discordLine,
  'reply.points': pointsLine,
  'reply.counter': (t, n, spec) => given(optionLabel(t, spec, n, 'counter'), (counter) => t('flows.line.counter', { counter })),
  'reply.lights': lightsLine,
  'reply.wait': (t, n) => given(num(n, 'seconds'), (count) => t('flows.line.pause', { amount: seconds(t, count) }))
};

const BY_KIND: Readonly<Record<string, Readonly<Record<string, Line>>>> = { trigger: TRIGGERS, condition: CONDITIONS };

export function portLabel(t: Translate, node: FlowNode, id: string): string {
  const percent = node.type === 'cond.chance' ? num(node, 'percent') : 0;
  if (percent) return id === 'yes' ? t('flows.line.chanceYes', { percent }) : t('flows.line.chanceNo');
  const spec = specFor(node.type);
  return spec ? outputLabel(t, spec, id) : '';
}

export function describe(t: Translate, node: FlowNode): string {
  const spec = specFor(node.type);
  if (!spec) return '';
  const table = BY_KIND[spec.kind] ?? REPLIES;
  return table[node.type]?.(t, node, spec) || stepSummary(t, spec);
}
