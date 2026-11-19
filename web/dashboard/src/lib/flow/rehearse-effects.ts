// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MessageKey } from '@bagel/kit';
import { configKey, defaultScenario, SCENARIO_FIELDS, scenarioKind, scenarioPatches, scenarioStateFields } from '@bagel/kit/automations';
import type { Definition, RehearsalScenario, ScenarioField, ScenarioInputField, ScenarioStateField } from '@bagel/kit/automations';
import { describe, durationFormatter } from './describe';
import type { Translate } from './messages';
import type { FlowNode } from './model';

type Detail = Record<string, string>;

const FIELD_LABELS: Readonly<Record<string, MessageKey>> = {
  user: 'flows.test.field.user', userId: 'flows.test.field.userId', role: 'flows.test.field.role', live: 'flows.test.field.live',
  message: 'flows.test.field.message', bits: 'flows.test.field.bits', amount: 'flows.test.field.amount', viewers: 'flows.test.field.viewers',
  tier: 'flows.test.field.tier', months: 'flows.test.field.months', giftCount: 'flows.test.field.giftCount', anonymous: 'flows.test.field.anonymous',
  input: 'flows.test.field.input', requestText: 'flows.test.field.requestText', isGift: 'flows.test.field.isGift',
  rewardId: 'flows.test.field.rewardId', reward_id: 'flows.test.field.rewardId', name: 'flows.test.field.name', is_gift: 'flows.test.field.isGift'
};

export const scenarioFieldLabel = (t: Translate, key: string): string => t(FIELD_LABELS[key] ?? 'flows.test.notSet');

function seedInputLabel(t: Translate, field: ScenarioStateField): string {
  if (field.seed === 'points') return t('flows.test.field.points');
  if (field.seed === 'counters') return t('flows.test.field.counter', { name: field.name });
  return field.kind === 'toggle' ? t('flows.test.field.timerRunning', { name: field.name }) : t('flows.test.field.timerLeft');
}

export function inputLabel(t: Translate, field: ScenarioInputField): string {
  if ('seed' in field) return seedInputLabel(t, field);
  switch (String(field.key)) {
    case 'bits':
    case 'amount': return t('flows.test.inputBits');
    case 'viewers': return t('flows.test.inputViewers');
    default: return scenarioFieldLabel(t, field.key);
  }
}

function pointsResult(t: Translate, detail: Detail, node: FlowNode): string | undefined {
  if (detail.value === undefined) return undefined;
  if (detail.code === 'insufficient_points') return t('flows.test.pointsInsufficient', { value: detail.value });
  if (detail.type === 'points_set') return t('flows.test.pointsSetResult', { value: detail.value });
  return t('flows.test.pointsResult', { action: describe(t, node), value: detail.value });
}

function rewardCooldownResult(t: Translate, seconds: string): string {
  const value = Number(seconds);
  return value ? t('flows.test.rewardCooldown', { cooldown: durationFormatter(t)(value) }) : t('flows.test.rewardCooldownOff');
}

function rewardResult(t: Translate, detail: Detail, node: FlowNode): string {
  const lines: Readonly<Record<string, (value: string) => string>> = {
    enabled: (value) => t(value === 'true' ? 'flows.test.rewardOn' : 'flows.test.rewardOff'),
    cost: (cost) => t('flows.test.rewardCost', { cost }),
    paused: (value) => t(value === 'true' ? 'flows.test.rewardPaused' : 'flows.test.rewardResumed'),
    cooldown_s: (value) => rewardCooldownResult(t, value)
  };
  const changes = Object.entries(lines).flatMap(([field, line]) => detail[field] ? [line(detail[field])] : []).join(', ');
  const result = detail.reset_on_stream_end === 'true' ? t('flows.test.rewardUntilEnd', { changes }) : changes;
  return t('flows.test.rewardResult', { name: String(node.config.rewardTitle || detail.reward_id || ''), changes: result });
}

const EFFECT_LINES: Readonly<Record<string, 'points' | 'counter' | 'timer' | 'reward'>> = {
  points_add: 'points', points_set: 'points',
  counter_add: 'counter', counter_set: 'counter',
  countdown_add: 'timer', cooldown_add: 'timer',
  reward_update: 'reward',
  points_spend: 'points'
};

function effectLine(t: Translate, detail: Detail, node: FlowNode): string {
  switch (EFFECT_LINES[detail.type ?? '']) {
    case 'points': return pointsResult(t, detail, node) ?? describe(t, node);
    case 'counter': return t('flows.test.counterResult', { name: detail.name!, value: detail.value! });
    case 'timer': return t('flows.test.timerResult', { name: detail.name!, left: durationFormatter(t)(Number(detail.left)) });
    case 'reward': return rewardResult(t, detail, node);
    default: return describe(t, node);
  }
}

const COMPUTED_VALUE_NODES = new Set(['reply.math', 'reply.text']);

export function resultText(t: Translate, detail: Detail, node: FlowNode): string {
  if (COMPUTED_VALUE_NODES.has(node.type) && detail.value !== undefined) return detail.value;
  const expanded = Object.entries(detail).flatMap(([param, value]) => {
    const key = configKey(param);
    const displayExpanded = typeof node.config[key] === 'string' || param === 'login';
    return displayExpanded ? [[key, value] as const] : [];
  });
  const config = { ...node.config, ...Object.fromEntries(expanded) };
  return effectLine(t, detail, { ...node, config });
}

function withCounterSeeds(scenario: RehearsalScenario, fields: readonly ScenarioStateField[]): RehearsalScenario {
  const defaults = fields.flatMap((field) => field.seed === 'counters' ? [[field.name, field.default] as const] : []);
  if (!defaults.length) return scenario;
  const supplied = Object.entries(scenario.state?.counters ?? {}).map(([name, count]) => [name.trim().toLowerCase(), count] as const);
  return { ...scenario, state: { ...scenario.state, counters: Object.fromEntries([...defaults, ...supplied]) } };
}

function initialScenario(definition: Definition, fields: readonly ScenarioField[]): RehearsalScenario {
  const scenario = defaultScenario(definition);
  for (const field of fields.filter((field) => field.key === 'bits' || field.key === 'viewers')) Object.assign(scenario, { [field.key]: field.default });
  return scenario;
}

export function rehearsalInputs(definition: Definition, supplied: Partial<RehearsalScenario> | undefined) {
  const kind = scenarioKind(definition);
  const eventFields = SCENARIO_FIELDS[kind] ?? [];
  const seeds = scenarioStateFields(definition);
  const fields = [...eventFields, ...seeds];
  const scenario = { ...initialScenario(definition, eventFields), ...scenarioPatches(fields, supplied ?? {}) };
  return { kind, fields, scenario: withCounterSeeds(scenario, seeds) };
}
