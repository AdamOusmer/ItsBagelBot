// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import { optionText, outputLabel, stepSummary } from './copy';
import type { Translate } from './messages';
import { specFor, type FieldSpec, type FlowNode, type NodeSpec } from './model';
import { awaitingSetup, configText } from './setup';

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
  return v === false ? '' : configText(node, key).trim();
};

const num = (node: FlowNode, key: string) => {
  const v = Number(node.config[key]);
  return Number.isFinite(v) && v > 0 ? v : 0;
};

const seconds = (t: Translate, count: number) => (count === 1 ? t('flows.line.secondsOne', { count }) : t('flows.line.secondsMany', { count }));

const points = (t: Translate, count: number) => (count === 1 ? t('flows.line.pointsOne', { count }) : t('flows.line.pointsMany', { count }));

const given = <T>(value: T, make: (value: T) => string): string => (value ? make(value) : '');

const roleLine = (t: Translate, level: string): string => (Object.hasOwn(ROLES, level) ? t(ROLES[level]!) : '');

const subLine = (t: Translate, variant: string): string => (Object.hasOwn(SUBS, variant) ? t(SUBS[variant]!) : '');

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

function triggerLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'twitch.chat.command':
      return given(text(n, 'trigger'), (trigger) => t('flows.line.command', { trigger }));
    case 'twitch.chat.word':
      return given(text(n, 'word'), (word) => t('flows.line.word', { word }));
    case 'twitch.sub':
      return subLine(t, text(n, 'variant'));
    case 'twitch.cheer':
      return given(num(n, 'minBits'), (bits) => t('flows.line.cheer', { bits }));
    case 'twitch.raid':
      return given(num(n, 'minViewers'), (viewers) => t('flows.line.raid', { viewers }));
    case 'twitch.reward':
      return given(optionLabel(t, spec, n, 'reward'), (reward) => t('flows.line.reward', { reward }));
    default:
      return '';
  }
}

function compareLine(t: Translate, n: FlowNode): string {
  const left = text(n, 'left');
  const right = text(n, 'right');
  if (!left || !right) return '';
  const op = text(n, 'op');
  return Object.hasOwn(COMPARISONS, op) ? t(COMPARISONS[op]!, { left, right }) : t('flows.line.compareIs', { left, right });
}

function cooldownLine(t: Translate, n: FlowNode): string {
  return given(num(n, 'seconds'), (count) =>
    t(n.config.perUser ? 'flows.line.cooldownViewer' : 'flows.line.cooldown', { amount: seconds(t, count) })
  );
}

function varCheckLine(t: Translate, n: FlowNode): string {
  const spec = specFor(n.type)!;
  const name = text(n, 'name');
  if (!name) return '';
  if (text(n, 'op') === 'exists') return t('flows.line.varExists', { name });
  return t('flows.line.varCheck', { name, op: optionLabel(t, spec, n, 'op'), value: text(n, 'value') });
}

function conditionLine(t: Translate, n: FlowNode): string {
  switch (n.type) {
    case 'cond.var':
      return varCheckLine(t, n);
    case 'cond.role':
      return roleLine(t, text(n, 'level'));
    case 'cond.user':
      return given(text(n, 'login') || text(n, 'userId'), (user) => t('flows.line.userOnly', { user }));
    case 'cond.cooldown':
      return cooldownLine(t, n);
    case 'cond.compare':
      return compareLine(t, n);
    case 'cond.chance':
      return given(num(n, 'percent'), (percent) => t('flows.line.chance', { percent }));
    default:
      return '';
  }
}

function pointsLine(t: Translate, n: FlowNode): string {
  const amount = Number(n.config.amount);
  if (!Number.isFinite(amount) || amount === 0) return '';
  return amount > 0 ? t('flows.line.pointsGive', { amount: points(t, amount) }) : t('flows.line.pointsTake', { amount: points(t, -amount) });
}

function lightsLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  const light = text(n, 'deviceName') || t('flows.line.lightsFallback');
  if (text(n, 'mode') !== 'scene') return given(text(n, 'color'), (color) => t('flows.line.lightsColor', { light, color }));
  return given(optionLabel(t, spec, n, 'scene'), (scene) => t('flows.line.lightsScene', { light, scene }));
}

function varAddLine(t: Translate, n: FlowNode): string {
  const name = text(n, 'name');
  const amount = Number(n.config.amount) || 0;
  if (!name || amount === 0) return '';
  return amount > 0 ? t('flows.line.varAdd', { name, amount }) : t('flows.line.varTake', { name, amount: -amount });
}

function varLine(t: Translate, n: FlowNode): string {
  const name = text(n, 'name');
  switch (n.type) {
    case 'reply.var_set':
      return given(name, (shown) => t('flows.line.varSet', { name: shown, value: text(n, 'value') }));
    case 'reply.var_add':
      return varAddLine(t, n);
    case 'reply.var_delete':
      return given(name, (shown) => t('flows.line.varDelete', { name: shown }));
    default:
      return '';
  }
}

function dataLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'reply.text':
      return given(text(n, 'saveAs'), (name) => t('flows.line.textSaved', { name, op: optionLabel(t, spec, n, 'op') }));
    case 'reply.math':
      return text(n, 'expression');
    case 'reply.loop':
      return given(text(n, 'times'), (times) => t('flows.line.loop', { times }));
    case 'reply.http_get':
    case 'reply.http_post':
      return text(n, 'url');
    default:
      return varLine(t, n);
  }
}

function rewardUpdateLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  const reward = optionLabel(t, spec, n, 'reward');
  return given(reward, (name) => (n.config.reset === true ? t('flows.line.rewardUpdateBack', { reward: name }) : t('flows.line.rewardUpdate', { reward: name })));
}

function moduleLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'reply.reward_update':
      return rewardUpdateLine(t, n, spec);
    case 'reply.overlay_show':
      return text(n, 'content');
    case 'reply.overlay_sound':
      return text(n, 'url');
    default:
      return dataLine(t, n, spec);
  }
}

function replyLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'reply.chat':
      return text(n, 'message');
    case 'reply.points':
      return pointsLine(t, n);
    case 'reply.counter':
      return given(optionLabel(t, spec, n, 'counter'), (counter) => t('flows.line.counter', { counter }));
    case 'reply.lights':
      return lightsLine(t, n, spec);
    case 'reply.wait':
      return given(num(n, 'seconds'), (count) => t('flows.line.pause', { amount: seconds(t, count) }));
    default:
      return moduleLine(t, n, spec);
  }
}

function lineFor(t: Translate, node: FlowNode, spec: NodeSpec): string {
  if (spec.kind === 'trigger') return triggerLine(t, node, spec);
  return spec.kind === 'condition' ? conditionLine(t, node) : replyLine(t, node, spec);
}

export function portLabel(t: Translate, node: FlowNode, id: string): string {
  const percent = node.type === 'cond.chance' ? num(node, 'percent') : 0;
  if (percent) return id === 'yes' ? t('flows.line.chanceYes', { percent }) : t('flows.line.chanceNo');
  const spec = specFor(node.type);
  return spec ? outputLabel(t, spec, id) : '';
}

export function describe(t: Translate, node: FlowNode): string {
  const spec = specFor(node.type);
  if (!spec) return '';
  if (awaitingSetup(node)) return t('flows.setup.line');
  return lineFor(t, node, spec) || stepSummary(t, spec);
}
