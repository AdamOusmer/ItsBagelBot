// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { specFor, type FlowNode, type NodeSpec } from './model';

export const OPS: Record<string, string> = {
  gte: 'is at least',
  lt: 'is less than',
  eq: 'is equal to',
  contains: 'contains'
};

const ROLES: Record<string, string> = {
  everyone: 'Everyone',
  sub: 'Subscribers and up',
  vip: 'VIPs and up',
  mod: 'Moderators and up',
  lead_mod: 'Lead moderators and up',
  broadcaster: 'Only the broadcaster'
};

const SUBS: Record<string, string> = {
  any: 'Someone subscribes, resubscribes or gifts',
  new: 'Someone subscribes for the first time',
  resub: 'Someone resubscribes',
  gift: 'Someone gifts a subscription'
};

const text = (node: FlowNode, key: string) => {
  const v = node.config[key];
  return v === undefined || v === false ? '' : String(v).trim();
};

const num = (node: FlowNode, key: string) => {
  const v = Number(node.config[key]);
  return Number.isFinite(v) && v > 0 ? v : 0;
};

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

const given = <T>(value: T, make: (value: T) => string): string => (value ? make(value) : '');

export function optionLabel(spec: NodeSpec, node: FlowNode, key: string): string {
  const field = spec.fields.find((f) => f.key === key);
  const value = text(node, key);
  if (!field || field.type !== 'select') return value;
  return field.options.find((o) => o.value === value)?.label ?? (value || field.options[0]?.label || '');
}

type Line = (node: FlowNode, spec: NodeSpec) => string;

const TRIGGERS: Record<string, Line> = {
  'twitch.chat.command': (n) => given(text(n, 'trigger'), (v) => `Someone types ${v} in chat`),
  'twitch.chat.word': (n) => given(text(n, 'word'), (v) => `Someone says ${v} in chat`),
  'twitch.sub': (n) => SUBS[text(n, 'variant')] ?? '',
  'twitch.cheer': (n) => given(num(n, 'minBits'), (v) => `Someone cheers ${v} bits or more`),
  'twitch.raid': (n) => given(num(n, 'minViewers'), (v) => `Raid with ${v}+ viewers`),
  'twitch.reward': (n, spec) => given(optionLabel(spec, n, 'reward'), (v) => `${v} is redeemed`)
};

const compareLine: Line = (n) => {
  const left = text(n, 'left');
  const right = text(n, 'right');
  return left && right ? `${left} ${OPS[text(n, 'op')] ?? 'is'} ${right}` : '';
};

const CONDITIONS: Record<string, Line> = {
  'cond.role': (n) => ROLES[text(n, 'level')] ?? '',
  'cond.user': (n) => given(text(n, 'login'), (v) => `Only ${v}`),
  'cond.cooldown': (n) =>
    given(num(n, 'seconds'), (v) => `Wait ${plural(v, 'second', 'seconds')} between runs${n.config.perUser ? ', per viewer' : ''}`),
  'cond.compare': compareLine,
  'cond.chance': (n) => given(num(n, 'percent'), (v) => `Passes ${v}% of the time`)
};

const discordLine: Line = (n, spec) => {
  const channel = optionLabel(spec, n, 'channel');
  const message = text(n, 'message');
  if (!message) return given(channel, (v) => `Post to ${v}`);
  return channel ? `${channel}: ${message}` : message;
};

const pointsLine: Line = (n) => {
  const amount = Number(n.config.amount);
  if (!Number.isFinite(amount) || amount === 0) return '';
  return amount > 0 ? `Give ${plural(amount, 'point', 'points')}` : `Take ${plural(-amount, 'point', 'points')}`;
};

const lightsLine: Line = (n, spec) => {
  const light = text(n, 'deviceName') || 'the lights';
  if (text(n, 'mode') !== 'scene') return given(text(n, 'color'), (v) => `Set ${light} to ${v}`);
  return given(optionLabel(spec, n, 'scene'), (v) => `Play the ${v} scene on ${light}`);
};

const REPLIES: Record<string, Line> = {
  'reply.chat': (n) => text(n, 'message'),
  'reply.discord': discordLine,
  'reply.points': pointsLine,
  'reply.counter': (n, spec) => given(optionLabel(spec, n, 'counter'), (v) => `Add one to the ${v} counter`),
  'reply.lights': lightsLine,
  'reply.wait': (n) => given(num(n, 'seconds'), (v) => `Pause ${plural(v, 'second', 'seconds')}`)
};

const BY_KIND: Record<string, Record<string, Line>> = { trigger: TRIGGERS, condition: CONDITIONS };

export function portLabel(node: FlowNode, port: { id: string; label: string }): string {
  if (node.type !== 'cond.chance') return port.label;
  const percent = num(node, 'percent');
  if (!percent) return port.label;
  return port.id === 'yes' ? `${percent}% of the time` : 'Otherwise';
}

export function describe(node: FlowNode): string {
  const spec = specFor(node.type);
  if (!spec) return '';
  const table = BY_KIND[spec.kind] ?? REPLIES;
  return table[node.type]?.(node, spec) || spec.summary;
}
