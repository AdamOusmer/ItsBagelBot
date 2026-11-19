// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, spyOn, test } from 'bun:test';
import * as automations from '@bagel/kit/automations';
import { english } from './english.testing';
import type { Flow, FlowNode } from './model';
import { prepareRehearsal, rehearseDraft, rehearseFlow, setStoredValue, type RehearsalOptions } from './rehearse';

type Id = FlowNode['id'];
type Kind = FlowNode['type'];
const n = (id: Id, type: Kind, config: FlowNode['config'] = {}): FlowNode => ({ id, type, config, x: 0, y: 0 });
const e = (from: Id, port: Id, to: Id) => ({ id: `${from}-${port}-${to}`, from, port, to });
const flowOf = (nodes: readonly FlowNode[], edges: readonly Flow['edges'][number][]): Flow => ({ id: 'test', name: 'Test', enabled: true, nodes: [...nodes], edges: [...edges] });
const run = (flow: Flow, options: RehearsalOptions = {}) => rehearseFlow(flow, english, options);
const trigger = n('t', 'twitch.chat.command', { trigger: '!test' });
const say = n('s', 'reply.chat', { message: 'hello {user}' });
const left = n('a', 'reply.chat', { message: 'A' });
const right = n('b', 'reply.chat', { message: 'B' });
const join = n('j', 'reply.chat', { message: 'JOIN' });
const wait = n('w', 'reply.wait', { seconds: 5 });
const fork = [e('t', 'next', 'a'), e('t', 'next', 'b')];
const loop = flowOf([trigger, n('l', 'reply.loop', { times: '3' }), n('s', 'reply.chat', { message: 'Pass {local:index}' }), join], [e('t', 'next', 'l'), e('l', 'body', 's'), e('l', 'done', 'j')]);

test.each([
  { name: 'parallel lanes', flow: flowOf([trigger, left, right], fork), expected: [['a', 0, [0], [], ['A']], ['b', 0, [1], [], ['B']]] },
  { name: 'paused parallel lane', flow: flowOf([trigger, wait, left, right], [e('t', 'next', 'w'), e('t', 'next', 'b'), e('w', 'next', 'a')]), expected: [['b', 0, [1], [], ['B']], ['a', 5000, [0], [], ['A']]] },
  { name: 'diamond continuation once after both arms', flow: flowOf([trigger, left, right, join], [...fork, e('a', 'next', 'j'), e('b', 'next', 'j')]), expected: [['a', 0, [0], [], ['A']], ['b', 0, [1], [], ['B']], ['j', 0, [], [], ['JOIN']]] },
  { name: 'every loop iteration', flow: loop, expected: [['s', 0, [], [1], ['Pass 1']], ['s', 0, [], [2], ['Pass 2']], ['s', 0, [], [3], ['Pass 3']], ['j', 0, [], [], ['JOIN']]] }
])('adapter maps $name', ({ flow, expected }) => {
  const actual = run(flow).steps.filter((step) => step.kind === 'say').map((step) => [step.id, step.atMs, step.lane, step.iteration, step.lines]);
  expect<unknown>(actual).toEqual(expected);
});

test.each([
  { name: 'missing trigger', flow: flowOf([say], []), text: 'Add a When step', node: null },
  { name: 'cycle', flow: flowOf([trigger, say], [e('t', 'next', 's'), e('s', 'next', 's')]), text: 'loop', node: 's' },
  { name: 'paused merge', flow: flowOf([trigger, wait, left, right, join], [e('t', 'next', 'w'), e('t', 'next', 'b'), e('w', 'next', 'a'), e('a', 'next', 'j'), e('b', 'next', 'j')]), text: 'A path with a Wait', node: 'j' }
])('structural error: $name', ({ flow, text, node }) => {
  const result = run(flow);
  expect([result.issue?.text.toLowerCase().includes(text.toLowerCase()), result.issue?.node, result.steps]).toEqual([true, node, []]);
});

test.each<{ type: Kind; config: FlowNode['config']; field: string; initial: string | number; expected: string | number; text: string }>([
  { type: 'twitch.cheer', config: { minBits: 1000 }, field: 'bits', initial: 500, expected: 1000, text: '500 is below 1000 bits' },
  { type: 'twitch.raid', config: { minViewers: 100 }, field: 'viewers', initial: 42, expected: 100, text: '42 is below 100 viewers' },
  { type: 'twitch.chat.command', config: { trigger: '!test' }, field: 'message', initial: 'hello', expected: '!test @Lotti good luck', text: 'does not match !test' }
])('would not start: $type, then accepts the explicit repair', ({ type, config, field, initial, expected, text }) => {
  const flow = flowOf([n('t', type, config), say], [e('t', 'next', 's')]);
  const before = run(flow, { scenario: { [field]: initial } });
  const after = run(flow, { scenario: before.startPatch });
  expect([before.starts, before.blocked?.includes(text), before.startPatch, after.steps.at(-1)?.id]).toEqual([false, true, { [field]: expected }, 's']);
});

test.each([
  { name: 'role', config: n('c', 'cond.role', { level: 'mod' }), patch: { role: 'mod' } },
  { name: 'viewer ID', config: n('c', 'cond.user', { userId: '42', login: 'Lotti' }), patch: { userId: '42', user: 'Lotti' } },
  { name: 'compare input', config: n('c', 'cond.compare', { left: '{user.id}', op: 'eq', right: '42' }), patch: { userId: '42' } }
])('make it pass changes the $name input', ({ config, patch }) => {
  const flow = flowOf([trigger, config, say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const before = run(flow);
  expect([before.starts, before.state, before.stoppedAt]).toEqual([true, 'stopped', 'c']);
  const repair = before.steps.find((step) => step.id === 'c')?.repair;
  const after = run(flow, { scenario: repair });
  expect([repair, before.steps.some((step) => step.id === 's'), after.steps.at(-1)?.id]).toEqual([patch, false, 's']);
});

test.each<{ type: Kind; config: FlowNode['config']; missing: string[] }>([
  { type: 'cond.compare', config: { left: '5', op: 'gte', right: '' }, missing: [] },
  { type: 'reply.announce', config: { text: '' }, missing: ['Message'] },
  { type: 'reply.reply', config: { text: '' }, missing: ['Message'] }
])('required fields use the save policy for $type', ({ type, config, missing }) => {
  const result = run(flowOf([trigger, n('c', type, config)], [e('t', 'next', 'c')]));
  expect(result.steps.find((step) => step.id === 'c')?.missing).toEqual(missing);
});

test.each([
  { name: 'chance', node: n('c', 'cond.chance', { percent: 50 }), overrides: { c: 'no' }, port: 'no' },
  { name: 'cooldown', node: n('c', 'cond.cooldown', { seconds: 30 }), overrides: { c: 'cooling' }, port: 'no' },
  { name: 'API', node: n('c', 'reply.http_get', { url: 'https://example.com' }), overrides: { c: 'failure' }, port: 'failure' }
])('per-step pretend $name uses the compiled path', ({ node, overrides, port }) => {
  const result = run(flowOf([trigger, node], [e('t', 'next', 'c')]), { overrides });
  expect(result.steps.find((step) => step.id === 'c')?.port).toBe(port);
});

test.each([
  { name: 'disconnected step', nodes: [trigger, say, left], edges: [e('t', 'next', 's')], expected: ['t', 's'] },
  { name: 'unset viewer check', nodes: [trigger, n('c', 'cond.user'), say], edges: [e('t', 'next', 'c'), e('c', 'yes', 's')], expected: ['t', 'c'] },
  { name: 'disabled action', nodes: [trigger, n('s', 'reply.chat', { message: 'hidden', disabled: true })], edges: [e('t', 'next', 's')], expected: ['t', 's'] }
])('mid-build rehearsal tolerates $name', ({ nodes, edges, expected }) => {
  const result = run(flowOf(nodes, edges));
  expect<unknown>([result.issue, result.steps.map((step) => step.id)]).toEqual([undefined, expected]);
});

test('missing values stay marked and disconnected previews stay unreachable', () => {
  const result = run(flowOf([trigger, n('s', 'reply.chat', { message: 'Bits: {bits}, value: {var:unset}' }), left], [e('t', 'next', 's')]));
  expect([result.steps.at(-1)?.missing, result.steps.some((step) => step.id === 'a')]).toEqual([['Bits', 'unset'], false]);
});

test('stored input is shared with checks and chat in the executor', () => {
  const node = n('c', 'cond.var', { name: 'score', op: 'gte', value: '10' });
  const flow = flowOf([trigger, node, n('s', 'reply.chat', { message: '{var:score}' })], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const result = run(flow, { scenario: setStoredValue({}, node, '12') });
  expect(result.steps.at(-1)?.lines).toEqual(['12']);
  expect(setStoredValue({ live: true, user: 'Sample' }, node, '12')).toEqual({ state: { channelVars: { score: '12' } } });
});

test.each([
  { type: 'twitch.raid', scenario: { user: 'Ana', viewers: 1 }, summary: 'Ana raids with 1 viewer' },
  { type: 'twitch.raid', scenario: { user: 'Ana', viewers: 42 }, summary: 'Ana raids with 42 viewers' },
  { type: 'twitch.cheer', scenario: { user: 'Ana', bits: 1 }, summary: 'Ana cheers 1 bit' },
  { type: 'twitch.cheer', scenario: { user: 'Ana', bits: 0 }, summary: 'Ana cheers 0 bits' }
])('scenario summary counts in words: $summary', ({ type, scenario, summary }) => {
  expect(run(flowOf([n('t', type)], []), { scenario }).summary).toBe(summary);
});

test.each([
  { times: '1', text: 'Repeat 1 time' },
  { times: '3', text: 'Repeat 3 times' }
])('loop step reads "$text"', ({ times, text }) => {
  const flow = flowOf([trigger, n('l', 'reply.loop', { times })], [e('t', 'next', 'l')]);
  expect(run(flow).steps.find((step) => step.id === 'l')?.text).toBe(text);
});

test('loop events have unique keys for repeated canvas nodes', () => {
  const keys = run(loop).steps.map((step) => step.key);
  expect(new Set(keys).size).toBe(keys.length);
});

test.each([
  { name: 'an event cannot acquire a moderator role', trigger: n('t', 'twitch.follow'), check: n('c', 'cond.role', { level: 'mod' }) },
  { name: 'a compare cannot replace the command itself', trigger, check: n('c', 'cond.compare', { left: '{message}', op: 'eq', right: 'different' }) }
])('repairs remain truthful: $name', ({ trigger, check }) => {
  const flow = flowOf([trigger, check, say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const result = run(flow);
  const step = result.steps.find((step) => step.id === 'c')!;
  expect(step.repair).toBeUndefined();
});

test.each([
  { op: 'eq', right: '', missing: [], reason: 'does not match' }, { op: 'gte', right: '', missing: [], reason: 'is not at least' },
  { op: 'lt', right: '', missing: [], reason: 'is not less than' }, { op: 'contains', right: '', missing: ['This value'], reason: 'does not contain' },
  { op: 'eq', right: '6', missing: [], reason: 'does not match' }, { op: 'gte', right: '10', missing: [], reason: 'is not at least' },
  { op: 'lt', right: '3', missing: [], reason: 'is not less than' }, { op: 'contains', right: '6', missing: [], reason: 'does not contain' }
])('a failed $op check explains comparing with "$right"', ({ op, right, missing, reason }) => {
  const check = n('c', 'cond.compare', { left: '5', op, right });
  const step = run(flowOf([trigger, check], [e('t', 'next', 'c')])).steps.find((entry) => entry.id === 'c');
  expect([step?.status, step?.missing, step?.note]).toEqual(['no', [...missing], `5 ${reason} ${right || 'an empty value'}.`]);
});

test.each(['twitch.chat.command', 'twitch.chat.word', 'twitch.follow', 'twitch.sub', 'twitch.cheer', 'twitch.raid', 'twitch.reward', 'twitch.stream.online'])(
  '%s explains when viewer roles are unavailable', (type) => {
    const check = n('c', 'cond.role', { level: 'mod' });
    const root = n('t', type, { trigger: '!test', word: 'hello', reward: 'reward' });
    const step = run(flowOf([root, check], [e('t', 'next', 'c')])).steps.find((entry) => entry.id === 'c');
    const note = type.startsWith('twitch.chat.') ? undefined : 'Roles are known only for chat triggers and countdowns. On this trigger everyone counts as a viewer.';
    expect([step?.status, step?.note, Boolean(step?.repair)]).toEqual(['no', note, type.startsWith('twitch.chat.')]);
  }
);

test('draft tolerance leaves the edited flow untouched', () => {
  const flow = flowOf([trigger, n('c', 'cond.user'), n('s', 'reply.chat', { message: '{bits}' }), left], [e('t', 'next', 'c'), e('c', 'no', 's')]);
  const before = structuredClone(flow);
  run(flow);
  expect(flow).toEqual(before);
});

test('a changed event trigger does not reuse the prior chat message', () => {
  const result = run(flowOf([n('t', 'twitch.follow')], []), { scenario: { message: '!old' } });
  expect(result.steps[0]?.text).not.toBe('!old');
});

test.each([
  { node: n('s', 'reply.chat', { message: '{random:1-10} {choice:a;b}' }), text: '', lines: ['1 a'], secondary: undefined },
  { node: n('s', 'reply.math', { expression: 'random(1,9)', saveAs: 'score' }), text: '1', lines: undefined, secondary: 'random(1,9)' }
])('samples and computed math values for $node.type', ({ node, text, lines, secondary }) => {
  const step = run(flowOf([trigger, node], [e('t', 'next', 's')])).steps.at(-1);
  expect<unknown>([step?.sample, step?.text, step?.lines, step?.secondary, step?.missing]).toEqual([true, text, lines, secondary, []]);
});

test.each([
  { node: n('s', 'reply.shoutout', { target: '{touser}' }), text: 'Shoutout to Sender' },
  { node: n('s', 'reply.text', { op: 'upper', input: '{user}', saveAs: 'result' }), text: 'SENDER' },
  { node: n('s', 'reply.var_set', { name: 'name', scope: 'channel', value: 'Hi {user}' }), text: 'Set name to Hi Sender' },
  { node: n('s', 'reply.http_get', { url: 'https://example.com/{user}', saveAs: 'result' }), text: 'https://example.com/Sender' }
])('effect descriptions show rehearsal values for $node.type', ({ node, text }) => {
  const flow = flowOf([trigger, node], [e('t', 'next', 's')]);
  const step = run(flow, { scenario: { message: '!test', user: 'Sender' } }).steps.at(-1);
  expect(step?.text).toBe(text);
});

test.each(['twitch.stream.online', 'twitch.stream.offline', 'countdown.finished'])('stale viewer patches cannot affect %s', (type) => {
  const result = run(flowOf([n('t', type, { name: 'timer' }), n('s', 'reply.points', { amount: 10 })], [e('t', 'next', 's')]), {
    scenario: { user: 'Old', userId: '42', message: '!old', bits: 50 }
  });
  expect([result.scenario.user, result.scenario.message, result.scenario.bits]).toEqual([undefined, undefined, undefined]);
});

test.each([false, true])('permission warnings match Save with moderator check %s', (checked) => {
  const check = n('c', 'cond.role', { level: 'mod' });
  const timeout = n('s', 'reply.timeout', { target: '{touser}', seconds: 60 });
  const flow = checked ? flowOf([trigger, check, timeout], [e('t', 'next', 'c'), e('c', 'yes', 's')]) : flowOf([trigger, timeout], [e('t', 'next', 's')]);
  const compiled = automations.compileFlow(flow);
  if (!compiled.ok) throw new Error(JSON.stringify(compiled.issues));
  const warnings = run(flow).steps.filter((step) => step.detail?.code === 'perm_required');
  const issues = automations.validateDefinition(compiled.value.definition).filter((issue) => issue.code === 'perm_required');
  expect([warnings.length, warnings.map((step) => step.id)]).toEqual([issues.length, checked ? [] : ['s']]);
});

test('repeated failed checks verify a repair only on request', () => {
  const nodes = [trigger, n('outer', 'reply.loop', { times: '100' }), n('inner', 'reply.loop', { times: '100' }), n('c', 'cond.role', { level: 'mod' }), left, right];
  const edges = [e('t', 'next', 'outer'), e('outer', 'body', 'inner'), e('inner', 'body', 'c'), e('c', 'yes', 'a'), e('c', 'no', 'b')];
  const executor = spyOn(automations, 'rehearseDefinition');
  const result = run(flowOf(nodes, edges));
  const calls = executor.mock.calls.length;
  const step = result.steps.find((step) => step.id === 'c')!;
  const verified = result.verifyRepair?.(step.repair!, step.path);
  const requested = executor.mock.calls.length;
  executor.mockRestore();
  expect([calls, requested, verified, result.steps.filter((step) => step.id === 'c').length > 100, step.repair]).toEqual([1, 2, true, true, { role: 'mod' }]);
});

test('scenario edits reuse the compiled draft', () => {
  const compiler = spyOn(automations, 'compileFlow');
  const draft = prepareRehearsal(flowOf([trigger, say], [e('t', 'next', 's')]));
  const lines = ['First', 'Second'].map((user) => rehearseDraft(draft, english, { scenario: { user } }).steps.at(-1)?.lines);
  const calls = compiler.mock.calls.length;
  compiler.mockRestore();
  expect([calls, lines]).toEqual([1, [['hello First'], ['hello Second']]]);
});

test('make it start verifies the trigger even when a later check stops the run', () => {
  const flow = flowOf([n('t', 'twitch.cheer', { minBits: 1000 }), n('c', 'cond.compare', { left: '1', op: 'eq', right: '2' }), say], [e('t', 'next', 'c'), e('c', 'yes', 's')]);
  const view = run(flow, { scenario: { bits: 1 } });
  expect(view.startPatch && view.verifyRepair?.(view.startPatch)).toBe(true);
});

test.each(['', 'Hello'])('HTTP response %j uses plain labels for missing values', (response) => {
  const flow = flowOf([trigger, n('api', 'reply.http_get', { url: 'https://example.com', saveAs: 'joke' }), n('s', 'reply.chat', { message: 'Joke: {local:joke}' })], [e('t', 'next', 'api'), e('api', 'ok', 's')]);
  const result = run(flow, { overrides: { 'api.response': response } });
  expect([result.steps.find((step) => step.id === 'api')?.note, result.steps.at(-1)?.missing]).toEqual(response ? [undefined, []] : ['Pretend response is empty.', ['joke']]);
});
