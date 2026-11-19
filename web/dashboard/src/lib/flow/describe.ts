// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { MessageKey } from '@bagel/kit';
import { activeRandomPaths, compactDuration, cooldownWindows, flowList, pointsChange, randomPathWeight, triggerAmountUnit, viewerList } from '@bagel/kit/automations';
import { optionText, outputLabel, randomPathLabel, stepSummary } from './copy';
import type { Translate } from './messages';
import { INITIAL_LIGHT_KELVIN, LINKED_REWARD_CHOICE, specFor, type FieldSpec, type FlowNode, type NodeSpec } from './model';
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

type SelectField = Extract<FieldSpec, { type: 'select' }>;

function selectLabel(t: Translate, spec: NodeSpec, field: SelectField, value: string): string {
  if (field.options.includes(value)) return optionText(t, spec, field, value);
  const first = String(field.initial ?? field.options[0] ?? '');
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

function commandLine(t: Translate, node: FlowNode): string {
  const trigger = text(node, 'trigger');
  if (!trigger) return '';
  const aliases = text(node, 'aliases');
  const imported = text(node, 'importedAliases').split('\n');
  const count = (aliases === imported.join(', ') ? imported : aliases.split(',')).filter((alias) => alias.trim()).length;
  if (count === 1) return t('flows.step.trigger.chatCommand.line.aliasOne', { trigger });
  if (count > 1) return t('flows.step.trigger.chatCommand.line.aliasMany', { trigger, count });
  return t('flows.step.trigger.chatCommand.line.start', { trigger });
}

function subLine(t: Translate, node: FlowNode, spec: NodeSpec): string {
  const variant = text(node, 'variant') || 'any';
  const line = t(`flows.step.trigger.sub.line.${variant}` as MessageKey);
  if (line === `flows.step.trigger.sub.line.${variant}`) return '';
  const tier = text(node, 'tier');
  if (!tier || tier === 'any') return line;
  return t('flows.step.trigger.sub.withTier', { line, tier: optionLabel(t, spec, node, 'tier') });
}

function triggerLine(t: Translate, node: FlowNode, spec: NodeSpec): string {
  const lines: Readonly<Record<string, () => string>> = {
    'twitch.chat.command': () => commandLine(t, node),
    'twitch.sub': () => subLine(t, node, spec),
    'twitch.reward': () => given(optionLabel(t, spec, node, 'reward'), (reward) => t('flows.line.reward', { reward })),
    'twitch.ad_break': () => {
      const length = num(node, 'minLength');
      return length ? t('flows.step.trigger.adBreak.line.minimum', { length: durationFormatter(t)(length) }) : t('flows.step.trigger.adBreak.line.start');
    },
    'schedule.timer': () => given(num(node, 'every'), (count) => t('flows.step.trigger.timer.line', { amount: cooldownAmount(t, count) }))
  };
  return lines[node.type]?.() ?? specLine(t, node, spec) ?? '';
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

function counterCheckLine(t: Translate, n: FlowNode): string {
  const name = text(n, 'counter');
  const value = text(n, 'value');
  if (!name || !value) return '';
  return t('flows.line.varCheck', { name, op: optionLabel(t, specFor(n.type)!, n, 'op'), value });
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
    case 'cond.counter':
      return counterCheckLine(t, n);
    default:
      return '';
  }
}

function counterLine(t: Translate, n: FlowNode): string {
  const counter = text(n, 'counter');
  if (!counter) return '';
  if (n.config.change === 'set') return given(text(n, 'value'), (value) => t('flows.step.reply.counter.line.set', { counter, value }));
  const amount = Number(n.config.amount ?? (n.config.change === undefined ? 1 : 0));
  if (!amount) return '';
  const key = amount < 0 ? 'flows.step.reply.counter.line.take' : 'flows.step.reply.counter.line.add';
  return t(key, { counter, amount: Math.abs(amount) });
}

const LIGHT_LINES: Readonly<Record<string, MessageKey>> = Object.freeze({
  color: 'flows.line.lightsColor',
  scene: 'flows.line.lightsScene',
  white: 'flows.line.lightsWhite',
  on: 'flows.line.lightsOn',
  off: 'flows.line.lightsOff'
});

export function lightGroupLabel(t: Translate, name: string, count: number): string {
  const light = name || t('flows.line.lightsFallback');
  return count > 0 ? t('flows.line.lightsMore', { light, count }) : light;
}

function lightsLine(t: Translate, node: FlowNode, spec: NodeSpec): string {
  const selected = text(node, 'mode');
  const mode = Object.hasOwn(LIGHT_LINES, selected) ? selected : 'color';
  const color = text(node, 'color');
  const scene = optionLabel(t, spec, node, 'scene');
  if (mode === 'color' && !color) return '';
  if (mode === 'scene' && !scene) return '';

  const count = text(node, 'extraLights').split(',').filter(Boolean).length;
  const light = lightGroupLabel(t, text(node, 'deviceName'), count);
  const kelvin = num(node, 'kelvin') || INITIAL_LIGHT_KELVIN;
  const line = t(LIGHT_LINES[mode]!, { light, color, scene, kelvin });
  return lightsSuffix(t, node, { line, mode });
}

function lightsSuffix(t: Translate, n: FlowNode, { line, mode }: { line: string; mode: string }): string {
  const percent = mode === 'off' ? 0 : num(n, 'brightness');
  const seconds = num(n, 'holdS');
  const bright = percent ? t('flows.line.lightsBrightness', { line, percent }) : line;
  return seconds ? t('flows.line.lightsHold', { line: bright, seconds }) : bright;
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
      return '';
  }
}

const REWARD_TOGGLE_LINES: Readonly<Record<string, Readonly<Record<string, MessageKey>>>> = {
  setEnabled: { on: 'flows.step.reply.rewardUpdate.line.on', off: 'flows.step.reply.rewardUpdate.line.off' },
  paused: { pause: 'flows.step.reply.rewardUpdate.line.pause', resume: 'flows.step.reply.rewardUpdate.line.resume' }
};

function rewardUpdateLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  const reward = optionLabel(t, spec, n, 'reward');
  if (!reward) return '';
  const lines = Object.entries(REWARD_TOGGLE_LINES).flatMap(([field, choices]) => {
    const key = choices[text(n, field)];
    return key ? [t(key, { reward })] : [];
  });
  const cost = num(n, 'cost');
  if (cost) lines.push(t('flows.step.reply.rewardUpdate.line.cost', { reward, cost: points(t, cost) }));
  const cooldown = rewardCooldownLine(t, n, reward);
  if (cooldown) lines.push(cooldown);
  const changes = lines.join(', ') || t('flows.line.rewardUpdate', { reward });
  return n.config.reset === true ? t('flows.step.reply.rewardUpdate.line.untilEnd', { changes }) : changes;
}

function rewardCooldownLine(t: Translate, node: FlowNode, reward: string): string {
  const stored = text(node, 'cooldown');
  if (!stored) return '';
  if (Number(stored) === 0) return t('flows.step.reply.rewardUpdate.line.cooldownOff', { reward });
  return given(num(node, 'cooldown'), (seconds) => t('flows.step.reply.rewardUpdate.line.cooldown', { reward, cooldown: durationFormatter(t)(seconds) }));
}

function moduleLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  return n.type === 'reply.reward_update' ? rewardUpdateLine(t, n, spec) : dataLine(t, n, spec);
}

function streamInfoLine(t: Translate, node: FlowNode): string {
  const branded = { on: 'brandedOn', off: 'brandedOff' } as const;
  const choice = node.config.branded;
  const lines = [
    ...(['title', 'game', 'tags'] as const).map((field) =>
      given(text(node, field), (value) => t(`flows.step.reply.streamInfo.line.${field}`, { [field]: value }))),
    choice === 'on' || choice === 'off' ? t(`flows.step.reply.streamInfo.line.${branded[choice]}`) : ''
  ].filter(Boolean);
  const [line = ''] = lines;
  return lines.length > 1 ? t('flows.step.reply.streamInfo.line.more', { count: lines.length - 1, line }) : line;
}

function streamLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'reply.shoutout':
      return given(text(n, 'target'), (target) => t('flows.line.shoutout', { target }));
    case 'reply.announce':
      return given(text(n, 'text'), (message) => t('flows.line.announce', { message }));
    case 'reply.reply':
      return given(text(n, 'text'), (message) => t('flows.line.reply', { message }));
    case 'reply.stream_info':
      return streamInfoLine(t, n);
    default:
      return moduleLine(t, n, spec);
  }
}

function replyLine(t: Translate, n: FlowNode, spec: NodeSpec): string {
  switch (n.type) {
    case 'reply.chat':
      return text(n, 'message');
    case 'reply.counter':
      return counterLine(t, n);
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
  return specLine(t, node, spec) ?? (spec.kind === 'condition' ? conditionLine(t, node) : replyLine(t, node, spec));
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

const WHO_LABELS: Readonly<Record<string, MessageKey>> = Object.freeze({
  viewer: 'flows.line.whoViewer',
  mention: 'flows.line.whoMention'
});

function whoLabel(t: Translate, node: FlowNode): string {
  const who = text(node, 'who');
  if (Object.hasOwn(WHO_LABELS, who)) return t(WHO_LABELS[who]!);
  if (who === 'user') return text(node, 'login') || text(node, 'userId');
  return text(node, 'target');
}

function lineValue(t: Translate, node: FlowNode, spec: NodeSpec, key: string): string {
  if (key === 'who') return whoLabel(t, node);
  const special = SPECIAL_LINE_VALUES[`${node.type}:${key}`];
  if (special) return special(t, node);
  const field = spec.fields.find((entry) => entry.key === key || (entry.type === 'choice' && entry.nameKey === key));
  const configKey = field?.key ?? key;
  if (!text(node, configKey)) return '';
  if (node.type === 'reply.chat_mode' && key === 'wait') return `${num(node, key)} ${t('flows.test.duration.second')}`;
  if (field?.type === 'duration') return given(num(node, key), durationFormatter(t));
  if (spec.kind === 'trigger' && field?.type === 'number') return given(num(node, key), String);
  return optionLabel(t, spec, node, configKey);
}

function lineSuffix(node: FlowNode, spec: NodeSpec): string {
  if (!spec.lineBy) return '';
  if (node.type === 'reply.points') return `.${pointsChange(node.config)}`;
  const field = spec.fields.find((entry) => entry.key === spec.lineBy);
  const variant = lineVariant(node.config[spec.lineBy] ?? lineFallback(spec, field));
  const showWait = node.type === 'reply.chat_mode' && node.config.mode === 'slow' && Boolean(node.config.wait);
  return variant === 'on' && showWait ? '.slow' : `.${variant}`;
}

function gamesLine(t: Translate, node: FlowNode): string {
  const games = flowList(node.config.games);
  if (games.length < 2) return games[0] ?? '';
  return t('flows.line.gameAlternatives', { first: games.slice(0, -1).join(', '), last: games.at(-1)! });
}

function lineFallback(spec: NodeSpec, field: FieldSpec | undefined): ConfigValue {
  if (field?.type === 'select' && field.fallback !== undefined) return field.fallback;
  return field?.initial;
}

const SPECIAL_LINE_VALUES: Readonly<Record<string, (t: Translate, node: FlowNode) => string>> = {
  'cond.category:games': gamesLine,
  'cond.accountAge:minDays': (_t, node) => String(node.config.minDays ?? 7),
  'reply.points:amount': (t, node) => given(Math.abs(Number(node.config.amount)), (count) => points(t, count)),
  'reply.points_spend:amount': (t, node) => given(num(node, 'amount'), (count) => points(t, count))
};

function specLine(t: Translate, node: FlowNode, spec: NodeSpec): string | undefined {
  if (node.type === 'cond.follow' && !num(node, 'minDays')) return t('flows.step.condition.follow.lineAny');
  const key = `flows.step.${spec.kind}.${spec.slug}.line${lineSuffix(node, spec)}` as MessageKey;
  const template = t(key);
  if (template === key) return undefined;
  let missing = false;
  const line = template.replace(/\{(\w+)\}/g, (_, key: string) => {
    const value = lineValue(t, node, spec, key);
    if (!value) missing = true;
    return value;
  });
  return missing ? '' : line;
}

type TriggerContext = Pick<FlowNode, 'type' | 'config'>;

function timerPerLine(t: Translate, node: FlowNode, trigger?: TriggerContext): string {
  if (node.type !== 'reply.countdown_add' && node.type !== 'reply.cooldown_add') return '';
  const unit = triggerAmountUnit(trigger);
  const per = num(node, 'per');
  const seconds = num(node, 'seconds');
  const name = text(node, 'name');
  if (!unit || !per || !seconds || !name) return '';
  const amount = t(`flows.line.timerAmount.${unit}.${per === 1 ? 'one' : 'many'}`, { count: per });
  return t('flows.line.timerPer', { name, seconds: durationFormatter(t)(seconds), amount });
}

export function portLabel(t: Translate, node: FlowNode, id: string): string {
  if (node.type === 'reply.random') return randomPortLabel(t, node, id);
  const percent = node.type === 'cond.chance' ? num(node, 'percent') : 0;
  if (percent) return id === 'yes' ? t('flows.line.chanceYes', { percent }) : t('flows.line.chanceNo');
  if (node.type === 'cond.live' && node.config.state === 'offline') id = id === 'yes' ? 'no' : 'yes';
  const spec = specFor(node.type);
  return spec ? outputLabel(t, spec, id) : '';
}

function randomPortLabel(t: Translate, node: FlowNode, id: FlowNode['id']): string {
  const paths = activeRandomPaths(node.config);
  const selected = paths.find((path) => path.id === id);
  if (!selected) return '';
  const weight = (key: keyof FlowNode['config']) => Math.max(0, randomPathWeight(node.config, key));
  const total = paths.reduce((sum, path) => sum + weight(path.weightKey), 0);
  const percent = total ? Math.round(100 * weight(selected.weightKey) / total) : 0;
  return t('flows.step.reply.random.pathPercent', { label: randomPathLabel(t, selected.path), percent });
}

export function describe(t: Translate, node: FlowNode, trigger?: TriggerContext): string {
  const spec = specFor(node.type);
  if (!spec) return '';
  if (awaitingSetup(node)) return t('flows.setup.line');
  return timerPerLine(t, node, trigger) || lineFor(t, node, spec) || stepSummary(t, spec);
}
