// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { compileFlow, type FlowNode } from './flow-graph';
import { SCENARIO_FIELDS, defaultScenario, matchTrigger, scenarioKind } from './rehearsal-scenario';
import { rehearseDefinition } from './rehearsal';
import type { Definition, Trigger } from './types';
import type { RehearsalScenario } from './rehearsal-types';

type Text = string;
function lowered(type: Text, config: FlowNode['config']): Definition {
  const result = compileFlow({ id: 'test', name: 'test', enabled: true, nodes: [{ id: 'trigger', x: 0, y: 0, type, config }], edges: [] });
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  return result.value.definition;
}

type DefaultCase = { type: Text; config: FlowNode['config']; values: Partial<RehearsalScenario> };
test.each<DefaultCase>([
  { type: 'twitch.chat.command', config: { trigger: '!hello' }, values: { message: '!hello @Lotti good luck', args: '@Lotti good luck' } },
  { type: 'twitch.chat.word', config: { word: 'café' }, values: { message: 'I love café', args: 'I love café' } },
  { type: 'twitch.chat.word', config: { word: 'a.b!' }, values: { message: 'I love a.b!', args: 'I love a.b!' } },
  { type: 'twitch.follow', config: {}, values: { user: 'PrincessBarney', userId: '40000001', role: 'viewer' } },
  { type: 'twitch.sub', config: { variant: 'new' }, values: { tier: '1000', is_gift: false } },
  { type: 'twitch.sub', config: { variant: 'resub' }, values: { tier: '1000', months: 14 } },
  { type: 'twitch.sub', config: { variant: 'gift' }, values: { tier: '1000', giftCount: 5, anonymous: false } },
  { type: 'twitch.cheer', config: { minBits: 1000 }, values: { bits: 1000 } },
  { type: 'twitch.cheer', config: { minBits: 1 }, values: { bits: 500 } },
  { type: 'twitch.raid', config: { minViewers: 100 }, values: { viewers: 100 } },
  { type: 'twitch.reward', config: { reward: 'r' }, values: { input: 'lofi beats', reward_id: 'r' } },
  { type: 'twitch.unban_request', config: {}, values: { requestText: 'please unban me' } },
  { type: 'twitch.stream.online', config: {}, values: { live: true } },
  { type: 'countdown.finished', config: { name: 'timer' }, values: { name: 'timer', live: true } }
])('default scenario for $type $config', ({ type, config, values }) => {
  const def = lowered(type, config);
  const scenario = defaultScenario(def);
  expect(scenario).toMatchObject(values);
  expect(matchTrigger(def, scenario)).toEqual({ starts: true });
  expect(SCENARIO_FIELDS[scenarioKind(def)]).toContainEqual({ key: 'live', kind: 'toggle', default: true, more: true });
});

type MatchCase = { trigger: Trigger; scenario: Partial<RehearsalScenario>; reason: Text };
test.each<MatchCase>([
  { trigger: { type: 'event', params: { kind: 'channel.cheer', filters: { amount: '1000' } } }, scenario: { bits: 1 }, reason: 'trigger_filter' },
  { trigger: { type: 'event', params: { kind: 'channel.raid', filters: { viewers: '100' } } }, scenario: { viewers: 42 }, reason: 'trigger_filter' },
  { trigger: { type: 'event', params: { kind: 'channel.subscribe', filters: { is_gift: 'false' } } }, scenario: { is_gift: true }, reason: 'trigger_filter' },
  { trigger: { type: 'command', params: { name: '!hello' } }, scenario: { message: '!other' }, reason: 'trigger_message' },
  { trigger: { type: 'reward', params: { reward_id: 'r' } }, scenario: { reward_id: 'other' }, reason: 'trigger_filter' },
  { trigger: { type: 'countdown', params: { name: 'timer' } }, scenario: { name: 'other' }, reason: 'trigger_filter' }
])('failed trigger $trigger', ({ trigger, scenario, reason }) => {
  const def: Definition = { version: 1, trigger, actions: [{ type: 'chat', params: { text: 'never' } }] };
  const result = rehearseDefinition(def, { ...defaultScenario(def), ...scenario }, {});
  expect([result.starts, result.reason?.reason, result.events]).toEqual([false, reason, []]);
});
