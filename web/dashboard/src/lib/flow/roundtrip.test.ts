// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { definitionToFlow, flowToDefinition, validateDefinition, type FlowConfig } from '@bagel/kit/automations';
import type { ChoiceSource } from './choices';
import { NODE_SPECS, type FieldSpec, type Flow, type FlowNode, type NodeSpec } from './model';

type Config = Record<string, string | number | boolean>;

const CHOICES: Record<ChoiceSource, { value: string; label: string; meta?: Record<string, string> }> = {
  rewards: { value: 'reward-1', label: 'Hydrate' },
  discordChannels: { value: '223456789012345678', label: '#announcements', meta: { guildId: '123456789012345678' } },
  counters: { value: 'deaths', label: 'deaths' },
  scenes: {
    value: 'snapshot:4:9',
    label: 'Sunset',
    meta: { sceneType: 'devices.capabilities.dynamic_scene', sceneInstance: 'snapshot', sceneValue: '{"paramId":4,"id":9}' }
  }
};

const LIGHT: Config = { device: 'AA:BB:CC:DD:EE:FF:00:11', sku: 'H6008', deviceName: 'Desk lamp' };

const TEXT: Record<string, string> = { trigger: '!hug', word: 'bagel', message: 'Thanks {user}!', left: '{bits}', right: '1000', color: '#f5a524' };

const SHOWN_NOT_SAVED = new Set(['login', 'rewardTitle', 'channelName']);

const withinRange = (field: Extract<FieldSpec, { type: 'number' }>) => Math.min(field.max ?? Infinity, Math.max(field.min ?? 0, 7));

function chosen(config: Config, field: Extract<FieldSpec, { type: 'choice' }>): void {
  const pick = CHOICES[field.source];
  config[field.key] = pick.value;
  if (field.nameKey) config[field.nameKey] = pick.label;
  Object.assign(config, pick.meta);
}

function fill(config: Config, field: FieldSpec): void {
  switch (field.type) {
    case 'number':
      config[field.key] = withinRange(field);
      break;
    case 'toggle':
    case 'select':
      break;
    case 'choice':
      chosen(config, field);
      break;
    case 'goveeLight':
      Object.assign(config, LIGHT);
      break;
    case 'login':
      Object.assign(config, { login: 'bagel', userId: '1234567' });
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
  return variations(spec).map((row) => {
    const config: Config = { ...row };
    const hidden = (field: FieldSpec) => field.when && config[field.when.key] !== field.when.value;
    for (const field of spec.fields) if (!hidden(field)) fill(config, field);
    return config;
  });
}

const node = (id: string, type: string, config: Config): FlowNode => ({ id, type, x: 0, y: 0, config });

function drawn(spec: NodeSpec, config: Config): Flow {
  const chat = node('z', 'reply.chat', { message: 'Thanks {user}!' });
  const port = spec.kind === 'condition' ? 'yes' : 'next';
  if (spec.kind === 'trigger') {
    return { id: 'f', name: 'f', enabled: true, nodes: [node('a', spec.type, config), chat], edges: [{ id: 'e1', from: 'a', port, to: 'z' }] };
  }
  const trigger = node('t', 'twitch.cheer', { minBits: 100 });
  return {
    id: 'f',
    name: 'f',
    enabled: true,
    nodes: [trigger, node('a', spec.type, config), chat],
    edges: [
      { id: 'e1', from: 't', port: 'next', to: 'a' },
      { id: 'e2', from: 'a', port, to: 'z' }
    ]
  };
}

const saved = (config: FlowConfig): Config =>
  Object.fromEntries(Object.entries(config).filter(([key, value]) => value !== '' && !SHOWN_NOT_SAVED.has(key)));

describe('every builder step survives a save and reload', () => {
  for (const spec of NODE_SPECS) {
    test.each(configsFor(spec).map((config) => [JSON.stringify(config), config] as const))(`${spec.type} %s`, (_label, config) => {
      const flow = drawn(spec, config);
      const lowered = flowToDefinition(flow);
      if (!lowered.ok) throw new Error(`${spec.type} was refused: ${JSON.stringify(lowered.issues)}`);
      expect(validateDefinition(lowered.value)).toEqual([]);

      const reopened = definitionToFlow(lowered.value, { id: 'f', name: 'f', enabled: true });
      if (!reopened.ok) throw new Error(`${spec.type} did not reopen: ${JSON.stringify(reopened.issues)}`);
      const back = reopened.value.nodes.find((n) => n.type === spec.type);

      expect(back).toBeDefined();
      expect(saved(back!.config)).toEqual(saved(config));
    });
  }
});
