// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { DEFAULT_REWARD_OUTCOME, definitionToFlow, flowToDefinition, validateDefinition, type FlowConfig, type FlowResult } from '@bagel/kit/automations';
import type { ChoiceSource } from './choices';
import { NODE_SPECS, fieldVisible, outputsFor, type FieldSpec, type Flow, type FlowNode, type NodeSpec } from './model';

type Config = Record<string, string | number | boolean>;

const CHOICES: Record<ChoiceSource, { value: string; label: string; meta?: Record<string, string> }> = {
  rewards: { value: 'reward-1', label: 'Hydrate' },
  manageableRewards: { value: 'reward-2', label: 'Hydrate' },
  counters: { value: 'deaths', label: 'deaths' },
  scenes: {
    value: 'snapshot:4:9',
    label: 'Sunset',
    meta: { sceneType: 'devices.capabilities.dynamic_scene', sceneInstance: 'snapshot', sceneValue: '{"paramId":4,"id":9}' }
  },
  namedCooldowns: { value: 'subathon', label: 'subathon, in Big cheer celebration' },
  namedCountdowns: { value: 'subathon', label: 'subathon, in Big cheer celebration' },
  recipes: { value: 'recipe-1', label: 'Helper' }
};

const LIGHT: Config = { device: 'AA:BB:CC:DD:EE:FF:00:11', sku: 'H6008', deviceName: 'Desk lamp' };

const TEXT: Record<string, string> = {
  trigger: '!hug',
  aliases: '!cuddle',
  choices: 'One\nTwo',
  outcomes: 'Yes\nNo',
  word: 'bagel',
  message: 'Thanks {user}!',
  left: '{bits}',
  right: '1000',
  color: '#f5a524',
  times: '3',
  who: 'viewer',
  url: 'https://api.example.com/data',
  content: 'https://cdn.example.com/a.png',
  jsonPath: 'data.value'
};

const SHOWN_NOT_SAVED = new Set(['login', 'rewardTitle']);

const withinRange = (field: Extract<FieldSpec, { type: 'number' }>) => Math.min(field.max ?? Infinity, Math.max(field.min ?? 0, 7));

function chosen(config: Config, field: Extract<FieldSpec, { type: 'choice' }>): void {
  const pick = CHOICES[field.source];
  config[field.key] = pick.value;
  if (field.nameKey) config[field.nameKey] = pick.label;
  Object.assign(config, pick.meta);
}

const STATIC_FIELD_CONFIGS = new Map<FieldSpec['type'], Config>([
  ['toggle', {}],
  ['select', {}],
  ['goveeLight', LIGHT],
  ['login', { login: 'bagel', userId: '1234567' }]
]);

function staticFieldConfig(field: FieldSpec): Config | undefined {
  if (field.key === 'users') return { users: '1234567:,2345678:' };
  return STATIC_FIELD_CONFIGS.get(field.type);
}

function fill(config: Config, field: FieldSpec): void {
  const fixed = staticFieldConfig(field);
  if (fixed) {
    Object.assign(config, fixed);
    return;
  }
  switch (field.type) {
    case 'number':
      config[field.key] = withinRange(field);
      break;
    case 'duration':
      config[field.key] = field.max;
      break;
    case 'slider':
      config[field.key] = field.max - field.step;
      break;
    case 'choice':
      chosen(config, field);
      break;
    case 'reward':
      Object.assign(config, { [field.key]: 'sample', onRedeem: DEFAULT_REWARD_OUTCOME });
      break;
    default:
      config[field.key] = TEXT[field.key] ?? 'sample';
  }
}

function variations(spec: NodeSpec): Config[] {
  const choices = spec.fields.filter((f): f is Extract<FieldSpec, { type: 'select' | 'toggle' }> => f.type === 'select' || f.type === 'toggle');
  return choices.reduce<Config[]>(
    (rows, field) => {
      const values = field.type === 'toggle' ? [true, false] : field.options;
      return rows.flatMap((row) => values.map((value) => ({ ...row, [field.key]: value })));
    },
    [{}]
  );
}

function configsFor(spec: NodeSpec): Config[] {
  const trigger = triggerFor(spec);
  return variations(spec).map((row) => {
    const config: Config = { ...row };
    for (const field of spec.fields) if (fieldVisible(spec, { config }, field, trigger)) fill(config, field);
    return config;
  });
}

const node = (id: string, type: string, config: Config): FlowNode => ({ id, type, x: 0, y: 0, config });

function triggerFor(spec: NodeSpec): FlowNode {
  if (spec.type === 'cond.pattern') return node('t', 'twitch.chat.command', { trigger: '!check' });
  if (spec.type === 'reply.unban_resolve') return node('t', 'twitch.unban_request', {});
  if (spec.type === 'reply.redemption') return node('t', 'twitch.reward', { reward: 'reward-1' });
  return node('t', 'twitch.cheer', { minBits: 100 });
}

function drawn(spec: NodeSpec, config: Config): Flow {
  const step = node('a', spec.type, config);
  const chat = node('z', 'reply.chat', { message: 'Thanks {user}!' });
  const port = outputsFor(spec, step)[0]?.id;
  if (spec.kind === 'trigger') {
    return { id: 'f', name: 'f', enabled: true, nodes: [step, chat], edges: [{ id: 'e1', from: 'a', port: port!, to: 'z' }] };
  }
  const trigger = triggerFor(spec);
  return {
    id: 'f',
    name: 'f',
    enabled: true,
    nodes: port ? [trigger, step, chat] : [trigger, step],
    edges: [
      { id: 'e1', from: 't', port: 'next', to: 'a' },
      ...(port ? [{ id: 'e2', from: 'a', port, to: 'z' }] : [])
    ]
  };
}

function saved(spec: NodeSpec, config: FlowConfig): Config {
  const hidden = spec.fields.filter((field) => !fieldVisible(spec, { config }, field)).map((field) => field.key);
  const omitted = new Set([...SHOWN_NOT_SAVED, ...hidden]);
  if (spec.type === 'reply.run') omitted.add('name');
  return Object.fromEntries(Object.entries(config).filter(([key, value]) => value !== '' && !omitted.has(key)));
}

function unwrap<T>(result: FlowResult<T>): T {
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value;
}

describe('every builder step survives a save and reload', () => {
  const cases = NODE_SPECS.filter((spec) => !spec.hidden && spec.type !== 'reply.delete_message').flatMap((spec) => {
    const configs = configsFor(spec);
    if (spec.kind === 'reply') configs.push({ ...configs[0], disabled: true });
    return configs.map((config) => ({ type: spec.type, variant: JSON.stringify(config), spec, config }));
  });

  test.each(cases)('$type $variant', ({ spec, config }) => {
    const lowered = unwrap(flowToDefinition(drawn(spec, config)));
    expect(validateDefinition(lowered)).toEqual([]);

    const reopened = unwrap(definitionToFlow(lowered, { id: 'f', name: 'f', enabled: true }));
    const back = reopened.nodes.find((n) => n.type === spec.type);

    expect({
      enabled: lowered.actions?.[0]?.enabled,
      config: back && saved(spec, back.config)
    }).toEqual({
      enabled: config.disabled ? false : undefined,
      config: saved(spec, config)
    });
  });
});
