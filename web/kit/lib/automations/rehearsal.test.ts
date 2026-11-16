// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import mathCases from './testdata/math_cases.json';
import textCases from './testdata/text_cases.json';
import { compileFlow, flowToDefinition, type Flow, type FlowNode } from './flow-graph';
import { compactDuration, defaultScenario, rehearseDefinition } from './rehearsal';
import { MAX_ACTIONS, MAX_LOOP_TIMES } from './limits';
import type { Action, Condition, Definition, Params } from './types';
import type { RehearsalOverrides, RehearsalScenario } from './rehearsal-types';

type Text = string;
const action = (type: Text, params: Params = {}): Action => ({ type, params });
const chat = (text: Text): Action => action('chat', { text });
const condition = (type: Text, params: Params): Condition => ({ type, params });
const choose = (condition: Condition): Action => action('if', { condition, then: [chat('yes')], else: [chat('no')] });
const definition = (actions: Action[]): Definition => ({ version: 1, trigger: { type: 'command', params: { name: '!hello' } }, actions });
const parallel = (branches: Action[][]): Action => action('parallel', { branches: branches.map((actions) => ({ weight: 1, actions })) });

type Options = { scenario?: Partial<RehearsalScenario>; overrides?: RehearsalOverrides };
function execute(actions: Action[], options: Options = {}) {
  const def = definition(actions);
  return rehearseDefinition(def, { ...defaultScenario(def), ...options.scenario }, options.overrides);
}

type RunCase = { name: Text; actions: Action[]; options?: Options; lines: Text[]; times?: number[] };
const runs: RunCase[] = [
  { name: 'parallel pause starts the sibling and continuation immediately', actions: [parallel([[action('delay', { ms: 5000 }), chat('A')], [chat('B')]]), chat('join')], lines: ['B', 'join', 'A'], times: [0, 0, 5000] },
  { name: 'resumed branches update shared state in time order', actions: [parallel([[action('delay', { ms: 5000 }), chat('{var:score}')], [action('var_set', { scope: 'channel', name: 'score', value: '9' })]])], lines: ['9'] },
  { name: 'later branches cannot observe future state', actions: [parallel([[action('delay', { ms: 5000 }), action('var_set', { scope: 'channel', name: 'score', value: '9' })], [chat('{var:score}')]])], lines: [] },
  { name: 'loop executes three times', actions: [action('loop', { times: '3', actions: [chat('{local:index}')] })], lines: ['1', '2', '3'] },
  { name: 'loop delay carries the remaining body and iterations', actions: [action('loop', { times: '3', actions: [action('delay', { ms: 5000 }), chat('{local:index}')] }), chat('done')], lines: ['1', '2', '3', 'done'], times: [5000, 10000, 15000, 15000] },
  { name: 'nested loop restores index', actions: [action('loop', { times: '2', actions: [action('loop', { times: '1', actions: [chat('inner {local:index}')] }), chat('outer {local:index}')] }), chat('{local:index}')], lines: ['inner 1', 'outer 1', 'inner 1', 'outer 2'] },
  { name: 'zero loop goes straight to done', actions: [action('loop', { times: '0', actions: [chat('never')] }), chat('done')], lines: ['done'] },
  { name: 'parallel locals are cloned and restored', actions: [action('math', { expression: '1', save_as: 'x' }), parallel([[action('math', { expression: '2', save_as: 'x' }), chat('{local:x}')], [chat('{local:x}')]]), chat('{local:x}')], lines: ['2', '1', '1'] },
  { name: 'http failure followed by success clears only error', actions: [action('http_get', { save_as: 'result', on_failure: [chat('{local:error}')] }), action('http_get', { save_as: 'result', on_ok: [chat('{local:result} {local:error}')] })], options: { overrides: { 'actions[0]': 'failure', 'actions[1].response': 'hello' } }, lines: ['upstream_error', 'hello '] },
  { name: 'http success cannot leak failure-only values into siblings', actions: [parallel([[action('http_get', { on_failure: [chat('{local:error}')] })], [chat('{local:error}')]])], options: { overrides: { 'actions[0].params.branches[0].actions[0]': 'failure' } }, lines: ['upstream_error'] },
  { name: 'http JSON extraction saves the selected value', actions: [action('http_get', { json_path: ['items', '0', 'name'], save_as: 'result', on_ok: [chat('{local:result}')] })], options: { overrides: { 'actions[0].response': '{"items":[{"name":"Lotti"}]}' } }, lines: ['Lotti'] },
  { name: 'bad JSON takes failure only', actions: [action('http_get', { json_path: ['value'], on_ok: [chat('never')], on_failure: [chat('{local:error}')] })], options: { overrides: { 'actions[0].response': 'not JSON' } }, lines: ['bad_def'] },
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
  { name: 'countdown starts and extends with time left', actions: [action('countdown_start', { name: 'timer', seconds: 60 }), action('delay', { ms: 5000 }), action('countdown_add', { name: 'timer', seconds: 10 }), chat('{countdown:left}')], lines: ['1m 5s'] },
  { name: 'named cooldown extends only a running window', actions: [choose(condition('cooldown', { name: 'timer', global_s: 60 })), action('cooldown_add', { name: 'timer', seconds: 30 }), chat('{cooldown:left}')], lines: ['yes', '1m 30s'] },
  { name: 'cooling branch reports current time left', actions: [choose(condition('cooldown', { name: 'timer', global_s: 60 })), action('delay', { ms: 5000 }), action('if', { condition: condition('cooldown', { name: 'timer', global_s: 60 }), else: [chat('{cooldown:left}')] })], lines: ['yes', '55s'] }
];

test.each(runs)('$name', ({ actions, options, lines, times }) => {
  const result = execute(actions, options);
  const said = result.events.filter((event) => event.kind === 'say');
  expect(said.flatMap((event) => event.lines ?? [])).toEqual(lines);
  if (times) expect(said.map((event) => event.atMs)).toEqual(times);
  expect(new Set(result.events.map((event) => event.key)).size).toBe(result.events.length);
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

test.each([
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
  { type: 'compare', params: { left: '{local:missing}', op: 'eq', right: '' }, expected: 'yes' }
])('$type $params', ({ type, params, expected, ...options }) => {
  const result = execute([choose(condition(type, params))], { ...options, scenario: { state: { channelVars: { score: '1', word: 'a' } } } });
  expect(result.events.find((event) => event.kind === 'say')?.lines).toEqual([expected]);
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

test.each([
  { name: 'missing JSON field', response: '{"other":1}', path: ['value'], status: 'failed', code: 'bad_def' },
  { name: 'object JSON value', response: '{"value":{"name":"hi"}}', path: ['value'], status: 'failed', code: 'bad_def' },
  { name: 'null JSON value', response: '{"value":null}', path: ['value'], status: 'failed', code: 'bad_def' },
  { name: 'scalar JSON value', response: '{"value":true}', path: ['value'], status: 'ok', code: undefined },
  { name: 'number JSON value', response: '{"value":42}', path: ['value'], status: 'ok', code: undefined }
])('HTTP $name', ({ response, path, status, code }) => {
  const result = execute([action('http_get', { json_path: path, save_as: 'result' })], { overrides: { 'actions[0].response': response } });
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
  const result = execute([action('http_get', { json_path: path, save_as: 'result' }), chat('{local:result}')], { overrides: { 'actions[0].response': response } });
  const event = result.events.find((event) => event.kind === 'effect');
  expect([event?.status, result.events.at(-1)?.lines]).toEqual([want === null ? 'failed' : 'ok', want === null ? [] : [want]]);
});

test.each([
  { template: '{local:missing}', lines: [], missing: 'local:missing', code: 'empty' },
  { template: 'Roll {random:1-10}, {choice:a;b}', lines: ['Roll 1, a'], missing: '', code: undefined },
  { template: '{choice:first,second}', lines: ['first'], missing: '', code: undefined },
  { template: '{random:1000-2000:,}', lines: ['1,000'], missing: '', code: undefined },
  { template: 'hello {var:missing}', lines: ['hello '], missing: 'var:missing', code: undefined },
  { template: '1\n\n2\n3\n4\n5\n6', lines: ['1', '2', '3', '4', '5'], missing: '', code: undefined },
  { template: 'one\rtwo', lines: ['one\rtwo'], missing: '', code: undefined }
])('chat expands and limits $template', ({ template, lines, missing, code }) => {
  const result = execute([chat(template)]);
  const event = result.events.at(-1);
  expect([event?.lines, event?.detail?.missing, event?.detail?.code]).toEqual([lines, missing, code]);
});

test.each([
  { current: '9007199254740992', amount: 1, want: '9007199254740993', status: 'ok' },
  { current: '-9007199254740992', amount: -1, want: '-9007199254740993', status: 'ok' },
  { current: '9223372036854775807', amount: 1, want: '9223372036854775807', status: 'failed' },
  { current: '-9223372036854775808', amount: -1, want: '-9223372036854775808', status: 'failed' },
  { current: '1.25', amount: 2, want: '3.25', status: 'ok' }
])('variable addition preserves $current plus $amount', ({ current, amount, want, status }) => {
  const result = execute([action('var_add', { scope: 'channel', name: 'score', amount }), chat('{var:score}')], { scenario: { state: { channelVars: { score: current } } } });
  expect([result.events[1]?.status, result.events.at(-1)?.lines]).toEqual([status, [want]]);
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

test.each([{ name: 'MaxLoopTimes', value: MAX_LOOP_TIMES, want: 100 }])('mirrored Go limit $name', ({ name, value, want }) => {
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
