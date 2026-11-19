// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { compileFlow, type FlowNode } from './flow-graph';
import { SCENARIO_FIELDS, defaultScenario, matchTrigger, scenarioKind, scenarioPatches, scenarioStateFields } from './rehearsal-scenario';
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
  { type: 'twitch.chat.command', config: { trigger: '!Hello', match: 'exact', caseSensitive: true }, values: { message: '!Hello', args: '' } },
  { type: 'twitch.chat.word', config: { word: 'café' }, values: { message: 'I love café', args: 'I love café' } },
  { type: 'twitch.chat.word', config: { word: 'a.b!' }, values: { message: 'I love a.b!', args: 'I love a.b!' } },
  { type: 'twitch.chat.word', config: { word: 'a\\1', match: 'anywhere' }, values: { message: 'I love a\\1', args: '' } },
  { type: 'twitch.follow', config: {}, values: { user: 'PrincessBarney', userId: '40000001' } },
  { type: 'twitch.sub', config: { variant: 'new' }, values: { tier: '1000', isGift: false } },
  { type: 'twitch.sub', config: { variant: 'gifted', tier: '2000' }, values: { tier: '2000', isGift: true } },
  { type: 'twitch.sub', config: { variant: 'resub' }, values: { tier: '1000', months: 14, streak: 3, message: 'Glad to be back!' } },
  { type: 'twitch.sub', config: { variant: 'gift' }, values: { tier: '1000', giftCount: 5, total: 20, anonymous: false } },
  { type: 'twitch.sub_gift', config: { minGifts: 7 }, values: { giftCount: 7, total: 20 } },
  { type: 'twitch.cheer', config: { minBits: 1000 }, values: { bits: 1000, message: 'GG!' } },
  { type: 'twitch.cheer', config: { minBits: 1 }, values: { bits: 500 } },
  { type: 'twitch.raid', config: { minViewers: 100 }, values: { viewers: 100 } },
  { type: 'twitch.reward', config: { reward: 'r' }, values: { input: 'lofi beats', rewardId: 'r', reward: 'VIP', cost: 500 } },
  { type: 'twitch.reward', config: { reward: 'r', title: 'Highlight', cost: 250 }, values: { rewardId: 'r', reward: 'Highlight', cost: 250 } },
  { type: 'twitch.unban_request', config: {}, values: { requestText: 'please unban me' } },
  { type: 'twitch.stream.online', config: {}, values: { live: true, title: 'A cozy stream', category: 'Just Chatting' } },
  { type: 'twitch.stream.offline', config: {}, values: { live: false } },
  { type: 'twitch.channel_update', config: { category: 'Chess' }, values: { title: 'A cozy stream', category: 'Chess' } },
  { type: 'twitch.ad_break', config: { startedBy: 'automatic' }, values: { seconds: 90, automatic: true } },
  { type: 'schedule.timer', config: { every: 900 }, values: { live: true } },
  { type: 'schedule.manual', config: {}, values: { args: '', live: true } },
  { type: 'countdown.finished', config: { name: 'timer' }, values: { name: 'timer', live: true } }
])('default scenario for $type $config', ({ type, config, values }) => {
  const def = lowered(type, config);
  const scenario = defaultScenario(def);
  const fields = SCENARIO_FIELDS[scenarioKind(def)];
  expect({ scenario, match: matchTrigger(def, scenario), live: fields?.find((field) => field.key === 'live'), role: fields?.some((field) => field.key === 'role') }).toMatchObject({
    scenario: values, match: { starts: true }, live: { key: 'live', kind: 'toggle', default: values.live ?? true }, role: type.startsWith('twitch.chat.')
  });
});

type InputCase = { kind: Text; definition?: Pick<Definition, 'actions' | 'conditions'>; keys: Text[] };
const commandKeys = ['user', 'userId', 'role', 'message', 'live'];
test.each<InputCase>([
  { kind: 'twitch.chat.command', keys: ['user', 'userId', 'role', 'message', 'live'] },
  { kind: 'twitch.sub.new', keys: ['user', 'userId', 'tier', 'isGift', 'live'] },
  { kind: 'twitch.sub.resub', keys: ['user', 'userId', 'tier', 'months', 'streak', 'message', 'live'] },
  { kind: 'twitch.sub.gift', keys: ['user', 'userId', 'tier', 'giftCount', 'total', 'anonymous', 'live'] },
  { kind: 'twitch.cheer', keys: ['user', 'userId', 'bits', 'message', 'anonymous', 'live'] },
  { kind: 'twitch.reward', keys: ['user', 'userId', 'input', 'reward', 'cost', 'live'] },
  { kind: 'twitch.stream.online', keys: ['title', 'category', 'live'] },
  { kind: 'twitch.channel_update', keys: ['title', 'category', 'live'] },
  { kind: 'twitch.ad_break', keys: ['seconds', 'automatic', 'live'] },
  { kind: 'schedule.timer', keys: ['live'] },
  { kind: 'schedule.manual', keys: ['args', 'live'] },
  {
    kind: 'twitch.chat.command', definition: { actions: [{ type: 'chat', params: { text: '{points}' } }] },
    keys: commandKeys
  },
  {
    kind: 'twitch.chat.command', definition: { actions: [{ type: 'points_set', params: { amount: 10 } }] },
    keys: [...commandKeys, 'state:points']
  },
  {
    kind: 'twitch.chat.command', definition: { conditions: [{ type: 'points', params: { op: 'gte', value: 10 } }] },
    keys: [...commandKeys, 'state:points']
  },
  {
    kind: 'twitch.chat.command',
    definition: { actions: [{ type: 'if', params: { condition: { type: 'counter', params: { name: 'Wins' } }, then: [
      { type: 'counter_add', params: { name: 'wins', amount: 1 } }, { type: 'counter_set', params: { name: 'score', value: 4 } },
      { type: 'chat', params: { text: '{counter:WINS} {counter:Losses|0}' } }
    ] } }] }, keys: [...commandKeys, 'state:counters:wins', 'state:counters:score', 'state:counters:losses']
  },
  {
    kind: 'twitch.chat.command',
    definition: { conditions: [{ type: 'cooldown', params: { name: 'break' } }], actions: [
      { type: 'countdown_start', params: { name: 'unseeded' } }, { type: 'countdown_add', params: { name: 'subathon' } },
      { type: 'countdown_stop', params: { name: 'clock' } }, { type: 'cooldown_reset', params: { name: 'rest' } },
      { type: 'points_spend', params: { amount: 100 } }
    ] }, keys: [...commandKeys, 'state:points', 'state:countdowns:subathon', 'state:countdowns:subathon:left', 'state:countdowns:clock', 'state:countdowns:clock:left',
      'state:cooldowns:break', 'state:cooldowns:break:left', 'state:cooldowns:rest', 'state:cooldowns:rest:left']
  }
])('scenario inputs for $kind $definition are in grid order', ({ kind, definition, keys }) => {
  const state = scenarioStateFields({ version: 1, trigger: { type: 'command' }, ...definition });
  expect([...(SCENARIO_FIELDS[kind] ?? []), ...state].map((field) => field.key)).toEqual(keys);
});

test.each(['twitch.stream.offline', 'twitch.stream.online', 'countdown.finished'])('changing trigger removes unavailable patches for %s', (kind) => {
  const patch = { user: 'Old', userId: '42', role: 'mod' as const, message: '!old', bits: 50, state: { points: 5 } };
  const next = scenarioPatches(SCENARIO_FIELDS[kind]!, patch);
  const returned = scenarioPatches(SCENARIO_FIELDS['twitch.chat.command']!, next);
  expect([next, returned]).toEqual([{ state: { points: 5 } }, { state: { points: 5 } }]);
});

type IdentityCase = { trigger: Trigger; scenario?: Partial<RehearsalScenario>; expected: string[]; level?: string };
test.each<IdentityCase>([
  { trigger: { type: 'command', params: { name: '!hello' } }, scenario: { role: 'mod' }, level: 'mod', expected: ['PrincessBarney/40000001/'] },
  { trigger: { type: 'event', params: { kind: 'channel.follow' } }, scenario: { role: 'mod' }, level: 'mod', expected: [] },
  { trigger: { type: 'reward', params: { reward_id: 'r' } }, scenario: { role: 'mod' }, level: 'mod', expected: [] },
  { trigger: { type: 'manual' }, scenario: { role: 'viewer' }, level: 'broadcaster', expected: ['/40000000/'] },
  { trigger: { type: 'timer' }, scenario: { role: 'viewer' }, level: 'broadcaster', expected: ['/40000000/'] },
  { trigger: { type: 'countdown', params: { name: 'timer' } }, scenario: { role: 'viewer', userId: '7' }, level: 'broadcaster', expected: ['/40000000/timer'] },
  { trigger: { type: 'event', params: { kind: 'channel.cheer' } }, scenario: { anonymous: true }, expected: ['//'] },
  { trigger: { type: 'event', params: { kind: 'channel.subscription.gift' } }, scenario: { anonymous: true }, expected: ['//'] },
  { trigger: { type: 'event', params: { kind: 'channel.subscription.gift' } }, scenario: { anonymous: false }, expected: ['PrincessBarney/40000001/'] },
  { trigger: { type: 'event', params: { kind: 'channel.cheer' } }, scenario: { anonymous: false }, expected: ['PrincessBarney/40000001/'] }
])('trigger identity $trigger $scenario', ({ trigger, scenario, expected, level = 'everyone' }) => {
  const def: Definition = {
    version: 1, trigger, conditions: [{ type: 'perm', params: { level } }],
    actions: [{ type: 'chat', params: { text: '{user}/{userid}/{event.name}' } }]
  };
  const result = rehearseDefinition(def, { ...defaultScenario(def), ...scenario });
  expect(result.events.flatMap((event) => event.lines ?? [])).toEqual(expected);
});

type MatchCase = { trigger: Trigger; scenario: Partial<RehearsalScenario>; reason: Text };
test.each<MatchCase>([
  { trigger: { type: 'command', params: { name: '!hello' } }, scenario: { message: '!other' }, reason: 'trigger_message' },
  { trigger: { type: 'command', params: { name: '' } }, scenario: { message: '' }, reason: 'no_name' },
  { trigger: { type: 'command', params: { name: '', match: 'regex' } }, scenario: { message: 'anything' }, reason: 'no_name' },
  { trigger: { type: 'command', params: { name: '(?P<word>bagel)', match: 'regex' } }, scenario: { message: 'bagel' }, reason: 'trigger_message' },
  { trigger: { type: 'reward', params: { reward_id: 'r' } }, scenario: { rewardId: 'other' }, reason: 'trigger_filter' },
  { trigger: { type: 'countdown', params: { name: 'timer' } }, scenario: { name: 'other' }, reason: 'trigger_filter' }
])('failed trigger $trigger', ({ trigger, scenario, reason }) => {
  const def: Definition = { version: 1, trigger, actions: [{ type: 'chat', params: { text: 'never' } }] };
  const result = rehearseDefinition(def, { ...defaultScenario(def), ...scenario }, {});
  expect([result.starts, result.reason?.reason, result.events]).toEqual([false, reason, []]);
});

type FilterCase = {
  kind: Text;
  field: Text;
  expected: Text;
  passes: Partial<RehearsalScenario>;
  fails: Partial<RehearsalScenario>;
};

test.each<FilterCase>([
  { kind: 'channel.subscribe', field: 'is_gift', expected: 'FALSE', passes: { isGift: false }, fails: { isGift: true } },
  { kind: 'channel.subscribe', field: 'tier', expected: '1000', passes: { tier: '1000' }, fails: { tier: '2000' } },
  { kind: 'channel.subscription.message', field: 'months', expected: '12', passes: { months: 14 }, fails: { months: 11 } },
  { kind: 'channel.subscription.message', field: 'streak', expected: '4', passes: { streak: 4 }, fails: { streak: 3 } },
  { kind: 'channel.subscription.message', field: 'message', expected: 'welcome', passes: { message: 'WELCOME' }, fails: { message: 'welcome back' } },
  { kind: 'channel.subscription.gift', field: 'amount', expected: '5', passes: { giftCount: 5 }, fails: { giftCount: 4 } },
  { kind: 'channel.subscription.gift', field: 'total', expected: '20', passes: { total: 25 }, fails: { total: 19 } },
  { kind: 'channel.cheer', field: 'amount', expected: '1000', passes: { bits: 1000 }, fails: { bits: 1 } },
  { kind: 'channel.raid', field: 'viewers', expected: '100', passes: { viewers: 120 }, fails: { viewers: 42 } },
  { kind: 'channel.channel_points_custom_reward_redemption.add', field: 'cost', expected: '500', passes: { cost: 750 }, fails: { cost: 499 } },
  { kind: 'channel.update', field: 'category', expected: 'Chess', passes: { category: 'chess' }, fails: { category: 'Music' } },
  { kind: 'channel.ad_break.begin', field: 'seconds', expected: '90', passes: { seconds: 120 }, fails: { seconds: 89 } },
  { kind: 'channel.ad_break.begin', field: 'seconds', expected: '60', passes: { seconds: 60 }, fails: { seconds: 60.5 } },
  { kind: 'channel.ad_break.begin', field: 'automatic', expected: 'true', passes: { automatic: true }, fails: { automatic: false } }
])('$kind applies its $field filter', ({ kind, field, expected, passes, fails }) => {
  const def: Definition = { version: 1, trigger: { type: 'event', params: { kind, filters: { [field]: expected } } } };
  expect([matchTrigger(def, { live: true, ...passes }), matchTrigger(def, { live: true, ...fails })]).toMatchObject([
    { starts: true }, { starts: false, reason: 'trigger_filter', values: { field, expected } }
  ]);
});

type TokenCase = { trigger: Trigger; scenario: Partial<RehearsalScenario>; text: Text; expected: Text };

test.each<TokenCase>([
  { trigger: { type: 'event', params: { kind: 'channel.subscription.gift' } }, scenario: { total: 20, giftCount: 6 },
    text: '{event.total}/{total}/{event.amount}/{count}', expected: '20/20/6/6' },
  { trigger: { type: 'event', params: { kind: 'channel.subscription.message' } }, scenario: { streak: 4, message: 'Hello again!' },
    text: '{event.streak}/{streak}/{event.message}/{message}', expected: '4/4/Hello again!/Hello again!' },
  { trigger: { type: 'event', params: { kind: 'channel.cheer' } }, scenario: { message: 'Nice!' },
    text: '{event.message}/{message}', expected: 'Nice!/Nice!' },
  { trigger: { type: 'command', params: { name: '!hello' } }, scenario: { message: '!hello hi' },
    text: '{args}/{message}/{event.message}', expected: 'hi//' },
  { trigger: { type: 'reward', params: { reward_id: 'r' } }, scenario: { reward: 'Highlight', cost: 100 },
    text: '{event.reward}/{reward}/{event.cost}/{cost}', expected: 'Highlight/Highlight/100/100' },
  ...['stream.online', 'channel.update'].map((kind): TokenCase => ({
    trigger: { type: 'event', params: { kind } }, scenario: { title: 'Speedrun', category: 'Minecraft' },
    text: '{event.title}/{title}/{event.category}/{game}', expected: 'Speedrun/Speedrun/Minecraft/Minecraft'
  })),
  { trigger: { type: 'event', params: { kind: 'channel.ad_break.begin' } }, scenario: { seconds: 120, automatic: false },
    text: '{event.seconds}/{seconds}/{event.automatic}', expected: '120/120/false' },
  { trigger: { type: 'manual' }, scenario: { args: 'one two', live: false }, text: '{args}/{2}', expected: 'one two/two' }
])('rehearsal expands scenario tokens for $trigger', ({ trigger, scenario, text, expected }) => {
  const def: Definition = { version: 1, trigger, actions: [{ type: 'chat', params: { text } }] };
  const result = rehearseDefinition(def, { ...defaultScenario(def), ...scenario });
  expect(result.events.flatMap((event) => event.lines ?? [])).toEqual([expected]);
});

test.each([
  { live: true, offlineOK: false, starts: true },
  { live: false, offlineOK: false, starts: false },
  { live: false, offlineOK: true, starts: true }
])('timer with live=$live and offline_ok=$offlineOK has starts=$starts', ({ live, offlineOK, starts }) => {
  const def: Definition = { version: 1, trigger: { type: 'timer', params: { offline_ok: offlineOK } } };
  expect(matchTrigger(def, { live }).starts).toBe(starts);
});
