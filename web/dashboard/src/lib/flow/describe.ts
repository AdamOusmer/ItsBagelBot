// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import { compactDuration, cooldownWindows, flowList, viewerList } from '@bagel/kit/automations';
import { optionText, outputLabel, stepSummary } from './copy';
import type { Translate } from './messages';
import { LINKED_REWARD_CHOICE, specFor, type FieldSpec, type FlowNode, type NodeSpec } from './model';
import { awaitingSetup, configText } from './setup';

const COMPARISONS: Readonly<Record<string, MessageKey>> = Object.freeze({
  gte: 'flows.line.compareGte',
  lt: 'flows.line.compareLt',
  eq: 'flows.line.compareEq',
  contains: 'flows.line.compareContains',
  ne: 'flows.line.compareNe',
  gt: 'flows.line.compareGt',
  lte: 'flows.line.compareLte',
  starts: 'flows.line.compareStarts'
});

const EMPTY_COMPARISONS: Readonly<Record<string, MessageKey>> = Object.freeze({
  eq: 'flows.line.compareEmpty',
  ne: 'flows.line.compareNotEmpty'
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

function namedLabel(node: FlowNode, field: FieldSpec, value: string): string | null {
  if (field.type === 'reward') return text(node, 'title') || (text(node, LINKED_REWARD_CHOICE.nameKey) || value);
  if (field.type === 'choice') return text(node, field.nameKey ?? field.key) || value;
  return null;
}

export function optionLabel(t: Translate, spec: NodeSpec, node: FlowNode, key: string): string {
  const field = spec.fields.find((f) => f.key === key);
  const value = text(node, key);
  if (!field) return value;
  return namedLabel(node, field, value) ?? (field.type === 'select' ? selectLabel(t, spec, field, value) : value);
}

function triggerLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'twitch.chat.command':
      return given(text(n, 'trigger'), (trigger) => t('flows.line.command', { trigger }));
    case 'twitch.chat.word':
      return given(text(n, 'word'), (word) => t('flows.line.word', { word }));
    case 'twitch.sub':
      return subLine(t, text(n, 'variant') || 'any');
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
  const right = configText(n, 'right');
  if (!left) return '';
  const op = text(n, 'op') || 'gte';
  if (right === '') return Object.hasOwn(EMPTY_COMPARISONS, op) ? t(EMPTY_COMPARISONS[op]!, { left, right }) : '';
  return Object.hasOwn(COMPARISONS, op) ? t(COMPARISONS[op]!, { left, right }) : t('flows.line.compareIs', { left, right });
}

const minutes = (t: Translate, count: number) => (count === 1 ? t('flows.line.minutesOne', { count }) : t('flows.line.minutesMany', { count }));

const hours = (t: Translate, count: number) => (count === 1 ? t('flows.line.hoursOne', { count }) : t('flows.line.hoursMany', { count }));

const days = (t: Translate, count: number) => (count === 1 ? t('flows.line.daysOne', { count }) : t('flows.line.daysMany', { count }));

function cooldownAmount(t: Translate, total: number): string {
  if (total % 86_400 === 0) return days(t, total / 86_400);
  if (total % 3600 === 0) return hours(t, total / 3600);
  if (total % 60 === 0) return minutes(t, total / 60);
  return seconds(t, total);
}

function cooldownLine(t: Translate, n: FlowNode): string {
  const { channelSeconds, viewerSeconds } = cooldownWindows(n.config);
  const channel = channelSeconds > 0 ? cooldownAmount(t, channelSeconds) : '';
  const viewer = viewerSeconds > 0 ? cooldownAmount(t, viewerSeconds) : '';
  if (channel && viewer) return t('flows.line.cooldownBoth', { channel, viewer });
  if (viewer) return t('flows.line.cooldownViewer', { amount: viewer });
  return channel ? t('flows.line.cooldown', { amount: channel }) : '';
}

function varCheckLine(t: Translate, n: FlowNode): string {
  const spec = specFor(n.type)!;
  const key = text(n, 'name');
  if (!key) return '';
  const name = t(n.config.scope === 'viewer' ? 'flows.line.varViewer' : 'flows.line.varChannel', { name: key });
  const op = text(n, 'op');
  if (op === 'exists') return t('flows.line.varExists', { name });
  if (op === 'not_exists') return t('flows.line.varNotExists', { name });
  return t('flows.line.varCheck', { name, op: optionLabel(t, spec, n, 'op'), value: text(n, 'value') });
}

export function viewerNames(node: FlowNode): string[] {
  return viewerList(node.config).map(({ id, login }) => login ? `@${login}` : id);
}

function viewerLine(t: Translate, n: FlowNode): string {
  const [first, second, ...remaining] = viewerNames(n);
  if (!first) return '';
  const pair = second ? t('flows.line.userPair', { first, second }) : first;
  const user = remaining.length ? t('flows.line.userMore', { first, second: second!, count: remaining.length }) : pair;
  return t(n.config.mode === 'except' ? 'flows.line.userExcept' : 'flows.line.userOnly', { user });
}

function conditionLine(t: Translate, n: FlowNode): string {
  switch (n.type) {
    case 'cond.var':
      return varCheckLine(t, n);
    case 'cond.role':
      return roleLine(t, text(n, 'level') || 'everyone');
    case 'cond.user':
      return viewerLine(t, n);
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
  return n.type === 'reply.reward_update' ? rewardUpdateLine(t, n, spec) : dataLine(t, n, spec);
}

function streamLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'reply.shoutout':
      return given(text(n, 'target'), (target) => t('flows.line.shoutout', { target }));
    case 'reply.announce':
      return given(text(n, 'text'), (message) => t('flows.line.announce', { message }));
    case 'reply.reply':
      return given(text(n, 'text'), (message) => t('flows.line.reply', { message }));
    case 'reply.timeout':
      return given(text(n, 'target'), (target) => t('flows.line.timeout', { target, amount: cooldownAmount(t, num(n, 'seconds')) }));
    default:
      return moduleLine(t, n, spec);
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
      return streamLine(t, n, spec);
  }
}

function lineFor(t: Translate, node: FlowNode, spec: NodeSpec): string {
  if (spec.kind === 'trigger') return triggerLine(t, node, spec);
  return spec.kind === 'condition' ? conditionLine(t, node) : replyLine(t, node, spec);
}

type ConfigValue = FlowNode['config'][string] | undefined;

export function durationFormatter(t: Translate): (seconds: number) => string {
  const units = [t('flows.test.duration.day'), t('flows.test.duration.hour'), t('flows.test.duration.minute'), t('flows.test.duration.second')];
  return (seconds) => compactDuration(seconds, units);
}

function lineVariant(value: ConfigValue): string {
  if (typeof value === 'boolean') return value ? 'on' : 'off';
  return String(value ?? '').trim();
}

function lineValue(t: Translate, node: FlowNode, spec: NodeSpec, key: string): string {
  if (node.type === 'cond.category' && key === 'games') return gamesLine(t, node);
  if (node.type === 'cond.accountAge' && key === 'minDays') return String(node.config.minDays ?? 7);
  if (!text(node, key)) return '';
  const field = spec.fields.find((entry) => entry.key === key);
  if (field?.type === 'duration') return given(num(node, key), durationFormatter(t));
  return optionLabel(t, spec, node, key);
}

function gamesLine(t: Translate, node: FlowNode): string {
  const games = flowList(node.config.games);
  if (games.length < 2) return games[0] ?? '';
  return t('flows.line.gameAlternatives', { first: games.slice(0, -1).join(', '), last: games.at(-1)! });
}

function specLineVariant(node: FlowNode, spec: NodeSpec): string {
  if (!spec.lineBy) return '';
  const initial = spec.kind === 'condition' ? spec.fields.find((field) => field.key === spec.lineBy)?.initial : undefined;
  return `.${lineVariant(node.config[spec.lineBy] ?? initial)}`;
}

function specLine(t: Translate, node: FlowNode, spec: NodeSpec): string | undefined {
  if (node.type === 'cond.follow' && !num(node, 'minDays')) return t('flows.step.condition.follow.lineAny');
  const suffix = specLineVariant(node, spec);
  const template = t(`flows.step.${spec.kind}.${spec.slug}.line${suffix}` as MessageKey);
  if (template === `flows.step.${spec.kind}.${spec.slug}.line${suffix}`) return undefined;
  let missing = false;
  const line = template.replace(/\{(\w+)\}/g, (_, key: string) => {
    const value = lineValue(t, node, spec, key);
    if (!value) missing = true;
    return value;
  });
  return missing ? '' : line;
}

export function portLabel(t: Translate, node: FlowNode, id: string): string {
  const percent = node.type === 'cond.chance' ? num(node, 'percent') : 0;
  if (percent) return id === 'yes' ? t('flows.line.chanceYes', { percent }) : t('flows.line.chanceNo');
  if (node.type === 'cond.live' && node.config.state === 'offline') id = id === 'yes' ? 'no' : 'yes';
  const spec = specFor(node.type);
  return spec ? outputLabel(t, spec, id) : '';
}

export function describe(t: Translate, node: FlowNode): string {
  const spec = specFor(node.type);
  if (!spec) return '';
  if (awaitingSetup(node)) return t('flows.setup.line');
  return (specLine(t, node, spec) ?? lineFor(t, node, spec)) || stepSummary(t, spec);
}
