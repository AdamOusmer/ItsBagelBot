// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import mathCases from './testdata/math_cases.json';
import textCases from './testdata/text_cases.json';
import { compileFlow, flowToDefinition, type Flow, type FlowNode } from './flow-graph';
import { compactDuration, defaultScenario, rehearseDefinition } from './rehearsal';
import {
  MAX_ACTIONS,
  MAX_AGE_DAYS,
  MAX_DEFINITION_BYTES,
  MAX_LIGHTS_BRIGHTNESS,
  MAX_LIGHTS_HOLD_S,
  MAX_LIGHTS_KELVIN,
  MAX_LOOP_TIMES,
  MAX_MATH_EXPRESSION_RUNES,
  MIN_LIGHTS_BRIGHTNESS,
  MIN_LIGHTS_KELVIN,
} from './limits';
import type { Action, Condition, Definition, Params, Trigger } from './types';
import type { RehearsalEvent, RehearsalOverrides, RehearsalScenario } from './rehearsal-types';

type Text = string;
const action = (type: Text, params: Params = {}): Action => ({ type, params });
const chat = (text: Text): Action => action('chat', { text });
const condition = (type: Text, params: Params): Condition => ({ type, params });
const choose = (condition: Condition): Action => action('if', { condition, then: [chat('yes')], else: [chat('no')] });
const definition = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'command', params: { name: '!hello' } }, actions });
const parallel = (branches: Action[][]): Action => action('parallel', { branches: branches.map((actions) => ({ weight: 1, actions })) });
const apiUrl = 'https://example.com';
const spend = action('points_spend', { amount: 100, on_ok: [chat('Enough: {points}')], on_failure: [chat('Not enough: {points}')] });

type Options = { conditions?: Condition[]; scenario?: Partial<RehearsalScenario>; overrides?: RehearsalOverrides; trigger?: Trigger };
function execute(actions: Action[], options: Options = {}) {
  const def = { ...definition(actions), conditions: options.conditions };
  if (options.trigger) def.trigger = options.trigger;
  return rehearseDefinition(def, { ...defaultScenario(def), ...options.scenario }, options.overrides);
}

type RunCase = { name: Text; actions: Action[]; options?: Options; lines: Text[]; times?: number[]; effects?: Pick<RehearsalEvent, 'status' | 'detail' | 'port'>[] };
const runs: RunCase[] = [
  { name: 'parallel pause starts the sibling and continuation immediately', actions: [parallel([[action('delay', { ms: 5000 }), chat('A')], [chat('B')]]), chat('join')], lines: ['B', 'join', 'A'], times: [0, 0, 5000] },
  { name: 'resumed branches update shared state in time order', actions: [parallel([[action('delay', { ms: 5000 }), chat('{var:score}')], [action('var_set', { scope: 'channel', name: 'score', value: '9' })]])], lines: ['9'] },
  { name: 'later branches cannot observe future state', actions: [parallel([[action('delay', { ms: 5000 }), action('var_set', { scope: 'channel', name: 'score', value: '9' })], [chat('{var:score}')]])], lines: [] },
  { name: 'loop executes three times', actions: [action('loop', { times: '3', actions: [chat('{local:index}')] })], lines: ['1', '2', '3'] },
  { name: 'loop delay carries the remaining body and iterations', actions: [action('loop', { times: '3', actions: [action('delay', { ms: 5000 }), chat('{local:index}')] }), chat('done')], lines: ['1', '2', '3', 'done'], times: [5000, 10000, 15000, 15000] },
  { name: 'nested loop restores index', actions: [action('loop', { times: '2', actions: [action('loop', { times: '1', actions: [chat('inner {local:index}')] }), chat('outer {local:index}')] }), chat('{local:index}')], lines: ['inner 1', 'outer 1', 'inner 1', 'outer 2'] },
  { name: 'zero loop goes straight to done', actions: [action('loop', { times: '0', actions: [chat('never')] }), chat('done')], lines: ['done'] },
  { name: 'parallel locals are cloned and restored', actions: [action('math', { expression: '1', save_as: 'x' }), parallel([[action('math', { expression: '2', save_as: 'x' }), chat('{local:x}')], [chat('{local:x}')]]), chat('{local:x}')], lines: ['2', '1', '1'] },
  { name: 'http failure followed by success clears only error', actions: [action('http_get', { url: apiUrl, save_as: 'result', on_failure: [chat('{local:error}')] }), action('http_get', { url: apiUrl, save_as: 'result', on_ok: [chat('{local:result} {local:error}')] })], options: { overrides: { 'actions[0]': 'failure', 'actions[1].response': 'hello' } }, lines: ['upstream_error', 'hello '] },
  { name: 'http success cannot leak failure-only values into siblings', actions: [parallel([[action('http_get', { url: apiUrl, on_failure: [chat('{local:error}')] })], [chat('{local:error}')]])], options: { overrides: { 'actions[0].params.branches[0].actions[0]': 'failure' } }, lines: ['upstream_error'] },
  { name: 'bad JSON takes failure only', actions: [action('http_get', { url: apiUrl, json_path: ['value'], on_ok: [chat('never')], on_failure: [chat('{local:error}')] })], options: { overrides: { 'actions[0].response': 'not JSON' } }, lines: ['bad_def'] },
  { name: 'math saves a computed local', actions: [action('math', { expression: '2.5 * 4 + max(1, 8)', save_as: 'answer' }), chat('{local:answer}')], lines: ['18'] },
  { name: 'actions store an empty expansion for missing values', actions: [action('var_set', { scope: 'channel', name: 'score', value: '{local:absent}' }), chat('value={var:score}.')], lines: ['value=.'] },
  { name: 'disabled chat is skipped', actions: [{ ...chat('never'), enabled: false }, chat('enabled')], lines: ['enabled'] },
  { name: 'cooldown loop sees its earlier claim', actions: [action('loop', { times: '3', actions: [choose(condition('cooldown', { name: 'test', global_s: 30 }))] })], lines: ['yes', 'no', 'no'] },
  { name: 'ready override replaces seed but cannot bypass a claim in this run', actions: [action('loop', { times: '3', actions: [choose(condition('cooldown', { name: 'test', global_s: 30 }))] })], options: { scenario: { state: { cooldowns: { test: 30000 } } }, overrides: { 'actions[0].params.actions[0]': 'ready' } }, lines: ['yes', 'no', 'no'] },
  { name: 'cooling override refuses an otherwise ready check', actions: [choose(condition('cooldown', { name: 'test', global_s: 30 }))], options: { overrides: { 'actions[0]': 'cooling' } }, lines: ['no'] },
  { name: 'cooldown expires on the logical clock', actions: [choose(condition('cooldown', { name: 'test', global_s: 1 })), action('delay', { ms: 1000 }), choose(condition('cooldown', { name: 'test', global_s: 1 }))], lines: ['yes', 'yes'] },
  { name: 'multiline chat keeps line boundaries', actions: [chat('Hi {user}!\nOne\r\nTwo\rThree')], lines: ['Hi PrincessBarney!', 'One\r', 'Two\rThree'] },
  { name: 'command args derive from the edited message', actions: [chat('{args}')], options: { scenario: { message: '!hello changed words', args: 'stale' } }, lines: ['changed words'] },
  { name: 'stored variables mutate independently by scope', actions: [action('var_set', { scope: 'channel', name: 'score', value: '2' }), action('var_add', { scope: 'channel', name: 'score', amount: 3 }), action('var_set', { scope: 'viewer', name: 'score', value: '8' }), chat('{var:score}/{viewervar:score}'), action('var_delete', { scope: 'channel', name: 'score' }), chat('{var:score}/{viewervar:score}')], lines: ['5/8', '/8'] },
  { name: 'counters and points reflect seeded state', actions: [action('counter_add', { name: 'wins', amount: 2 }), action('points_add', { amount: 5 }), chat('{counter:wins}/{points}')], options: { scenario: { state: { counters: { wins: 3 }, points: 10 } } }, lines: ['5/15'] },
  { name: 'stored template text is never expanded twice', actions: [chat('{var:literal}')], options: { scenario: { state: { channelVars: { literal: '{user}' } } } }, lines: ['{user}'] },
  { name: 'variable tokens ignore inherited names', actions: [chat('{var:constructor}/{viewervar:constructor}')], lines: ['/'] },
  { name: 'countdown starts and extends with time left', actions: [action('countdown_start', { name: 'timer', seconds: 60 }), action('delay', { ms: 5000 }), action('countdown_add', { name: 'timer', seconds: 10 }), chat('{countdown:left}')], lines: ['1m 5s'] },
  { name: 'named cooldown extends only a running window', actions: [choose(condition('cooldown', { name: 'timer', global_s: 60 })), action('cooldown_add', { name: 'timer', seconds: 30 }), chat('{cooldown:left}')], lines: ['yes', '1m 30s'] },
  { name: 'cooldown add cannot extend an unnamed window', actions: [choose(condition('cooldown', { global_s: 60 })), action('cooldown_add', { name: 'global', seconds: 30 }), chat('{cooldown:left}')], lines: ['yes'] },
  { name: 'cooling branch reports current time left', actions: [choose(condition('cooldown', { name: 'timer', global_s: 60 })), action('delay', { ms: 5000 }), action('if', { condition: condition('cooldown', { name: 'timer', global_s: 60 }), else: [chat('{cooldown:left}')] })], lines: ['yes', '55s'] },
  {
    name: 'taking points floors the viewer balance at zero',
    actions: [action('points_add', { amount: -10 }), chat('{points}')],
    options: { scenario: { state: { points: 3 } } }, lines: ['0'],
    effects: [{ status: 'ok', detail: { value: '0' } }]
  },
  {
    name: 'targeting another viewer preserves the seed and reports each target',
    actions: [action('points_add', { amount: 5, who: 'user', user_id: '42' }),
      action('points_set', { amount: 50, who: 'login', login: '@Ana' }), action('points_add', { amount: 5, who: 'mention' }), chat('{points}')],
    options: { scenario: { state: { points: 3 } } }, lines: ['3'],
    effects: [{ status: 'ok', detail: { target: '42' } }, { status: 'ok', detail: { target: 'Ana' } }, { status: 'ok', detail: { target: 'Lotti' } }]
  },
  {
    name: 'setting points through the viewer login replaces the balance',
    actions: [action('points_set', { amount: 50, login: '@princessbarney' }), chat('{points}')],
    options: { scenario: { state: { points: 3 } } }, lines: ['50'],
    effects: [{ status: 'ok', detail: { value: '50' } }]
  },
  {
    name: 'setting points uses the allowed bounds',
    actions: [action('points_set', { amount: -1, who: 'viewer' }), chat('{points}'),
      action('points_set', { amount: 1_000_000_001, who: 'viewer' }), chat('{points}')],
    lines: ['0', '1000000000']
  },
  {
    name: 'counter writes, seeds and token lookups use the same name',
    actions: [action('counter_add', { name: 'Deaths', amount: 1 }), chat('{counter:DEATHS}'),
      action('counter_set', { name: 'Deaths', value: 0 }), chat('{counter:deaths}/{counter:WINS}')],
    options: { scenario: { state: { counters: { deaths: 3, Wins: 8 } } } }, lines: ['4', '0/8'],
    effects: [{ status: 'ok', detail: { value: '4' } }, { status: 'ok', detail: { value: '0' } }]
  },
  {
    name: 'redemption steps report the status for the reward trigger',
    actions: [action('redemption', { status: 'fulfill' }), action('redemption', { status: 'cancel' })],
    options: { trigger: { type: 'reward', params: { reward_id: 'hydrate' } } }, lines: [],
    effects: [{ status: 'ok', detail: { value: 'fulfill' } }, { status: 'ok', detail: { value: 'cancel' } }]
  },
  {
    name: 'a command cannot settle a redemption', actions: [action('redemption', { status: 'cancel' })],
    lines: [], effects: [{ status: 'failed', detail: { code: 'no_redemption' } }]
  },
  {
    name: 'reward updates report changes and reject reset without a change',
    actions: [action('reward_update', { reward_id: 'hydrate', enabled: false, cost: 500, reset_on_stream_end: true }),
      action('reward_update', { reward_id: 'hydrate', enabled: null, cost: null, reset_on_stream_end: true })],
    lines: [], effects: [{ status: 'ok', detail: { enabled: 'false', cost: '500', reset_on_stream_end: 'true' } },
      { status: 'failed', detail: { code: 'nothing_to_change' } }]
  },
  {
    name: 'points and counter conditions compare the seeded values',
    actions: [choose(condition('points', { op: 'eq', value: 10 })), choose(condition('points', { op: 'ne', value: 10 })),
      choose(condition('points', { op: 'gt', value: 9 })), choose(condition('counter', { name: 'WINS', op: 'gte', value: 4 })),
      choose(condition('counter', { name: 'wins', op: 'lt', value: 4 })), choose(condition('counter', { name: 'Wins', op: 'lte', value: 4 }))],
    options: { scenario: { state: { points: 10, counters: { wins: 4 } } } }, lines: ['yes', 'no', 'yes', 'yes', 'no', 'yes']
  },
  {
    name: 'timer adds check the rate before looking for a running timer',
    actions: [action('countdown_add', { name: 'timer', seconds: 10, per: 100 }), action('cooldown_add', { name: 'timer', seconds: 10, per: 100 })],
    options: { trigger: { type: 'event', params: { kind: 'channel.cheer' } }, scenario: { bits: 5 } },
    lines: [], effects: [{ status: 'skipped', detail: { code: 'below_rate' } }, { status: 'skipped', detail: { code: 'below_rate' } }]
  },
  {
    name: 'spend points deducts the amount before taking Enough', actions: [spend],
    options: { scenario: { state: { points: 250 } } }, lines: ['Enough: 150'],
    effects: [{ status: 'ok', port: 'enough', detail: { value: '150' } }]
  },
  {
    name: 'spend points can spend the entire balance', actions: [spend],
    options: { scenario: { state: { points: 100 } } }, lines: ['Enough: 0'],
    effects: [{ status: 'ok', port: 'enough', detail: { value: '0' } }]
  },
  {
    name: 'spend points preserves the balance on Not enough', actions: [spend],
    options: { scenario: { state: { points: 75 } } }, lines: ['Not enough: 75'],
    effects: [{ status: 'failed', port: 'notEnough', detail: { code: 'insufficient_points', value: '75' } }]
  },
  {
    name: 'spend points fails before branching when no viewer is present', actions: [spend, chat('Balance: {points}')],
    options: { scenario: { userId: '', state: { points: 250 } } }, lines: ['Balance: 250'],
    effects: [{ status: 'failed', detail: { code: 'no_viewer' } }]
  },
  {
    name: 'reward changes include pause, resume and zero cooldown',
    actions: [action('reward_update', { reward_id: 'hydrate', paused: true }),
      action('reward_update', { reward_id: 'hydrate', paused: false, cooldown_s: 300 }),
      action('reward_update', { reward_id: 'hydrate', cooldown_s: 0 })],
    lines: [], effects: [{ status: 'ok', detail: { paused: 'true' } },
      { status: 'ok', detail: { paused: 'false', cooldown_s: '300' } }, { status: 'ok', detail: { cooldown_s: '0' } }]
  },
  {
    name: 'stop countdown removes a running timer and fails when repeated',
    actions: [action('countdown_stop', { name: 'subathon' }), action('countdown_stop', { name: 'subathon' })],
    options: { scenario: { state: { countdowns: { subathon: 60_000 } } } }, lines: [],
    effects: [{ status: 'ok', detail: { value: 'subathon' } }, { status: 'failed', detail: { code: 'not_running' } }]
  },
  {
    name: 'reset cooldown clears the seeded window and succeeds when repeated',
    actions: [choose(condition('cooldown', { name: 'raffle', global_s: 60 })),
      action('cooldown_reset', { name: 'raffle' }), action('cooldown_reset', { name: 'raffle' }),
      choose(condition('cooldown', { name: 'raffle', global_s: 60 })), choose(condition('cooldown', { user_s: 60 }))],
    options: { scenario: { state: { cooldowns: { raffle: 60_000, 'viewer:40000001': 60_000 } } } }, lines: ['no', 'yes', 'no'],
    effects: [{ status: 'ok', detail: { value: 'raffle' } }, { status: 'ok', detail: { value: 'raffle' } }]
  },
  {
    name: 'reset preserves unnamed cooldowns with names that could collide',
    actions: [choose(condition('cooldown', { global_s: 60, user_s: 60 })),
      action('cooldown_reset', { name: 'global' }), action('cooldown_reset', { name: 'viewer' }),
      choose(condition('cooldown', { global_s: 60 })), choose(condition('cooldown', { user_s: 60 }))],
    lines: ['yes', 'no', 'no']
  }
];

test.each(runs)('$name', ({ actions, options, lines, times, effects }) => {
  const result = execute(actions, options);
  const said = result.events.filter((event) => event.kind === 'say');
  const rendered = { lines: said.flatMap((event) => event.lines ?? []), times: said.map((event) => event.atMs) };
  expect(rendered).toMatchObject({ lines, ...(times ? { times } : {}) });
  if (effects) expect(result.events.filter((event) => event.kind === 'effect')).toMatchObject(effects);
  expect(new Set(result.events.map((event) => event.key)).size).toBe(result.events.length);
});

const stoppedPath = [chat('before'), { type: 'stop' }, chat('unreachable')];
test.each([
  { name: 'whole run', actions: stoppedPath, lane: [], lines: ['before'] },
  { name: 'parallel lane', actions: [parallel([stoppedPath, [chat('sibling')]]), chat('after')], lane: [0], lines: ['before', 'sibling', 'after'] }
])('stop ends only its $name', ({ actions, lane, lines }) => {
  const result = execute(actions);
  const skips = result.events.filter(({ kind }) => kind === 'skip').map(({ status, detail, lane }) => ({ status, code: detail?.code, lane }));
  expect({ skips, lines: result.events.flatMap((event) => event.lines ?? []) })
    .toEqual({ skips: [{ status: 'skipped', code: 'stopped', lane }], lines });
});

test.each([
  { override: undefined, path: '1', text: 'first' },
  { override: '2', path: '2', text: 'second' },
  { override: '5', path: '1', text: 'first' }
])('random pick samples $path with override $override', ({ override, path, text }) => {
  const branches = ['first', 'second'].map((label, i) => ({ weight: i + 1, actions: [chat(label)] }));
  const overrides = override ? { 'actions[0].path': override } : undefined;
  const result = execute([action('random_pick', { branches }), chat('after')], { overrides });
  const row = result.events[1];
  expect({ port: row?.port, detail: row?.detail, lines: result.events.flatMap((event) => event.lines ?? []) })
    .toEqual({ port: `p${path}`, detail: { path, random: '1' }, lines: [text, 'after'] });
});

test.each([
  { role: 'viewer', level: 'sub', expected: 'no' },
  { role: 'sub', level: 'sub', expected: 'yes' },
  { role: 'sub', level: 'everyone', expected: 'yes' },
  { role: 'vip', level: 'mod', expected: 'no' },
  { role: 'lead_mod', level: 'mod', expected: 'yes' },
  { role: 'mod', level: 'lead_mod', expected: 'no' },
  { role: 'broadcaster', level: 'lead_mod', expected: 'yes' }
] as const)('role $role requires $level', ({ role, level, expected }) => {
  const result = execute([choose(condition('perm', { level }))], { scenario: { role } });
  expect(result.events.find((event) => event.kind === 'say')?.lines).toEqual([expected]);
});

type ConditionCase = Options & { name?: Text; expected: Text | Text[]; code?: Text; starts?: boolean; blockedAt?: Text } & (
  { type: Text; params: Params } | { actions?: Action[] }
);
const unnamedCooldown = (seconds: number) => condition('cooldown', { global_s: seconds });
const conditionCases: ConditionCase[] = [
  { type: 'users', params: { allow_ids: ['40000001'] }, expected: 'yes' },
  { type: 'users', params: { allow_ids: ['40000002'] }, expected: 'no' },
  { type: 'users', params: { deny_ids: ['40000001'] }, expected: 'no' },
  { type: 'compare', params: { left: '9', op: 'gte', right: '10' }, expected: 'no' },
  { type: 'compare', params: { left: '{args}', op: 'contains', right: '@Lotti' }, expected: 'yes' },
  { type: 'var', params: { scope: 'channel', name: 'score', op: 'eq', value: '01' }, expected: 'yes' },
  { type: 'var', params: { scope: 'channel', name: 'absent', op: 'exists' }, expected: 'no' },
  { type: 'var', params: { scope: 'channel', name: 'word', op: 'lt', value: 'z' }, expected: 'yes' },
  { type: 'live', params: { state: 'offline' }, expected: 'no' },
  { type: 'chance', params: { percent: 50 }, expected: 'no', overrides: { 'actions[0]': 'no' } },
  { type: 'chance', params: { percent: 100 }, expected: 'yes', overrides: { 'actions[0]': 'no' } },
  { type: 'chance', params: { percent: 150 }, expected: 'yes', overrides: { 'actions[0]': 'no' } },
  { type: 'chance', params: { percent: 0 }, expected: 'no', overrides: { 'actions[0]': 'yes' } },
  { type: 'chance', params: { percent: -1 }, expected: 'no', overrides: { 'actions[0]': 'yes' } },
  { type: 'compare', params: { left: '{local:missing}', op: 'eq', right: '' }, expected: 'yes' },
  { type: 'users', params: { allow: ['alice'] }, scenario: { user: 'Alice' }, expected: 'yes' },
  { type: 'users', params: { allow: ['alice'] }, scenario: { user: 'Bob' }, expected: 'no' },
  { type: 'users', params: { deny: ['alice'], allow_ids: ['42'] }, scenario: { user: 'Alice', userId: '42' }, expected: 'no' },
  { type: 'users', params: { allow_ids: ['00042'] }, scenario: { userId: '42' }, expected: 'yes' },
  { type: 'users', params: { allow_ids: ['000'] }, scenario: { userId: '0' }, expected: 'no' },
  { type: 'users', params: { allow_ids: ['0x2a'] }, scenario: { userId: '0x2a' }, expected: 'no' },
  { type: 'users', params: { allow: ['_setup_required'] }, scenario: { user: '_setup_required' }, expected: 'no', code: 'setup_required' },
  { type: 'var', params: { name: 'constructor', op: 'exists' }, expected: 'no' },
  { type: 'var', params: { name: 'constructor', op: 'eq', value: '1' }, expected: 'no' },
  { type: 'var', params: { name: 'constructor', op: 'not_exists', value: '{var:constructor}' }, expected: 'yes' },
  { type: 'var', params: { name: 'score', op: 'not_exists' }, expected: 'no' },
  { type: 'var', params: { name: 'score', op: 'starts', value: '1' }, expected: 'yes' },
  { type: 'compare', params: { left: 'HYPE train', op: 'starts', right: 'hype', ignore_case: true }, expected: 'yes' },
  { type: 'compare', params: { left: 'HYPE train', op: 'starts', right: 'hype' }, expected: 'no' },
  { name: 'root chance refuses before cooldown is armed', conditions: [unnamedCooldown(30), condition('chance', { percent: 50 })], overrides: { 'conditions[1]': 'no' }, expected: ['no'], starts: false, blockedAt: 'conditions[1]' },
  { name: 'named global and unnamed cooldowns have separate windows', conditions: [condition('cooldown', { name: 'global', global_s: 30 }), unnamedCooldown(5)], expected: ['yes', 'yes'] },
  { name: 'legacy named seed still applies', type: 'cooldown', params: { name: 'test', global_s: 30 }, scenario: { state: { cooldowns: { test: 30000 } } }, expected: 'no' },
  { name: 'global seed covers only the unnamed window', type: 'cooldown', params: { name: 'global', global_s: 30 }, scenario: { state: { cooldowns: { global: 30000 } } }, expected: 'yes' },
  { name: 'namespaced seed can target a cooldown named global', type: 'cooldown', params: { name: 'global', global_s: 30 }, scenario: { state: { cooldowns: { 'named:global': 30000 } } }, expected: 'no' },
  { name: 'two unnamed cooldowns', actions: [action('loop', { times: '2', actions: [choose(unnamedCooldown(30)), choose(unnamedCooldown(5))] })], expected: ['yes', 'yes', 'no', 'no'] },
  { name: 'ordinals include unvisited branches and follow root and parent checks', conditions: [unnamedCooldown(30)], actions: [action('if', { condition: unnamedCooldown(5), then: [choose(unnamedCooldown(5))], else: [choose(unnamedCooldown(5))] }), choose(unnamedCooldown(5))], scenario: { state: { cooldowns: { 'global:2': 30000, 'global:4': 30000 } } }, expected: ['yes', 'yes', 'no', 'no'] },
  { type: 'arg_regex', params: { pattern: '^[0-9]+$' }, scenario: { message: '!hello 42' }, expected: 'yes' },
  { type: 'arg_regex', params: { pattern: '^[0-9]+$' }, scenario: { message: '!hello x' }, expected: 'no' },
  { type: 'arg_regex', params: { pattern: '(?i)^(?P<name>alice)$' }, scenario: { message: '!hello Alice' }, expected: 'yes' },
  { type: 'arg_regex', params: { pattern: '(?=42)' }, scenario: { message: '!hello 42' }, expected: 'no' },
  { type: 'arg_regex', params: { pattern: '[' }, expected: 'no' },
  { type: 'category', params: { allow: ['minecraft'] }, scenario: { game: 'Minecraft' }, expected: 'yes' },
  { type: 'category', params: { deny: ['minecraft'], allow: ['Minecraft'] }, scenario: { game: 'Minecraft' }, expected: 'no' },
  { type: 'category', params: { allow: ['minecraft'] }, expected: 'yes' },
  { type: 'category', params: { allow: ['minecraft'] }, overrides: { 'actions[0]': 'no' }, expected: 'no' },
  { type: 'follow', params: { min_days: 0 }, expected: 'yes' },
  { type: 'follow', params: { min_days: 30 }, overrides: { 'actions[0]': 'no' }, expected: 'no' },
  { type: 'follow', params: { min_days: 0 }, scenario: { userId: undefined }, overrides: { 'actions[0]': 'yes' }, expected: 'no' },
  { type: 'account_age', params: { min_days: 7 }, expected: 'yes' },
  { type: 'account_age', params: { min_days: 7 }, overrides: { 'actions[0]': 'no' }, expected: 'no' },
  { type: 'account_age', params: { min_days: 7 }, scenario: { userId: undefined }, overrides: { 'actions[0]': 'yes' }, expected: 'no' }
];

test.each(conditionCases)('$name $type $params', (row) => {
  const actions = 'type' in row ? [choose(condition(row.type, row.params))] : row.actions ?? [];
  const scenario = { state: { channelVars: { score: '1', word: 'a' } }, ...row.scenario };
  const result = execute(actions, { ...row, scenario });
  const checks = result.events.filter((event) => event.kind === 'check');
  const lines = result.events.find((event) => event.kind === 'say')?.lines;
  const expected = [row.expected].flat();
  expect({ starts: result.starts, states: checks.map((event) => event.status), code: checks[0]?.detail?.code, blockedAt: result.reason?.values.path, lines: 'type' in row ? lines : undefined }).toEqual({
    starts: row.starts ?? true, states: expected, code: row.code, blockedAt: row.blockedAt, lines: 'type' in row ? expected : undefined
  });
});

test.each(mathCases)('shared math $expression', ({ expression, want, error }) => {
  const result = execute([action('math', { expression, save_as: 'answer' }), chat('{local:answer}')]);
  const event = result.events.find((event) => event.kind === 'effect');
  expect([event?.detail?.code ?? '', result.events.at(-1)?.lines]).toEqual([error, want ? [want] : []]);
});

test.each(textCases)('shared text $op $input', ({ want, error, ...params }) => {
  const result = execute([action('text', { ...params, save_as: 'result' }), chat('{local:result}')]);
  const event = result.events.find((event) => event.kind === 'effect');
  expect([event?.detail?.code ?? '', result.events.at(-1)?.lines]).toEqual([error, want ? [want] : []]);
});

const node = (id: Text, type: Text, config: FlowNode['config'] = {}): FlowNode => ({ id, type, x: 0, y: 0, config });
const edge = (from: Text, port: Text, to: Text) => ({ id: `${from}-${to}`, from, port, to });
const flow = (nodes: FlowNode[], edges: Flow['edges']): Flow => ({ id: 'test', name: 'test', enabled: true, nodes, edges });

type CompileCase = { name: Text; graph: Flow; paths: [Text, Text][]; lines: Text[] };
test.each<CompileCase>([
  {
    name: 'diamond join',
    graph: flow([node('t', 'twitch.follow'), node('a', 'reply.chat', { message: 'A' }), node('b', 'reply.chat', { message: 'B' }), node('j', 'reply.chat', { message: 'JOIN' })], [edge('t', 'next', 'a'), edge('t', 'next', 'b'), edge('a', 'next', 'j'), edge('b', 'next', 'j')]),
    paths: [['trigger', 't'], ['actions[0].params.branches[0].actions[0]', 'a'], ['actions[0].params.branches[1].actions[0]', 'b'], ['actions[1]', 'j']],
    lines: ['A', 'B', 'JOIN']
  },
  {
    name: 'leading gate and nested condition',
    graph: flow([node('t', 'twitch.follow'), node('g', 'cond.role', { level: 'everyone' }), node('c', 'cond.chance', { percent: 50 }), node('y', 'reply.chat', { message: 'yes' }), node('n', 'reply.chat', { message: 'no' })], [edge('t', 'next', 'g'), edge('g', 'yes', 'c'), edge('c', 'yes', 'y'), edge('c', 'no', 'n')]),
    paths: [['trigger', 't'], ['conditions[0]', 'g'], ['actions[0]', 'c'], ['actions[0].params.condition', 'c'], ['actions[0].params.then[0]', 'y'], ['actions[0].params.else[0]', 'n']],
    lines: ['yes']
  },
  {
    name: 'loop and HTTP port paths',
    graph: flow([node('t', 'twitch.follow'), node('l', 'reply.loop', { times: '2' }), node('b', 'reply.chat', { message: '{local:index}' }), node('h', 'reply.http_get', { url: 'https://example.com', saveAs: 'value' }), node('o', 'reply.chat', { message: 'ok' }), node('f', 'reply.chat', { message: 'failed' })], [edge('t', 'next', 'l'), edge('l', 'body', 'b'), edge('l', 'done', 'h'), edge('h', 'ok', 'o'), edge('h', 'failure', 'f')]),
    paths: [['trigger', 't'], ['actions[0]', 'l'], ['actions[0].params.actions[0]', 'b'], ['actions[1]', 'h'], ['actions[1].params.on_ok[0]', 'o'], ['actions[1].params.on_failure[0]', 'f']],
    lines: ['1', '2', 'ok']
  }
])('compile $name maps every owned path', ({ graph, paths, lines }) => {
  const result = compileFlow(graph);
  if (!result.ok) throw new Error(JSON.stringify(result.issues));
  const def = result.value.definition;
  expect([...result.value.nodeByPath].sort()).toEqual([...paths].sort());
  expect(flowToDefinition(graph)).toEqual({ ok: true, value: def });
  const run = rehearseDefinition(def, defaultScenario(def), {});
  expect(run.events.flatMap((event) => event.lines ?? [])).toEqual([...lines]);
});

test.each([
  { times: '1000000', count: MAX_LOOP_TIMES },
  { times: '250', count: MAX_LOOP_TIMES },
  { times: '99999999999999999999999999999', count: MAX_LOOP_TIMES },
  { times: '-10', count: 0 },
  { times: 'bad', count: 0 }
])('loop count uses the run budget $times', ({ times, count }) => {
  const result = execute([action('loop', { times, actions: [chat('hi')] })]);
  expect(result.events.filter((event) => event.kind === 'say')).toHaveLength(count);
});

test.each(['effects', 'visits'])('shared run budget bounds %s', (budget) => {
  const body = budget === 'effects' ? [chat('hi')] : [{ ...chat('never'), enabled: false }];
  const nested = action('loop', { times: '100', actions: [action('loop', { times: '100', actions: body })] });
  const result = execute([nested]);
  expect(result.events.filter((event) => event.kind === 'say').length).toBeLessThanOrEqual(MAX_ACTIONS);
  expect(result.events.length).toBeLessThan(5 * MAX_ACTIONS);
  expect(result.events.filter((event) => event.kind === 'skip' && event.status === 'failed').map((event) => event.detail?.code)).toEqual([budget === 'visits' ? 'max_steps' : 'max_actions']);
});

test.each(runs.slice(0, 4))('execution is deterministic and pure: $name', ({ actions, options }) => {
  const original = JSON.stringify({ actions, options });
  const first = execute(actions, options);
  expect(execute(actions, options)).toEqual(first);
  expect(JSON.stringify({ actions, options })).toBe(original);
});

test.each<{ name: Text; type: Text; params: Params; overrides?: RehearsalOverrides; want: unknown }>([
  { name: 'invalid expanded GET URL', type: 'http_get', params: { url: '{args}' },
    want: ['failed', 'failure', 'bad_def', 'not a url', undefined, undefined, ['bad_def']] },
  { name: 'empty JSON sample', type: 'http_get', params: { url: apiUrl, json_path: ['items', '0', 'name'] },
    want: ['failed', 'failure', 'bad_def', apiUrl, undefined, undefined, ['bad_def']] },
  { name: 'JSON extraction with a pasted sample', type: 'http_get', params: { url: apiUrl, json_path: ['items', '0', 'name'] },
    overrides: { 'actions[0].response': '{"items":[{"name":"Lotti"}]}' },
    want: ['ok', 'ok', undefined, apiUrl, undefined, 'Lotti', ['Lotti']] },
  { name: 'expanded POST URL and body', type: 'http_post', params: { url: `${apiUrl}/{user}`, body: 'Hello {user}' },
    overrides: { 'actions[0].response': 'Received' },
    want: ['ok', 'ok', undefined, `${apiUrl}/Sender`, 'Hello Sender', 'Received', ['Received']] },
  { name: 'invalid POST URL takes precedence over a pretend upstream failure', type: 'http_post', params: { url: '{args}' },
    overrides: { 'actions[0]': 'failure' },
    want: ['failed', 'failure', 'bad_def', 'not a url', undefined, undefined, ['bad_def']] }
])('HTTP $name', ({ type, params, overrides, want }) => {
  const step = action(type, { ...params, save_as: 'result', on_ok: [chat('{local:result}')], on_failure: [chat('{local:error}')] });
  const { events } = execute([step], { scenario: { message: '!hello not a url', user: 'Sender' }, overrides });
  const { status, port, detail } = events.find((candidate) => candidate.kind === 'effect')!;
  const { code, url, body, response } = detail!;
  expect<unknown>([status, port, code, url, body, response, events.at(-1)?.lines]).toEqual(want);
});

test.each([
  { name: 'missing JSON field', response: '{"other":1}', path: ['value'], status: 'failed', code: 'bad_def' },
  { name: 'object JSON value', response: '{"value":{"name":"hi"}}', path: ['value'], status: 'failed', code: 'bad_def' },
  { name: 'null JSON value', response: '{"value":null}', path: ['value'], status: 'failed', code: 'bad_def' },
  { name: 'scalar JSON value', response: '{"value":true}', path: ['value'], status: 'ok', code: undefined },
  { name: 'number JSON value', response: '{"value":42}', path: ['value'], status: 'ok', code: undefined }
])('HTTP $name', ({ response, path, status, code }) => {
  const result = execute([action('http_get', { url: apiUrl, json_path: path, save_as: 'result' })], { overrides: { 'actions[0].response': response } });
  const event = result.events.at(-1);
  expect([event?.status, event?.detail?.code]).toEqual([status, code]);
});

test.each([
  { response: '{"value":9007199254740993}', path: ['value'], want: '9007199254740993' },
  { response: '{"value":1.2300e+04}', path: ['value'], want: '1.2300e+04' },
  { response: '{"value":-0}', path: ['value'], want: '-0' },
  { response: '{"value":["first","second"]}', path: ['value', '01'], want: 'second' },
  { response: '{"value":{"0":"wrong"}}', path: ['value', '0'], want: null },
  { response: '{"value":[1]}', path: ['value', 'length'], want: null },
  { response: '{"value":"quote: \\""}', path: ['value'], want: 'quote: "' },
  { response: '{"value":true}', path: ['value'], want: 'true' }
])('JSON preserves scalar text $response $path', ({ response, path, want }) => {
  const result = execute([action('http_get', { url: apiUrl, json_path: path, save_as: 'result' }), chat('{local:result}')], { overrides: { 'actions[0].response': response } });
  const event = result.events.find((event) => event.kind === 'effect');
  expect([event?.status, result.events.at(-1)?.lines]).toEqual([want === null ? 'failed' : 'ok', want === null ? [] : [want]]);
});

test.each([
  { template: '{local:missing}', lines: [], missing: 'local:missing', code: 'empty' },
  { template: 'Roll {random}', lines: ['Roll 1'], missing: '', code: undefined, random: '1' },
  { template: 'Roll {random:1-10}, {choice:a;b}', lines: ['Roll 1, a;b'], missing: '', code: undefined, random: '1' },
  { template: '{choice:a,b;c}', lines: ['a'], missing: '', code: undefined, random: '1' },
  { template: '[{choice: a;b ,c}]', lines: ['[ a;b ]'], missing: '', code: undefined, random: '1' },
  { template: '{random:1000-2000:,}', lines: ['1,000'], missing: '', code: undefined, random: '1' },
  { template: 'hello {var:missing}', lines: ['hello '], missing: 'var:missing', code: undefined },
  { template: '1\n\n2\n3\n4\n5\n6', lines: ['1', '2', '3', '4', '5'], missing: '', code: undefined },
  { template: 'one\rtwo', lines: ['one\rtwo'], missing: '', code: undefined }
])('chat expands and limits $template', ({ template, lines, missing, code, random }) => {
  const result = execute([chat(template)]);
  const event = result.events.at(-1);
  expect([event?.lines, event?.detail?.missing, event?.detail?.code, event?.detail?.random]).toEqual([lines, missing, code, random]);
});

test.each([
  { current: '9007199254740992', amount: 1, want: '9007199254740993', status: 'ok' },
  { current: '-9007199254740992', amount: -1, want: '-9007199254740993', status: 'ok' },
  { current: '9223372036854775807', amount: 1, want: '9223372036854775807', status: 'failed' },
  { current: '-9223372036854775808', amount: -1, want: '-9223372036854775808', status: 'failed' },
  { current: '1.25', amount: 2, want: '3.25', status: 'ok' }
])('variable addition preserves $current plus $amount', ({ current, amount, want, status }) => {
  const result = execute([action('var_add', { scope: 'channel', name: 'score', amount }), chat('{var:score}')], { scenario: { state: { channelVars: { score: current } } } });
  expect([result.events[1]?.status, result.events[1]?.detail?.value, result.events.at(-1)?.lines]).toEqual([status, status === 'ok' ? want : String(amount), [want]]);
});

test.each([
  { params: { name: 'timer', global_s: 90 }, units: ['d', 'h', 'm', 's'], want: '1m 30s' },
  { params: { user_s: 30 }, units: ['д', 'ч', 'м', 'с'], want: '30с' }
])('pretend cooling supplies localized time $want', ({ params, units, want }) => {
  const def = definition([action('if', { condition: condition('cooldown', params), else: [chat('{cooldown:left}')] })]);
  const result = rehearseDefinition(def, defaultScenario(def), { 'actions[0]': 'cooling' }, (seconds) => compactDuration(seconds, units));
  expect([result.events.at(-1)?.lines, result.events.at(-1)?.detail?.missing]).toEqual([[want], '']);
});

test.each([
  { type: 'math', params: { expression: 'random(1,9)' }, random: '1' },
  { type: 'math', params: { expression: 'min(1,9)' }, random: undefined },
  { type: 'text', params: { op: 'split_pick', input: 'a,b', arg1: ',', arg2: ' RANDOM ' }, random: '1' },
  { type: 'text', params: { op: 'split_pick', input: 'a,b', arg1: ',', arg2: '1' }, random: undefined },
  { type: 'text', params: { op: 'upper', input: '{local:absent}' }, random: undefined }
])('sample metadata $type $params', ({ type, params, random }) => {
  const result = execute([action(type, { ...params, save_as: 'value' })]);
  expect([result.events.at(-1)?.status, result.events.at(-1)?.detail?.random]).toEqual(['ok', random]);
});

test.each([
  { name: 'MaxLoopTimes', value: MAX_LOOP_TIMES, want: 100 },
  { name: 'MaxMathExpressionRunes', value: MAX_MATH_EXPRESSION_RUNES, want: 200 },
  { name: 'MaxActions', value: MAX_ACTIONS, want: 1000 },
  { name: 'MaxDefinitionBytes', value: MAX_DEFINITION_BYTES, want: 262144 },
  { name: 'MaxLightsHoldS', value: MAX_LIGHTS_HOLD_S, want: 60 },
  { name: 'MinLightsKelvin', value: MIN_LIGHTS_KELVIN, want: 2000 },
  { name: 'MaxLightsKelvin', value: MAX_LIGHTS_KELVIN, want: 9000 },
  { name: 'MinLightsBrightness', value: MIN_LIGHTS_BRIGHTNESS, want: 1 },
  { name: 'MaxLightsBrightness', value: MAX_LIGHTS_BRIGHTNESS, want: 100 },
  { name: 'MaxAgeDays', value: MAX_AGE_DAYS, want: 3650 }
])('mirrored Go limit $name', ({ name, value, want }) => {
  const source = readFileSync(new URL('../../../../internal/domain/automation/limits.go', import.meta.url), 'utf8');
  const declared = Number(new RegExp(`${name}\\s*=\\s*(\\d+)`).exec(source)?.[1]);
  expect([value, declared]).toEqual([want, want]);
});

test.each([
  { type: 'countdown_add', params: { name: 'missing', seconds: 5 }, code: 'not_running' },
  { type: 'cooldown_add', params: { name: 'missing', seconds: 5 }, code: 'not_running' },
  { type: 'text', params: { op: 'substring', input: 'hello', arg1: '99999999999999999999' }, code: 'text_error' },
  { type: 'var_add', params: { scope: 'channel', name: 'max', amount: 1 }, code: 'var_limit' }
])('action failure or skip $type $params', ({ type, params, code }) => {
  const result = execute([action(type, params)], { scenario: { state: { channelVars: { max: '9223372036854775807' } } } });
  expect(result.events.at(-1)?.detail?.code).toBe(code);
});
